package application

import (
	"context"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type SetupStore interface {
	Status(context.Context) (bool, error)
	ReplaceCurrentToken(context.Context, []byte, time.Duration) (complete bool, err error)
	PreflightToken(context.Context, []byte) error
	Complete(context.Context, []byte, domain.Name, domain.Email, string) (domain.User, error)
}

// SetupLocaleStore is an additive seam used by the PostgreSQL adapter to
// persist the first administrator's account language atomically with setup.
type SetupLocaleStore interface {
	CompleteWithLocale(context.Context, []byte, domain.Name, domain.Email, string, domain.Locale) (domain.User, error)
}

// AccountLocaleStore initializes legacy NULL locales only after successful
// password verification. A non-NULL locale is never overwritten at login.
type AccountLocaleStore interface {
	InitializeLocale(context.Context, string, domain.Locale) (domain.Account, error)
}

type AccountStore interface {
	FindByCanonicalEmail(context.Context, string) (domain.Account, error)
	FindPublicByID(context.Context, string) (domain.User, error)
}

// VersionedAccountStore is implemented by stores that participate in
// password-reset session revocation. AccountStore remains intentionally
// narrow so existing adapters and tests can still resolve public users.
type VersionedAccountStore interface {
	FindPublicAccountByID(context.Context, string) (domain.Account, error)
}

type PasswordHasher interface {
	Hash(context.Context, string) (string, error)
	Verify(context.Context, string, string) (bool, error)
}

type SessionStore interface {
	Create(context.Context, string, string) error
	ResolveAndTouch(context.Context, string) (userID string, err error)
	Delete(context.Context, string) error
}

type VersionedSessionStore interface {
	CreateVersioned(context.Context, string, string, int64) error
	ResolveAndTouchVersioned(context.Context, string) (userID string, authVersion int64, err error)
}

// ReadOnlySessionStore resolves a session without changing its last activity
// or expiry. It is used by background status checks and online-user polling.
type ReadOnlySessionStore interface {
	Resolve(context.Context, string) (userID string, err error)
}

// ReadOnlyVersionedSessionStore is the read-only counterpart to
// VersionedSessionStore. The account's authentication version remains the
// revocation authority and is checked by Authentication after resolution.
type ReadOnlyVersionedSessionStore interface {
	ResolveVersioned(context.Context, string) (userID string, authVersion int64, err error)
}

type LoginLimiter interface {
	Allow(context.Context, string) (bool, error)
	ResetEmail(context.Context, string) error
}

// SourceAwareLoginLimiter adds a separately throttled source bucket to the
// legacy login limiter. Keeping the small legacy interface intact preserves
// embedders that only provide the original email/global policy.
type SourceAwareLoginLimiter interface {
	AllowLogin(context.Context, string, string) (bool, error)
}

type PasswordResetLimiter interface {
	AllowPasswordReset(context.Context, string) (bool, error)
}

type SourceAwarePasswordResetLimiter interface {
	AllowPasswordResetFromSource(context.Context, string, string) (bool, error)
	AllowPasswordResetComplete(context.Context, string) (bool, error)
}

type SetupLimiter interface {
	AllowSetup(context.Context, string) (bool, error)
}

type InvitationAcceptLimiter interface {
	AllowInvitationAccept(context.Context, string) (bool, error)
}

type InvitationSendLimiter interface {
	AllowInvitationSend(context.Context, string, string) (bool, error)
}

type TestEmailLimiter interface {
	AllowTestEmail(context.Context, string, string) (bool, error)
}

type PasswordResetStore interface {
	RequestPasswordReset(context.Context, string, []byte, []byte, time.Duration, domain.Locale) error
	PreflightPasswordReset(context.Context, []byte, []byte) error
	CompletePasswordReset(context.Context, []byte, []byte, string, domain.Locale, time.Duration) (time.Time, error)
}

type MailKind string

const (
	MailPasswordReset   MailKind = "password_reset"
	MailPasswordChanged MailKind = "password_changed"
	MailUserInvitation  MailKind = "user_invitation"
	MailEmailChangeCode MailKind = "email_change_code"
	MailEmailChanged    MailKind = "email_changed"
	MailTest            MailKind = "test_email"
)

type MailJob struct {
	ID                       string
	Kind                     MailKind
	UserID                   string
	InvitationID             string
	Name                     string
	Email                    string
	SystemName               string
	Locale                   domain.Locale
	ResetSelector            []byte
	EmailChangeSelector      []byte
	VerifierDigest           []byte
	InvitationSelector       []byte
	InvitationVerifierDigest []byte
	EmailChangeRequestID     string
	EncryptedMaterial        []byte
	Attempts                 int
	Round                    int
	RoundAttempts            int
	LeaseToken               string
	CreatedAt                time.Time
	ExpiresAt                time.Time
}

type MailOutboxStore interface {
	ClaimMail(context.Context, string, time.Duration) (*MailJob, error)
	MarkMailSent(context.Context, string, string) (bool, error)
	RetryMail(context.Context, string, string, time.Duration, string) (bool, error)
	DeadLetterMail(context.Context, string, string, string) (bool, error)
	DiscardMail(context.Context, string, string, string) (bool, error)
	SweepMail(context.Context) error
	CleanupMail(context.Context) error
}

// MailAttemptRecorder is an optional recovery seam for a worker which loses
// its lease before it can commit the send result. Recording by the immutable
// task/round/attempt identity never changes task state, so a late result cannot
// overwrite a newer round; a deleted task simply rejects the insert through
// the task foreign key.
type MailAttemptRecorder interface {
	RecordMailAttempt(context.Context, string, int, int, string, string) error
}

type OutgoingMail struct {
	MessageID  string        `json:"messageId"`
	Kind       MailKind      `json:"kind"`
	SystemName string        `json:"systemName"`
	Name       string        `json:"name"`
	To         string        `json:"to"`
	Locale     domain.Locale `json:"locale"`
	ChangedAt  time.Time     `json:"changedAt,omitempty"`
	Subject    string        `json:"subject"`
	Text       string        `json:"text"`
	HTML       string        `json:"html"`
}

type Mailer interface {
	Send(context.Context, OutgoingMail) error
}

// MailerProvider resolves the currently persisted SMTP configuration for each
// delivery. Implementations must not return credentials or configuration to
// callers other than the returned sender. Resolving per task keeps workers in
// separate processes from using a stale in-memory SMTP client after settings
// change.
type MailerProvider interface {
	CurrentMailer(context.Context) (Mailer, error)
}

type MailDeliveryError struct {
	Code      string
	Temporary bool
}

func (e *MailDeliveryError) Error() string {
	if e == nil {
		return "mail delivery failed"
	}
	return "mail delivery " + e.Code
}

type RandomSource interface {
	Read([]byte) error
}

// MailSettingsProvider supplies the current cross-device email configuration.
// It is intentionally narrow so invitations and password recovery can use the
// same default locale without depending on the settings HTTP adapter.
type MailSettingsProvider interface {
	DefaultMailLocale(context.Context) (domain.Locale, error)
	EnsureMailConfigured(context.Context) error
}
