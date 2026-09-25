package application

import (
	"context"
	"errors"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

const passwordResetSelectorBytes = domain.PasswordResetSelectorBytes

type PasswordResetRequestInput struct {
	Email string
	// SourceIP is supplied by the trusted HTTP adapter, never by JSON input.
	SourceIP string
}

type PasswordResetCompleteInput struct {
	Token    string
	Password string
	// SourceIP is supplied by the trusted HTTP adapter, never by JSON input.
	SourceIP string
}

// PasswordResetTargetStore is an optional audit seam. It returns only the
// account identity bound to a valid reset digest; the reset token and password
// material never leave the recovery service.
type PasswordResetTargetStore interface {
	FindPasswordResetTarget(context.Context, []byte, []byte) (domain.User, error)
}

type PasswordRecovery struct {
	store        PasswordResetStore
	limiter      PasswordResetLimiter
	hasher       PasswordHasher
	random       RandomSource
	tokenKey     []byte
	linkTTL      time.Duration
	noticeTTL    time.Duration
	minResponse  time.Duration
	now          func() time.Time
	sleep        func(time.Duration)
	mailSettings MailSettingsProvider
}

func NewPasswordRecovery(store PasswordResetStore, limiter PasswordResetLimiter, hasher PasswordHasher, random RandomSource, tokenKey []byte, linkTTL, noticeTTL, minResponse time.Duration, mailSettings ...MailSettingsProvider) *PasswordRecovery {
	recovery := &PasswordRecovery{
		store:       store,
		limiter:     limiter,
		hasher:      hasher,
		random:      random,
		tokenKey:    append([]byte(nil), tokenKey...),
		linkTTL:     linkTTL,
		noticeTTL:   noticeTTL,
		minResponse: minResponse,
		now:         time.Now,
		sleep:       time.Sleep,
	}
	if len(mailSettings) > 0 {
		recovery.mailSettings = mailSettings[0]
	}
	return recovery
}

// NewPasswordRecoveryWithClock makes the response-timing behavior deterministic
// in application tests without exposing timing controls to production wiring.
func NewPasswordRecoveryWithClock(store PasswordResetStore, limiter PasswordResetLimiter, hasher PasswordHasher, random RandomSource, tokenKey []byte, linkTTL, noticeTTL, minResponse time.Duration, now func() time.Time, sleep func(time.Duration)) *PasswordRecovery {
	recovery := NewPasswordRecovery(store, limiter, hasher, random, tokenKey, linkTTL, noticeTTL, minResponse)
	if now != nil {
		recovery.now = now
	}
	if sleep != nil {
		recovery.sleep = sleep
	}
	return recovery
}

func (r *PasswordRecovery) Request(ctx context.Context, input PasswordResetRequestInput) error {
	started := r.now()
	email, err := domain.NewEmail(input.Email)
	if err != nil {
		return err
	}
	if r.limiter == nil || r.store == nil || r.random == nil {
		return ErrDependencyUnavailable
	}
	// Check the source/object/resource buckets before reading mail settings or
	// generating reset material. Unknown addresses still take the same path
	// after this boundary, while rejected attempts have no reset side effect.
	allowed, err := r.allowRequest(ctx, input.SourceIP, email.Canonical)
	if err != nil {
		return dependencyError(err)
	}
	if !allowed {
		return ErrRateLimited
	}
	locale, err := r.mailLocale(ctx)
	if err != nil {
		return err
	}

	// This selector/HMAC work happens for both known and unknown addresses. The
	// store owns the conditional account lookup and never returns its outcome.
	selector := make([]byte, passwordResetSelectorBytes)
	if err := r.random.Read(selector); err != nil {
		return dependencyError(err)
	}
	material, err := domain.NewPasswordResetMaterial(r.tokenKey, selector)
	if err != nil {
		return dependencyError(err)
	}
	if err := r.store.RequestPasswordReset(ctx, email.Canonical, material.Selector, material.VerifierDigest, r.linkTTL, locale); err != nil {
		return dependencyError(err)
	}
	r.waitMinimum(started)
	return nil
}

func (r *PasswordRecovery) Complete(ctx context.Context, input PasswordResetCompleteInput) error {
	_, err := r.CompleteWithTarget(ctx, input)
	return err
}

func (r *PasswordRecovery) CompleteWithTarget(ctx context.Context, input PasswordResetCompleteInput) (domain.User, error) {
	if allowed, err := r.allowCompletion(ctx, input.SourceIP); err != nil {
		return domain.User{}, dependencyError(err)
	} else if !allowed {
		return domain.User{}, ErrRateLimited
	}
	selector, verifierDigest, ok := domain.ParsePasswordResetToken(input.Token)
	if !ok {
		return domain.User{}, ErrInvalidPasswordResetToken
	}
	var err error
	var target domain.User
	if targetStore, ok := r.store.(PasswordResetTargetStore); ok {
		target, err = targetStore.FindPasswordResetTarget(ctx, selector, verifierDigest)
		if err != nil {
			if isPasswordResetTokenError(err) {
				return domain.User{}, ErrInvalidPasswordResetToken
			}
			return domain.User{}, dependencyError(err)
		}
	}
	locale, err := r.mailLocale(ctx)
	if err != nil {
		return domain.User{}, err
	}
	if r.store == nil {
		return domain.User{}, ErrDependencyUnavailable
	}
	// Preflight is deliberately cheap and happens before Argon2 work. The
	// transaction revalidates the same digest under a row lock after hashing.
	if err := r.store.PreflightPasswordReset(ctx, selector, verifierDigest); err != nil {
		switch {
		case isPasswordResetTokenError(err):
			return domain.User{}, ErrInvalidPasswordResetToken
		default:
			return domain.User{}, dependencyError(err)
		}
	}
	password, err := domain.NewPassword(input.Password)
	if err != nil {
		return domain.User{}, err
	}
	if r.hasher == nil {
		return domain.User{}, ErrDependencyUnavailable
	}
	hash, err := r.hasher.Hash(ctx, string(password))
	if err != nil {
		if errors.Is(err, ErrPasswordHashBusy) {
			return domain.User{}, ErrDependencyUnavailable
		}
		return domain.User{}, dependencyError(err)
	}
	if _, err := r.store.CompletePasswordReset(ctx, selector, verifierDigest, hash, locale, r.noticeTTL); err != nil {
		if isPasswordResetTokenError(err) {
			return domain.User{}, ErrInvalidPasswordResetToken
		}
		return domain.User{}, dependencyError(err)
	}
	return target, nil
}

func (r *PasswordRecovery) allowRequest(ctx context.Context, sourceIP, canonicalEmail string) (bool, error) {
	if sourceAware, ok := r.limiter.(SourceAwarePasswordResetLimiter); ok {
		return sourceAware.AllowPasswordResetFromSource(ctx, sourceIP, canonicalEmail)
	}
	if r.limiter == nil {
		return false, ErrDependencyUnavailable
	}
	return r.limiter.AllowPasswordReset(ctx, canonicalEmail)
}

func (r *PasswordRecovery) allowCompletion(ctx context.Context, sourceIP string) (bool, error) {
	if sourceAware, ok := r.limiter.(SourceAwarePasswordResetLimiter); ok {
		return sourceAware.AllowPasswordResetComplete(ctx, sourceIP)
	}
	// Existing embedders may not yet expose a completion bucket. The production
	// PostgreSQL store does, while compatibility fakes retain the old seam.
	return true, nil
}

func (r *PasswordRecovery) mailLocale(ctx context.Context) (domain.Locale, error) {
	if r.mailSettings != nil {
		if err := r.mailSettings.EnsureMailConfigured(ctx); err != nil {
			return "", err
		}
		return r.mailSettings.DefaultMailLocale(ctx)
	}
	return domain.LocaleEnglish, nil
}

func (r *PasswordRecovery) waitMinimum(started time.Time) {
	if r.minResponse <= 0 || r.sleep == nil {
		return
	}
	remaining := r.minResponse - r.now().Sub(started)
	if remaining > 0 {
		r.sleep(remaining)
	}
}

func isPasswordResetTokenError(err error) bool {
	return errors.Is(err, ErrInvalidPasswordResetToken)
}
