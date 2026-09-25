package application

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"strings"
	"sync"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type activeMailDelivery struct {
	cancel context.CancelFunc
	stop   func() bool
}

type MailDispatcher struct {
	outbox          MailOutboxStore
	mailer          Mailer
	random          RandomSource
	tokenKey        []byte
	invitationKey   []byte
	emailChangeKey  []byte
	publicURL       string
	pollInterval    time.Duration
	lease           time.Duration
	retryInitial    time.Duration
	retryMax        time.Duration
	maxAttempts     int
	now             func() time.Time
	identity        SystemIdentityProvider
	materialBox     SecretBox
	requireMaterial bool
	retryPolicy     MailTaskRetryPolicyProvider
	mailerProvider  MailerProvider

	mu              sync.Mutex
	stopping        bool
	stopCh          chan struct{}
	shutdownContext context.Context
	active          *activeMailDelivery
}

func derivePurposeKey(master []byte, purpose string) []byte {
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte(purpose))
	return mac.Sum(nil)
}

func (d *MailDispatcher) SetSystemIdentityProvider(identity SystemIdentityProvider) {
	if d != nil {
		d.identity = identity
	}
}

// SetMailTaskSecretBox enables the generated durable path. It must be called
// during server startup; without it only the deprecated legacy dispatcher
// constructor path can compose material-less test fixtures.
func (d *MailDispatcher) SetMailTaskSecretBox(box SecretBox) {
	if d != nil {
		d.materialBox = box
		// Generated servers call this setter during startup. Once configured,
		// an unencrypted task is a dependency failure rather than permission to
		// reconstruct a message from mutable account state.
		d.requireMaterial = true
	}
}

func (d *MailDispatcher) SetMailRetryPolicyProvider(provider MailTaskRetryPolicyProvider) {
	if d != nil {
		d.retryPolicy = provider
	}
}

// SetMailerProvider makes each delivery resolve the latest persisted SMTP
// configuration instead of relying on a process-local sender. This is needed
// when more than one API/dispatcher instance shares the database.
func (d *MailDispatcher) SetMailerProvider(provider MailerProvider) {
	if d != nil {
		d.mailerProvider = provider
	}
}

func NewMailDispatcher(outbox MailOutboxStore, mailer Mailer, random RandomSource, tokenKey []byte, publicURL string, pollInterval, lease, retryInitial, retryMax time.Duration, invitationKeys ...[]byte) *MailDispatcher {
	dispatcher := &MailDispatcher{
		outbox:       outbox,
		mailer:       mailer,
		random:       random,
		tokenKey:     append([]byte(nil), tokenKey...),
		publicURL:    strings.TrimRight(publicURL, "/"),
		pollInterval: pollInterval,
		lease:        lease,
		retryInitial: retryInitial,
		retryMax:     retryMax,
		maxAttempts:  10,
		now:          time.Now,
		stopCh:       make(chan struct{}),
	}
	if len(invitationKeys) > 0 {
		dispatcher.invitationKey = append([]byte(nil), invitationKeys[0]...)
	}
	if len(invitationKeys) > 1 {
		dispatcher.emailChangeKey = append([]byte(nil), invitationKeys[1]...)
	} else if len(tokenKey) == 32 {
		// Keep the legacy constructor source-compatible without reusing the
		// password-reset key as an email-change authority.
		dispatcher.emailChangeKey = derivePurposeKey(tokenKey, "temvia-email-change-code-key-v1")
	}
	return dispatcher
}

// BeginShutdown stops new claims while allowing the currently claimed job to
// finish until the supplied context ends. The context should carry the shared
// shutdown deadline; it is deliberately not canceled at the moment the
// dispatcher is told to stop claiming.
func (d *MailDispatcher) BeginShutdown(ctx context.Context) {
	if d == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	d.mu.Lock()
	if d.stopping {
		d.mu.Unlock()
		return
	}
	d.stopping = true
	if d.stopCh == nil {
		d.stopCh = make(chan struct{})
	}
	close(d.stopCh)
	d.shutdownContext = ctx
	active := d.active
	d.mu.Unlock()
	if active != nil {
		d.linkActiveDelivery(active, ctx)
	}
}

func (d *MailDispatcher) isStopping() bool {
	if d == nil {
		return true
	}
	d.mu.Lock()
	stopping := d.stopping
	d.mu.Unlock()
	return stopping
}

func (d *MailDispatcher) Run(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if d.pollInterval <= 0 {
		d.pollInterval = time.Second
	}
	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil || d.isStopping() {
			return
		}
		if err := d.ProcessOnce(ctx); err != nil && ctx.Err() != nil {
			return
		}
		if ctx.Err() != nil || d.isStopping() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
		}
	}
}

// ProcessOnce performs one bounded claim/send/ack cycle. SMTP is called only
// after the claim transaction has committed.
func (d *MailDispatcher) ProcessOnce(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.isStopping() {
		return context.Canceled
	}
	if d.outbox == nil || d.random == nil || (d.mailer == nil && d.mailerProvider == nil) {
		return ErrDependencyUnavailable
	}
	if err := d.outbox.SweepMail(ctx); err != nil {
		return dependencyError(err)
	}
	if err := d.outbox.CleanupMail(ctx); err != nil {
		return dependencyError(err)
	}
	if err := ctx.Err(); err != nil || d.isStopping() {
		if err != nil {
			return err
		}
		return context.Canceled
	}
	leaseToken, err := newLeaseToken(d.random)
	if err != nil {
		return dependencyError(err)
	}
	job, err := d.outbox.ClaimMail(ctx, leaseToken, d.lease)
	if err != nil {
		return dependencyError(err)
	}
	if job == nil {
		return nil
	}
	jobCtx, releaseJob := d.activeJobContext(ctx)
	defer releaseJob()
	if len(job.EncryptedMaterial) == 0 && !d.requireMaterial && !d.now().Before(job.ExpiresAt) {
		ackCtx, cancel := d.acknowledgementContext(jobCtx)
		defer cancel()
		discarded, discardErr := d.outbox.DiscardMail(ackCtx, job.ID, job.LeaseToken, "expired")
		if discardErr != nil {
			return dependencyError(discardErr)
		}
		if !discarded {
			d.recordMailAttempt(ackCtx, *job, MailTaskOutcomeFailed, "expired")
			return nil
		}
		return nil
	}
	message, err := d.compose(jobCtx, *job)
	if err != nil {
		ackCtx, cancel := d.acknowledgementContext(jobCtx)
		defer cancel()
		switch {
		case errors.Is(err, ErrInvalidPasswordResetToken):
			discarded, discardErr := d.outbox.DiscardMail(ackCtx, job.ID, job.LeaseToken, "invalid_reset")
			if discardErr != nil {
				return dependencyError(discardErr)
			}
			if !discarded {
				d.recordMailAttempt(ackCtx, *job, MailTaskOutcomeFailed, "invalid_reset")
				return nil
			}
			return nil
		case errors.Is(err, ErrInvitationInvalid):
			discarded, discardErr := d.outbox.DiscardMail(ackCtx, job.ID, job.LeaseToken, "invalid_invitation")
			if discardErr != nil {
				return dependencyError(discardErr)
			}
			if !discarded {
				d.recordMailAttempt(ackCtx, *job, MailTaskOutcomeFailed, "invalid_invitation")
				return nil
			}
			return nil
		case errors.Is(err, ErrInvalidEmailChange):
			discarded, discardErr := d.outbox.DiscardMail(ackCtx, job.ID, job.LeaseToken, "invalid_email_change")
			if discardErr != nil {
				return dependencyError(discardErr)
			}
			if !discarded {
				d.recordMailAttempt(ackCtx, *job, MailTaskOutcomeFailed, "invalid_email_change")
				return nil
			}
			return nil
		default:
			// Identity storage is a runtime dependency. Keep the queued message
			// retryable instead of misclassifying it as a corrupt reset or invite.
			return d.handleDeliveryFailure(ackCtx, *job, &MailDeliveryError{Code: "dependency", Temporary: true})
		}
	}
	deliveryCtx, cancelDelivery := d.deliveryContext(jobCtx)
	defer cancelDelivery()
	mailer, mailerErr := d.resolveMailer(deliveryCtx)
	if mailerErr != nil {
		ackCtx, cancelAck := d.acknowledgementContext(jobCtx)
		defer cancelAck()
		if errors.Is(mailerErr, ErrMailNotConfigured) {
			// The task schema intentionally exposes only the stable diagnostic
			// vocabulary. Missing settings are a non-retryable configuration
			// failure for this round and therefore map to "permanent".
			mailerErr = &MailDeliveryError{Code: "permanent", Temporary: false}
		} else {
			mailerErr = &MailDeliveryError{Code: "dependency", Temporary: true}
		}
		return d.handleDeliveryFailure(ackCtx, *job, mailerErr)
	}
	sendErr := mailer.Send(deliveryCtx, message)
	ackCtx, cancelAck := d.acknowledgementContext(jobCtx)
	defer cancelAck()
	if sendErr == nil {
		marked, err := d.outbox.MarkMailSent(ackCtx, job.ID, job.LeaseToken)
		if err != nil {
			return dependencyError(err)
		}
		// A false result means another worker already changed the row or the
		// lease expired. The SMTP send is still valid; there is no safe local
		// state transition left to make.
		if !marked {
			d.recordMailAttempt(ackCtx, *job, MailTaskOutcomeSent, "")
			return nil
		}
		return nil
	}
	return d.handleDeliveryFailure(ackCtx, *job, sendErr)
}

// activeJobContext detaches a claimed operation from the claim-loop
// cancellation. The active job remains bounded by its lease, and
// BeginShutdown links it to the shared shutdown deadline so a first signal does
// not interrupt the send while a deadline still allows it to drain.
func (d *MailDispatcher) activeJobContext(ctx context.Context) (context.Context, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	lease := d.lease
	if lease <= 0 {
		lease = 30 * time.Second
	}
	jobCtx, cancel := boundedDetachedTimeout(ctx, lease)
	active := &activeMailDelivery{cancel: cancel}
	d.mu.Lock()
	d.active = active
	shutdownContext := d.shutdownContext
	d.mu.Unlock()
	if shutdownContext != nil {
		d.linkActiveDelivery(active, shutdownContext)
	}
	return jobCtx, func() { d.releaseActiveDelivery(active) }
}

func (d *MailDispatcher) linkActiveDelivery(active *activeMailDelivery, shutdownContext context.Context) {
	if active == nil || shutdownContext == nil {
		return
	}
	stop := context.AfterFunc(shutdownContext, active.cancel)
	d.mu.Lock()
	if d.active == active {
		active.stop = stop
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()
	stop()
}

func (d *MailDispatcher) releaseActiveDelivery(active *activeMailDelivery) {
	if active == nil {
		return
	}
	d.mu.Lock()
	if d.active != active {
		d.mu.Unlock()
		return
	}
	d.active = nil
	stop := active.stop
	d.mu.Unlock()
	if stop != nil {
		stop()
	}
	active.cancel()
}

// deliveryContext bounds SMTP and outbox acknowledgement work by both the
// existing lease and any earlier common deadline. The active job context is
// already detached from stop-claim cancellation, so this child must retain
// its cancellation rather than detaching it a second time.
func (d *MailDispatcher) deliveryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	lease := d.lease
	if lease <= 0 {
		lease = 30 * time.Second
	}
	return context.WithTimeout(ctx, lease)
}

// acknowledgementContext is deliberately independent of the SMTP/worker
// context. A timeout or shutdown cancellation must not prevent the worker from
// recording the result it already obtained. The acknowledgement remains
// bounded by the lease and deliberately ignores the parent deadline: an HTTP
// request or shutdown signal may have expired by the time the worker obtains
// an SMTP result, but the result still needs one final database attempt.
func (d *MailDispatcher) acknowledgementContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	lease := d.lease
	if lease <= 0 {
		lease = 30 * time.Second
	}
	deadline := time.Now().Add(lease)
	return context.WithDeadline(context.WithoutCancel(ctx), deadline)
}

func (d *MailDispatcher) recordMailAttempt(ctx context.Context, job MailJob, outcome, errorCode string) {
	recorder, ok := d.outbox.(MailAttemptRecorder)
	if !ok || job.ID == "" || job.Round <= 0 || job.RoundAttempts <= 0 {
		return
	}
	// The result is best effort when the task was deleted or the database is
	// unavailable. It must never make a stale worker resurrect a task or mask
	// the primary delivery result.
	_ = recorder.RecordMailAttempt(ctx, job.ID, job.Round, job.RoundAttempts, outcome, errorCode)
}

func boundedDetachedTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)
	if parentDeadline, ok := ctx.Deadline(); ok && parentDeadline.Before(deadline) {
		deadline = parentDeadline
	}
	return context.WithDeadline(context.WithoutCancel(ctx), deadline)
}

func (d *MailDispatcher) resolveMailer(ctx context.Context) (Mailer, error) {
	if d.mailerProvider != nil {
		mailer, err := d.mailerProvider.CurrentMailer(ctx)
		if err != nil {
			return nil, err
		}
		if mailer == nil {
			return nil, ErrDependencyUnavailable
		}
		return mailer, nil
	}
	if d.mailer == nil {
		return nil, ErrDependencyUnavailable
	}
	return d.mailer, nil
}

func (d *MailDispatcher) compose(ctx context.Context, job MailJob) (OutgoingMail, error) {
	if len(job.EncryptedMaterial) > 0 {
		return d.composePersistedMaterial(job)
	}
	if d.requireMaterial {
		return OutgoingMail{}, &MailDeliveryError{Code: "dependency", Temporary: true}
	}
	return d.composeLegacy(ctx, job)
}

func (d *MailDispatcher) composePersistedMaterial(job MailJob) (OutgoingMail, error) {
	if d.materialBox == nil {
		return OutgoingMail{}, &MailDeliveryError{Code: "dependency", Temporary: true}
	}
	material, err := OpenMailTaskMaterial(d.materialBox, job.EncryptedMaterial)
	if err != nil {
		return OutgoingMail{}, &MailDeliveryError{Code: "dependency", Temporary: true}
	}
	if material.Message != nil {
		message := *material.Message
		message.MessageID = "temvia-outbox-" + job.ID + "@temvia"
		return message, nil
	}
	stableJob := job
	stableJob.Kind = material.Kind
	stableJob.Name = material.Name
	stableJob.Email = material.Email
	stableJob.Locale = material.Locale
	stableJob.SystemName = material.SystemName
	stableJob.CreatedAt = material.CreatedAt
	stableJob.ExpiresAt = material.ExpiresAt
	stableJob.ResetSelector = append([]byte(nil), material.ResetSelector...)
	stableJob.EmailChangeSelector = append([]byte(nil), material.EmailSelector...)
	stableJob.VerifierDigest = append([]byte(nil), material.VerifierDigest...)
	return d.composeSnapshot(stableJob)
}

func (d *MailDispatcher) composeLegacy(ctx context.Context, job MailJob) (OutgoingMail, error) {
	identity := defaultSystemIdentityView()
	if d.identity != nil {
		loaded, err := d.identity.CurrentSystemIdentity(ctx)
		if err != nil {
			return OutgoingMail{}, err
		}
		identity = loaded
	}
	job.SystemName = identity.DisplayName()
	return d.composeSnapshot(job)
}

func (d *MailDispatcher) composeSnapshot(job MailJob) (OutgoingMail, error) {
	message := OutgoingMail{
		MessageID:  "temvia-outbox-" + job.ID + "@temvia",
		Kind:       job.Kind,
		SystemName: job.SystemName,
		Name:       job.Name,
		To:         job.Email,
		Locale:     job.Locale,
	}
	if job.Kind == MailPasswordReset {
		if len(job.ResetSelector) != domain.PasswordResetSelectorBytes || len(job.VerifierDigest) != domain.PasswordResetVerifierBytes {
			return OutgoingMail{}, ErrInvalidPasswordResetToken
		}
		material, err := domain.NewPasswordResetMaterial(d.tokenKey, job.ResetSelector)
		if err != nil || subtle.ConstantTimeCompare(material.VerifierDigest, job.VerifierDigest) != 1 {
			return OutgoingMail{}, ErrInvalidPasswordResetToken
		}
		token, err := domain.NewPasswordResetToken(d.tokenKey, job.ResetSelector)
		if err != nil {
			return OutgoingMail{}, ErrInvalidPasswordResetToken
		}
		link := d.publicURL + "/reset-password#token=" + token
		return resetMail(message, link, job.Locale, job.ExpiresAt), nil
	}
	if job.Kind == MailPasswordChanged {
		return changedMail(message, job.CreatedAt, job.Locale), nil
	}
	if job.Kind == MailEmailChanged {
		return emailChangedMail(message, job.CreatedAt, job.Locale), nil
	}
	if job.Kind == MailEmailChangeCode {
		if len(job.EmailChangeSelector) != domain.EmailChangeSelectorBytes || len(job.VerifierDigest) != domain.PasswordResetVerifierBytes || len(d.emailChangeKey) != 32 {
			return OutgoingMail{}, ErrInvalidEmailChange
		}
		_, materialDigest, err := domain.NewEmailChangeMaterial(d.emailChangeKey, job.EmailChangeSelector)
		if err != nil || subtle.ConstantTimeCompare(materialDigest, job.VerifierDigest) != 1 {
			return OutgoingMail{}, ErrInvalidEmailChange
		}
		code, err := domain.EmailChangeCode(d.emailChangeKey, job.EmailChangeSelector)
		if err != nil {
			return OutgoingMail{}, ErrInvalidEmailChange
		}
		return emailChangeCodeMail(message, code, job.Locale, job.ExpiresAt), nil
	}
	if job.Kind == MailUserInvitation {
		if len(job.ResetSelector) != 16 || len(job.VerifierDigest) != 32 || len(d.invitationKey) != 32 {
			return OutgoingMail{}, ErrInvitationInvalid
		}
		material, err := domain.NewInvitationMaterial(d.invitationKey, job.ResetSelector)
		if err != nil || subtle.ConstantTimeCompare(material.VerifierDigest, job.VerifierDigest) != 1 {
			return OutgoingMail{}, ErrInvitationInvalid
		}
		token, err := domain.NewInvitationToken(d.invitationKey, job.ResetSelector)
		if err != nil {
			return OutgoingMail{}, ErrInvitationInvalid
		}
		link := d.publicURL + "/accept-invitation#token=" + token
		return invitationMail(message, link, job.Locale, job.ExpiresAt), nil
	}
	return OutgoingMail{}, fmt.Errorf("unsupported mail kind")
}

func (d *MailDispatcher) handleDeliveryFailure(ctx context.Context, job MailJob, err error) error {
	var deliveryErr *MailDeliveryError
	if len(job.EncryptedMaterial) == 0 && !d.requireMaterial && (job.Kind == MailPasswordReset || job.Kind == MailUserInvitation || job.Kind == MailEmailChangeCode) && !d.now().Before(job.ExpiresAt) {
		discarded, updateErr := d.outbox.DiscardMail(ctx, job.ID, job.LeaseToken, "expired")
		if updateErr != nil {
			return dependencyError(updateErr)
		}
		if !discarded {
			d.recordMailAttempt(ctx, job, MailTaskOutcomeFailed, "expired")
			return nil
		}
		return nil
	}
	maxAttempts := d.maxAttempts
	if d.retryPolicy != nil {
		retries, policyErr := d.retryPolicy.CurrentMailRetryCount(ctx)
		if policyErr != nil {
			// Never substitute the default budget when the shared policy cannot
			// be read: doing so could turn an explicitly configured zero-retry
			// policy into automatic sends. Preserve the safe diagnostic and end
			// this round instead.
			deadLettered, updateErr := d.outbox.DeadLetterMail(ctx, job.ID, job.LeaseToken, "dependency")
			if updateErr != nil {
				return dependencyError(updateErr)
			}
			if !deadLettered {
				d.recordMailAttempt(ctx, job, MailTaskOutcomeFailed, "dependency")
				return nil
			}
			return nil
		}
		if retries < MailTaskMinRetryCount {
			retries = MailTaskMinRetryCount
		}
		if retries > MailTaskMaxRetryCount {
			retries = MailTaskMaxRetryCount
		}
		maxAttempts = retries + 1 // retry count excludes the initial attempt
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	roundAttempts := job.RoundAttempts
	if roundAttempts <= 0 {
		roundAttempts = job.Attempts
	}
	if errors.As(err, &deliveryErr) && deliveryErr.Temporary && roundAttempts < maxAttempts && (len(job.EncryptedMaterial) > 0 || d.now().Before(job.ExpiresAt)) {
		delayAttempt := roundAttempts
		if delayAttempt <= 0 {
			delayAttempt = job.Attempts
		}
		delay, jitterErr := d.retryDelay(delayAttempt)
		if jitterErr != nil {
			delay = d.retryInitial
		}
		if delay <= 0 {
			delay = d.retryInitial
			if delay <= 0 {
				delay = time.Second
			}
		}
		if d.retryMax > 0 && delay > d.retryMax {
			delay = d.retryMax
		}
		retryCode := "temporary"
		if deliveryErr != nil && deliveryErr.Code == "dependency" {
			retryCode = "dependency"
		}
		retried, updateErr := d.outbox.RetryMail(ctx, job.ID, job.LeaseToken, delay, retryCode)
		if updateErr != nil {
			return dependencyError(updateErr)
		}
		if !retried {
			d.recordMailAttempt(ctx, job, MailTaskOutcomeFailed, retryCode)
			return nil
		}
		return nil
	}
	code := "permanent"
	if deliveryErr != nil {
		switch deliveryErr.Code {
		case "temporary", "permanent", "expired", "superseded", "invalid_reset", "invalid_invitation", "invalid_email_change", "dependency":
			code = deliveryErr.Code
		case "not_configured":
			// Keep the persisted error vocabulary bounded; settings must be
			// explicitly repaired before this failed task is retried.
			code = "permanent"
		}
	}
	deadLettered, updateErr := d.outbox.DeadLetterMail(ctx, job.ID, job.LeaseToken, code)
	if updateErr != nil {
		return dependencyError(updateErr)
	}
	if !deadLettered {
		d.recordMailAttempt(ctx, job, MailTaskOutcomeFailed, code)
		return nil
	}
	return nil
}

func (d *MailDispatcher) retryDelay(attempt int) (time.Duration, error) {
	if d.retryInitial <= 0 {
		return 0, nil
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := d.retryInitial
	for i := 1; i < attempt && delay < d.retryMax; i++ {
		if delay > d.retryMax/2 {
			delay = d.retryMax
			break
		}
		delay *= 2
	}
	if d.retryMax > 0 && delay > d.retryMax {
		delay = d.retryMax
	}
	if delay <= 1 {
		return delay, nil
	}
	var raw [8]byte
	if err := d.random.Read(raw[:]); err != nil {
		return 0, err
	}
	return time.Duration(binary.BigEndian.Uint64(raw[:]) % uint64(delay)), nil
}

func newLeaseToken(random RandomSource) (string, error) {
	var raw [16]byte
	if err := random.Read(raw[:]); err != nil {
		return "", err
	}
	// UUIDv4 layout gives database UUID parsing plus an unpredictable lease.
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}

func resetMail(message OutgoingMail, link string, locale domain.Locale, expiresAt time.Time) OutgoingMail {
	expiry := formatMailExpiry(expiresAt)
	systemName := mailSystemName(message)
	if locale == domain.LocaleChinese {
		message.Subject = "重置 " + systemName + " 密码"
		message.Text = systemName + " · 账户安全\n\n密码恢复\n\n您好，" + message.Name + "：\n\n我们收到了一次重置 " + systemName + " 密码的请求。请使用下面的链接设置新密码。\n\n到期时间：" + expiry + "\n\n设置新密码：\n" + link + "\n\n如果这不是您发起的请求，可以忽略这封邮件。只有完成重置流程后，密码才会改变。"
		message.HTML = mailFrame(locale, systemName, "密码恢复", resetMailBody(locale, message.Name, link, expiry, systemName))
		return message
	}
	message.Subject = "Reset your " + systemName + " password"
	message.Text = systemName + " · ACCOUNT SECURITY\n\nPASSWORD RECOVERY\n\nHello " + message.Name + ",\n\nWe received a request to reset your " + systemName + " password. Use the link below to choose a new password.\n\nExpires at: " + expiry + "\n\nSet a new password:\n" + link + "\n\nIf you did not request this, you can safely ignore this email. Your password will not change unless you complete the reset."
	message.HTML = mailFrame(locale, systemName, "PASSWORD RECOVERY", resetMailBody(locale, message.Name, link, expiry, systemName))
	return message
}

func invitationMail(message OutgoingMail, link string, locale domain.Locale, expiresAt time.Time) OutgoingMail {
	expiry := formatMailExpiry(expiresAt)
	systemName := mailSystemName(message)
	safeSystemName := html.EscapeString(systemName)
	safeName := html.EscapeString(message.Name)
	safeLink := html.EscapeString(link)
	safeExpiry := html.EscapeString(expiry)
	if locale == domain.LocaleChinese {
		message.Subject = "加入 " + systemName + " 管理后台"
		message.Text = systemName + " · 管理员邀请\n\n您好，" + message.Name + "：\n\n您收到了一封 " + systemName + " 管理后台邀请。请使用下面的一次性链接设置密码并激活账户。\n\n到期时间：" + expiry + "\n\n接受邀请：\n" + link + "\n\n如果您不认识邀请方，可以忽略这封邮件。"
		message.HTML = mailFrame(locale, systemName, "管理员邀请", `<span style="display:inline-block;padding:6px 10px;border:1px solid #C7D2FE;border-radius:999px;color:#4338CA;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:11px;font-weight:700;letter-spacing:1.2px;line-height:16px;">管理员邀请</span>
<h1 style="margin:18px 0 16px;color:#101828;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:30px;font-weight:700;letter-spacing:-0.5px;line-height:38px;">接受你的邀请</h1>
<p style="margin:0 0 16px;color:#344054;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:16px;line-height:26px;">您好，`+safeName+`：</p>
<p style="margin:0 0 28px;color:#475467;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:16px;line-height:26px;">您被邀请加入 `+safeSystemName+` 管理后台。请设置密码以激活您的账户。</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 24px;"><tr><td style="padding:16px 18px;border-left:4px solid #5B5CE2;background:#EEF0FF;"><p style="margin:0 0 5px;color:#4338CA;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:11px;font-weight:700;letter-spacing:1.2px;line-height:16px;">到期时间</p><p style="margin:0;color:#101828;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:20px;font-weight:700;line-height:28px;">`+safeExpiry+`</p><p style="margin:5px 0 0;color:#475467;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:13px;line-height:20px;">链接只能使用一次。</p></td></tr></table>
<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 26px;"><tr><td bgcolor="#5B5CE2" style="border-radius:6px;background:#5B5CE2;"><a href="`+safeLink+`" style="display:inline-block;padding:13px 22px;border:1px solid #5B5CE2;border-radius:6px;color:#FFFFFF;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:16px;font-weight:700;line-height:24px;text-decoration:none;">接受邀请</a></td></tr></table>
<p style="margin:0 0 8px;color:#667085;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:13px;line-height:20px;">按钮无法打开？请将下面的链接复制到浏览器：</p><p style="margin:0 0 28px;color:#4338CA;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,&quot;Liberation Mono&quot;,&quot;Courier New&quot;,monospace;font-size:12px;line-height:19px;word-break:break-all;">`+safeLink+`</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr><td style="padding:15px 16px;border-left:4px solid #D97706;background:#FFFAEB;"><p style="margin:0;color:#7A2E0C;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:14px;line-height:22px;"><strong>不是您发起的请求？</strong><br>可以忽略这封邮件。</p></td></tr></table>`)
		return message
	}
	message.Subject = "You are invited to " + systemName
	message.Text = systemName + " · ADMIN INVITATION\n\nHello " + message.Name + ",\n\nYou have been invited to the " + systemName + " administration app. Use the one-time link below to set your password and activate your account.\n\nExpires at: " + expiry + "\n\nAccept invitation:\n" + link + "\n\nIf you do not recognize this invitation, you can safely ignore this email."
	message.HTML = mailFrame(locale, systemName, "ADMIN INVITATION", `<span style="display:inline-block;padding:6px 10px;border:1px solid #C7D2FE;border-radius:999px;color:#4338CA;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:11px;font-weight:700;letter-spacing:1.2px;line-height:16px;">ADMIN INVITATION</span>
<h1 style="margin:18px 0 16px;color:#101828;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:30px;font-weight:700;letter-spacing:-0.5px;line-height:38px;">Accept your invitation</h1>
<p style="margin:0 0 16px;color:#344054;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:16px;line-height:26px;">Hello `+safeName+`,</p>
<p style="margin:0 0 28px;color:#475467;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:16px;line-height:26px;">You have been invited to the `+safeSystemName+` administration app. Set a password to activate your account.</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 24px;"><tr><td style="padding:16px 18px;border-left:4px solid #5B5CE2;background:#EEF0FF;"><p style="margin:0 0 5px;color:#4338CA;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:11px;font-weight:700;letter-spacing:1.2px;line-height:16px;">EXPIRES AT</p><p style="margin:0;color:#101828;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:20px;font-weight:700;line-height:28px;">`+safeExpiry+`</p><p style="margin:5px 0 0;color:#475467;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:13px;line-height:20px;">This link can be used once.</p></td></tr></table>
<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 26px;"><tr><td bgcolor="#5B5CE2" style="border-radius:6px;background:#5B5CE2;"><a href="`+safeLink+`" style="display:inline-block;padding:13px 22px;border:1px solid #5B5CE2;border-radius:6px;color:#FFFFFF;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:16px;font-weight:700;line-height:24px;text-decoration:none;">Accept invitation</a></td></tr></table>
<p style="margin:0 0 8px;color:#667085;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:13px;line-height:20px;">Button not working? Copy and paste this link into your browser:</p><p style="margin:0 0 28px;color:#4338CA;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,&quot;Liberation Mono&quot;,&quot;Courier New&quot;,sans-serif;font-size:12px;line-height:19px;word-break:break-all;">`+safeLink+`</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr><td style="padding:15px 16px;border-left:4px solid #D97706;background:#FFFAEB;"><p style="margin:0;color:#7A2E0C;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:14px;line-height:22px;"><strong>Did not expect this?</strong><br>You can safely ignore this email.</p></td></tr></table>`)
	return message
}

func resetMailBody(locale domain.Locale, name, link, expiry, systemName string) string {
	safeSystemName := html.EscapeString(systemName)
	safeName := html.EscapeString(name)
	safeLink := html.EscapeString(link)
	safeExpiry := html.EscapeString(expiry)
	if locale == domain.LocaleChinese {
		return `<span style="display:inline-block;padding:6px 10px;border:1px solid #C7D2FE;border-radius:999px;color:#4338CA;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:11px;font-weight:700;letter-spacing:1.2px;line-height:16px;">密码恢复</span>
<h1 style="margin:18px 0 16px;color:#101828;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:30px;font-weight:700;letter-spacing:-0.5px;line-height:38px;">重置你的密码</h1>
<p style="margin:0 0 16px;color:#344054;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:16px;line-height:26px;">您好，` + safeName + `：</p>
<p style="margin:0 0 28px;color:#475467;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:16px;line-height:26px;">我们收到了一次重置 ` + safeSystemName + ` 密码的请求。请使用下面的按钮设置新密码。</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 24px;"><tr><td style="padding:16px 18px;border-left:4px solid #5B5CE2;background:#EEF0FF;"><p style="margin:0 0 5px;color:#4338CA;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:11px;font-weight:700;letter-spacing:1.2px;line-height:16px;">到期时间</p><p style="margin:0;color:#101828;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:20px;font-weight:700;line-height:28px;">` + safeExpiry + `</p><p style="margin:5px 0 0;color:#475467;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:13px;line-height:20px;">链接只能使用一次。</p></td></tr></table>
<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 26px;"><tr><td bgcolor="#5B5CE2" style="border-radius:6px;background:#5B5CE2;"><a href="` + safeLink + `" style="display:inline-block;padding:13px 22px;border:1px solid #5B5CE2;border-radius:6px;color:#FFFFFF;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:16px;font-weight:700;line-height:24px;text-decoration:none;">设置新密码</a></td></tr></table>
<p style="margin:0 0 8px;color:#667085;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:13px;line-height:20px;">按钮无法打开？请将下面的链接复制到浏览器：</p>
<p style="margin:0 0 28px;color:#4338CA;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,&quot;Liberation Mono&quot;,&quot;Courier New&quot;,monospace;font-size:12px;line-height:19px;word-break:break-all;">` + safeLink + `</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr><td style="padding:15px 16px;border-left:4px solid #D97706;background:#FFFAEB;"><p style="margin:0;color:#7A2E0C;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:14px;line-height:22px;"><strong>不是你发起的请求？</strong><br>可以忽略这封邮件。只有完成重置流程后，密码才会改变。</p></td></tr></table>`
	}
	return `<span style="display:inline-block;padding:6px 10px;border:1px solid #C7D2FE;border-radius:999px;color:#4338CA;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:11px;font-weight:700;letter-spacing:1.2px;line-height:16px;">PASSWORD RECOVERY</span>
<h1 style="margin:18px 0 16px;color:#101828;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:30px;font-weight:700;letter-spacing:-0.5px;line-height:38px;">Reset your password</h1>
<p style="margin:0 0 16px;color:#344054;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:16px;line-height:26px;">Hello ` + safeName + `,</p>
<p style="margin:0 0 28px;color:#475467;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:16px;line-height:26px;">We received a request to reset your ` + safeSystemName + ` password. Use the button below to choose a new password.</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 24px;"><tr><td style="padding:16px 18px;border-left:4px solid #5B5CE2;background:#EEF0FF;"><p style="margin:0 0 5px;color:#4338CA;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:11px;font-weight:700;letter-spacing:1.2px;line-height:16px;">EXPIRES AT</p><p style="margin:0;color:#101828;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:20px;font-weight:700;line-height:28px;">` + safeExpiry + `</p><p style="margin:5px 0 0;color:#475467;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:13px;line-height:20px;">This link can be used once.</p></td></tr></table>
<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 26px;"><tr><td bgcolor="#5B5CE2" style="border-radius:6px;background:#5B5CE2;"><a href="` + safeLink + `" style="display:inline-block;padding:13px 22px;border:1px solid #5B5CE2;border-radius:6px;color:#FFFFFF;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:16px;font-weight:700;line-height:24px;text-decoration:none;">Set a new password</a></td></tr></table>
<p style="margin:0 0 8px;color:#667085;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:13px;line-height:20px;">Button not working? Copy and paste this link into your browser:</p>
<p style="margin:0 0 28px;color:#4338CA;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,&quot;Liberation Mono&quot;,&quot;Courier New&quot;,monospace;font-size:12px;line-height:19px;word-break:break-all;">` + safeLink + `</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr><td style="padding:15px 16px;border-left:4px solid #D97706;background:#FFFAEB;"><p style="margin:0;color:#7A2E0C;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:14px;line-height:22px;"><strong>Did not request this?</strong><br>You can safely ignore this email. Your password will not change unless you complete the reset.</p></td></tr></table>`
}

func mailFrame(locale domain.Locale, systemName, badge, body string) string {
	lang := "en"
	sectionLabel := "ACCOUNT SECURITY"
	footerTitle := systemName + " · Account security"
	footerCopy := "This message was sent automatically."
	if locale == domain.LocaleChinese {
		lang = "zh-CN"
		sectionLabel = "账户安全"
		footerTitle = systemName + " · 账户安全"
		footerCopy = "这是一封自动发送的邮件。"
	}
	return `<!DOCTYPE html>
<html lang="` + html.EscapeString(lang) + `">
<head><meta http-equiv="Content-Type" content="text/html; charset=UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"></head>
<body style="margin:0;padding:0;background:#F4F6F8;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" bgcolor="#F4F6F8" style="width:100%;background:#F4F6F8;"><tr><td align="center" style="padding:32px 16px;">
<table role="presentation" width="600" cellpadding="0" cellspacing="0" border="0" style="width:100%;max-width:600px;">
<tr><td bgcolor="#101828" style="padding:28px 36px;background:#101828;"><table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr><td valign="top"><p style="margin:0;color:#FFFFFF;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:20px;font-weight:800;letter-spacing:1px;line-height:24px;">` + html.EscapeString(systemName) + `</p><p style="margin:7px 0 0;color:#98A2B3;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:11px;font-weight:700;letter-spacing:1.5px;line-height:16px;">` + html.EscapeString(sectionLabel) + `</p></td><td align="right" valign="top"><span style="display:inline-block;padding:5px 8px;border:1px solid #475467;border-radius:4px;color:#D0D5DD;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:10px;font-weight:700;letter-spacing:1px;line-height:14px;">` + html.EscapeString(badge) + `</span></td></tr></table></td></tr>
<tr><td bgcolor="#FFFFFF" style="padding:40px 40px 36px;background:#FFFFFF;">` + body + `</td></tr>
<tr><td bgcolor="#FFFFFF" style="padding:22px 40px 30px;border-top:1px solid #EAECF0;background:#FFFFFF;"><p style="margin:0 0 5px;color:#344054;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:13px;font-weight:700;line-height:20px;">` + html.EscapeString(footerTitle) + `</p><p style="margin:0;color:#98A2B3;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:12px;line-height:18px;">` + html.EscapeString(footerCopy) + `</p></td></tr>
</table></td></tr></table>
</body></html>`
}

func emailChangeCodeMail(message OutgoingMail, code string, locale domain.Locale, expiresAt time.Time) OutgoingMail {
	systemName := mailSystemName(message)
	expiry := formatMailExpiry(expiresAt)
	if locale == domain.LocaleChinese {
		message.Subject = systemName + " 邮箱验证码"
		message.Text = systemName + " · 账户安全\n\n邮箱变更验证码\n\n您好，" + message.Name + "：\n\n你正在修改账户邮箱。验证码为：" + code + "\n\n验证码将在 " + expiry + " 失效。请勿将验证码提供给任何人。"
		message.HTML = mailFrame(locale, systemName, "邮箱变更", `<h1 style="margin:18px 0 16px;color:#101828;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:30px;line-height:38px;">验证你的新邮箱</h1><p style="color:#475467;font-size:16px;line-height:26px;">您好，`+html.EscapeString(message.Name)+`：</p><p style="color:#475467;font-size:16px;line-height:26px;">请输入下面的验证码完成邮箱变更：</p><p style="font-family:ui-monospace,monospace;font-size:32px;font-weight:800;letter-spacing:8px;color:#101828;">`+html.EscapeString(code)+`</p><p style="color:#667085;font-size:13px;line-height:20px;">验证码有效至 `+html.EscapeString(expiry)+`。请勿转发。</p>`)
		return message
	}
	message.Subject = "Your " + systemName + " email verification code"
	message.Text = systemName + " · ACCOUNT SECURITY\n\nEMAIL CHANGE VERIFICATION\n\nHello " + message.Name + ",\n\nYour verification code is: " + code + "\n\nIt expires at " + expiry + ". Never share this code with anyone."
	message.HTML = mailFrame(locale, systemName, "EMAIL CHANGE", `<h1 style="margin:18px 0 16px;color:#101828;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:30px;line-height:38px;">Verify your new email</h1><p style="color:#475467;font-size:16px;line-height:26px;">Hello `+html.EscapeString(message.Name)+`,</p><p style="color:#475467;font-size:16px;line-height:26px;">Enter this verification code to finish changing your email:</p><p style="font-family:ui-monospace,monospace;font-size:32px;font-weight:800;letter-spacing:8px;color:#101828;">`+html.EscapeString(code)+`</p><p style="color:#667085;font-size:13px;line-height:20px;">This code expires at `+html.EscapeString(expiry)+`. Never share it.</p>`)
	return message
}

func emailChangedMail(message OutgoingMail, changedAt time.Time, locale domain.Locale) OutgoingMail {
	systemName := mailSystemName(message)
	when := changedAt.UTC().Format("2006-01-02 15:04:05 UTC")
	if locale == domain.LocaleChinese {
		message.Subject = systemName + " 邮箱已修改"
		message.Text = systemName + " · 账户安全\n\n邮箱已修改\n\n您好，" + message.Name + "：\n\n你的邮箱已于 " + when + " 修改。如果这不是你发起的操作，请立即联系管理员。"
		message.HTML = mailFrame(locale, systemName, "安全状态 · 邮箱已修改", `<h1 style="margin:18px 0 16px;color:#101828;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:30px;line-height:38px;">邮箱已修改</h1><p style="color:#475467;font-size:16px;line-height:26px;">您好，`+html.EscapeString(message.Name)+`：</p><p style="color:#475467;font-size:16px;line-height:26px;">你的账户邮箱已于 `+html.EscapeString(when)+` 修改。如果这不是你发起的操作，请立即联系管理员。</p>`)
		return message
	}
	message.Subject = "Your " + systemName + " email was changed"
	message.Text = systemName + " · ACCOUNT SECURITY\n\nEMAIL CHANGED\n\nHello " + message.Name + ",\n\nYour account email was changed at " + when + ". If you did not make this change, contact your administrator immediately."
	message.HTML = mailFrame(locale, systemName, "SECURITY STATUS · EMAIL CHANGED", `<h1 style="margin:18px 0 16px;color:#101828;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:30px;line-height:38px;">Your email was changed</h1><p style="color:#475467;font-size:16px;line-height:26px;">Hello `+html.EscapeString(message.Name)+`,</p><p style="color:#475467;font-size:16px;line-height:26px;">Your account email was changed at `+html.EscapeString(when)+`. If you did not make this change, contact your administrator immediately.</p>`)
	return message
}

func changedMailBody(locale domain.Locale, name, when, systemName string) string {
	safeSystemName := html.EscapeString(systemName)
	safeName := html.EscapeString(name)
	safeWhen := html.EscapeString(when)
	if locale == domain.LocaleChinese {
		return `<span style="display:inline-block;padding:6px 10px;border:1px solid #C7D2FE;border-radius:999px;color:#4338CA;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:11px;font-weight:700;letter-spacing:1.2px;line-height:16px;">密码已更新</span>
<h1 style="margin:18px 0 16px;color:#101828;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:30px;font-weight:700;letter-spacing:-0.5px;line-height:38px;">你的密码已修改</h1>
<p style="margin:0 0 16px;color:#344054;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:16px;line-height:26px;">您好，` + safeName + `：</p>
<p style="margin:0 0 28px;color:#475467;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:16px;line-height:26px;">你的 ` + safeSystemName + ` 密码已成功更新。以下是这次修改的记录时间。</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 28px;"><tr><td style="padding:17px 18px;border-left:4px solid #5B5CE2;background:#EEF0FF;"><p style="margin:0 0 5px;color:#4338CA;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:11px;font-weight:700;letter-spacing:1.2px;line-height:16px;">修改时间（UTC）</p><p style="margin:0;color:#101828;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,&quot;Liberation Mono&quot;,&quot;Courier New&quot;,monospace;font-size:17px;font-weight:700;line-height:26px;word-break:break-word;">` + safeWhen + `</p></td></tr></table>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr><td style="padding:16px 16px;border-left:4px solid #D97706;background:#FFFAEB;"><p style="margin:0;color:#7A2E0C;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:14px;line-height:22px;"><strong>不是你发起的操作？</strong><br>请立即联系管理员。请不要通过邮件提供密码。</p></td></tr></table>`
	}
	return `<span style="display:inline-block;padding:6px 10px;border:1px solid #C7D2FE;border-radius:999px;color:#4338CA;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:11px;font-weight:700;letter-spacing:1.2px;line-height:16px;">PASSWORD UPDATED</span>
<h1 style="margin:18px 0 16px;color:#101828;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:30px;font-weight:700;letter-spacing:-0.5px;line-height:38px;">Your password was changed</h1>
<p style="margin:0 0 16px;color:#344054;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:16px;line-height:26px;">Hello ` + safeName + `,</p>
<p style="margin:0 0 28px;color:#475467;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:16px;line-height:26px;">Your ` + safeSystemName + ` password was successfully updated. The change was recorded at the time shown below.</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 28px;"><tr><td style="padding:17px 18px;border-left:4px solid #5B5CE2;background:#EEF0FF;"><p style="margin:0 0 5px;color:#4338CA;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:11px;font-weight:700;letter-spacing:1.2px;line-height:16px;">CHANGED AT (UTC)</p><p style="margin:0;color:#101828;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,&quot;Liberation Mono&quot;,&quot;Courier New&quot;,monospace;font-size:17px;font-weight:700;line-height:26px;word-break:break-word;">` + safeWhen + `</p></td></tr></table>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr><td style="padding:16px 16px;border-left:4px solid #D97706;background:#FFFAEB;"><p style="margin:0;color:#7A2E0C;font-family:-apple-system,BlinkMacSystemFont,&quot;Segoe UI&quot;,Roboto,Helvetica,Arial,sans-serif;font-size:14px;line-height:22px;"><strong>Did not make this change?</strong><br>Contact your administrator immediately. Never share your password by email.</p></td></tr></table>`
}

func changedMail(message OutgoingMail, changedAt time.Time, locale domain.Locale) OutgoingMail {
	when := changedAt.UTC().Format("2006-01-02 15:04:05 UTC")
	systemName := mailSystemName(message)
	if locale == domain.LocaleChinese {
		message.Subject = systemName + " 密码已修改"
		message.Text = systemName + " · 账户安全\n\n密码已更新\n\n您好，" + message.Name + "：\n\n你的 " + systemName + " 密码已于 " + when + " 修改。\n\n如果这不是您发起的操作，请立即联系管理员。请不要通过邮件提供密码。"
		message.HTML = mailFrame(locale, systemName, "安全状态 · 已确认", changedMailBody(locale, message.Name, when, systemName))
		return message
	}
	message.Subject = "Your " + systemName + " password was changed"
	message.Text = systemName + " · ACCOUNT SECURITY\n\nPASSWORD UPDATED\n\nHello " + message.Name + ",\n\nYour " + systemName + " password was changed at " + when + ".\n\nIf you did not make this change, contact your administrator immediately. Never share your password by email."
	message.HTML = mailFrame(locale, systemName, "SECURITY STATUS · CONFIRMED", changedMailBody(locale, message.Name, when, systemName))
	return message
}

func testMail(message OutgoingMail) OutgoingMail {
	systemName := mailSystemName(message)
	if message.Locale == domain.LocaleChinese {
		message.Subject = systemName + " 邮件服务测试"
		message.Text = "这是一封 " + systemName + " 邮件服务测试邮件。"
		message.HTML = "<p>这是一封 " + html.EscapeString(systemName) + " 邮件服务测试邮件。</p>"
		return message
	}
	message.Subject = systemName + " email service test"
	message.Text = "This is a test email from " + systemName + "."
	message.HTML = "<p>This is a test email from " + html.EscapeString(systemName) + ".</p>"
	return message
}

func mailSystemName(message OutgoingMail) string {
	if systemName := strings.TrimSpace(message.SystemName); systemName != "" {
		return systemName
	}
	return DefaultSystemName
}

func formatMailExpiry(expiresAt time.Time) string {
	return expiresAt.UTC().Format("2006-01-02 15:04:05 UTC")
}
