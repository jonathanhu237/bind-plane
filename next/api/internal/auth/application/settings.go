package application

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

const (
	DefaultAutoRetryCount    = MailTaskDefaultRetryCount
	MinAutoRetryCount        = MailTaskMinRetryCount
	MaxAutoRetryCount        = MailTaskMaxRetryCount
	DefaultMailRetentionDays = MailTaskDefaultRetentionDays
	MinMailRetentionDays     = MailTaskMinRetentionDays
	MaxMailRetentionDays     = MailTaskMaxRetentionDays
)

// EmailSettingsRecord is the encrypted persistence projection. PasswordCiphertext
// must never be copied into an HTTP response or a browser draft.
type EmailSettingsRecord struct {
	Host               string
	Port               int
	Security           string
	Username           string
	PasswordCiphertext []byte
	FromAddress        string
	FromName           string
	DefaultLocale      domain.Locale
	AutoRetryCount     int
	RetentionDays      int
	Revision           int64
	UpdatedAt          time.Time
}

type EmailSettingsStore interface {
	GetEmailSettings(context.Context) (EmailSettingsRecord, error)
	SaveEmailSettings(context.Context, int64, EmailSettingsRecord) (EmailSettingsRecord, error)
}

type EmailSettingsView struct {
	Configured     bool
	Host           string
	Port           int
	Security       string
	Username       string
	PasswordSet    bool
	FromAddress    string
	FromName       string
	DefaultLocale  domain.Locale
	AutoRetryCount int
	RetentionDays  int
	Revision       int64
	UpdatedAt      time.Time
}

type EmailSettingsInput struct {
	Host           string
	Port           int
	Security       string
	Username       string
	Password       *string
	ClearPassword  bool
	FromAddress    string
	FromName       string
	DefaultLocale  string
	AutoRetryCount *int
	RetentionDays  *int
	Revision       int64
}

type SMTPSettings struct {
	Host        string
	Port        int
	Security    string
	Username    string
	Password    string
	FromAddress string
	FromName    string
}

type MailerFactory func(SMTPSettings) (Mailer, error)

// SettingsManagement owns the cross-device system settings lifecycle. The
// service stores only authenticated/encrypted SMTP credentials and updates a
// runtime mailer atomically after an optimistic-lock success.
type SettingsManagement struct {
	store       EmailSettingsStore
	cipher      SecretBox
	factory     MailerFactory
	runtime     *ReloadableMailer
	identity    SystemIdentityProvider
	production  bool
	mailLimiter TestEmailLimiter
	mailTasks   MailTaskStore
	// saveMu serializes the commit and runtime reload pair. Without one
	// critical section, two successful saves could reload their mailers in the
	// opposite order and leave the runtime using an older committed revision.
	saveMu sync.Mutex
}

func NewSettingsManagement(store EmailSettingsStore, cipher SecretBox, factory MailerFactory, runtime *ReloadableMailer) *SettingsManagement {
	return &SettingsManagement{store: store, cipher: cipher, factory: factory, runtime: runtime}
}

func (s *SettingsManagement) SetProductionMode(production bool) {
	if s != nil {
		s.production = production
	}
}

func (s *SettingsManagement) SetTestEmailLimiter(limiter TestEmailLimiter) {
	if s != nil {
		s.mailLimiter = limiter
	}
}

func (s *SettingsManagement) SetSystemIdentityProvider(identity SystemIdentityProvider) {
	if s != nil {
		s.identity = identity
	}
}

func (s *SettingsManagement) SetMailTaskEnqueuer(enqueuer MailTaskStore) {
	if s != nil {
		s.mailTasks = enqueuer
	}
}

func (s *SettingsManagement) GetEmailSettings(ctx context.Context) (EmailSettingsView, error) {
	if s == nil || s.store == nil {
		return EmailSettingsView{}, ErrDependencyUnavailable
	}
	record, err := s.store.GetEmailSettings(ctx)
	if errors.Is(err, ErrMailNotConfigured) {
		return EmailSettingsView{AutoRetryCount: DefaultAutoRetryCount, RetentionDays: DefaultMailRetentionDays}, nil
	}
	if err != nil {
		return EmailSettingsView{}, dependencyError(err)
	}
	return emailSettingsView(record), nil
}

func (s *SettingsManagement) SaveEmailSettings(ctx context.Context, input EmailSettingsInput) (EmailSettingsView, error) {
	if s == nil || s.store == nil {
		return EmailSettingsView{}, ErrDependencyUnavailable
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	input = normalizeEmailSettingsInput(input)
	current, currentErr := s.store.GetEmailSettings(ctx)
	hasCurrent := currentErr == nil
	if currentErr != nil && !errors.Is(currentErr, ErrMailNotConfigured) {
		return EmailSettingsView{}, dependencyError(currentErr)
	}
	if input.Revision < 0 || (hasCurrent && input.Revision != current.Revision) || (!hasCurrent && input.Revision != 0) {
		return EmailSettingsView{}, ErrStaleRevision
	}
	input = applyMailPolicyDefaults(input, current, hasCurrent)
	if err := validateEmailSettingsInput(input, s.production); err != nil {
		return EmailSettingsView{}, err
	}
	record := EmailSettingsRecord{
		Host:           strings.TrimSpace(input.Host),
		Port:           input.Port,
		Security:       strings.ToLower(strings.TrimSpace(input.Security)),
		Username:       strings.TrimSpace(input.Username),
		FromAddress:    strings.TrimSpace(input.FromAddress),
		FromName:       strings.TrimSpace(input.FromName),
		DefaultLocale:  domain.Locale(input.DefaultLocale),
		AutoRetryCount: valueOrDefault(input.AutoRetryCount, DefaultAutoRetryCount),
		RetentionDays:  valueOrDefault(input.RetentionDays, DefaultMailRetentionDays),
		Revision:       input.Revision,
	}
	password, ciphertext, err := s.resolveSMTPPassword(input, current, hasCurrent, true)
	if err != nil {
		return EmailSettingsView{}, err
	}
	record.PasswordCiphertext = ciphertext
	if !validSMTPCredentials(record.Username, password != "") {
		return EmailSettingsView{}, ErrInvalidMailSettings
	}
	// Build and validate the SMTP client before committing the row. A bad
	// connection setting must not leave a saved configuration that the runtime
	// cannot activate.
	mailer, err := s.mailerForSMTPSettings(record, password)
	if err != nil {
		return EmailSettingsView{}, err
	}
	record, err = s.store.SaveEmailSettings(ctx, input.Revision, record)
	if err != nil {
		if errors.Is(err, ErrStaleRevision) {
			return EmailSettingsView{}, ErrStaleRevision
		}
		return EmailSettingsView{}, dependencyError(err)
	}
	if s.runtime != nil && mailer != nil {
		s.runtime.Reload(mailer)
	}
	return emailSettingsView(record), nil
}

func applyMailPolicyDefaults(input EmailSettingsInput, current EmailSettingsRecord, hasCurrent bool) EmailSettingsInput {
	if input.AutoRetryCount == nil {
		value := DefaultAutoRetryCount
		if hasCurrent && current.AutoRetryCount >= MinAutoRetryCount && current.AutoRetryCount <= MaxAutoRetryCount {
			value = current.AutoRetryCount
		}
		input.AutoRetryCount = &value
	}
	if input.RetentionDays == nil {
		value := DefaultMailRetentionDays
		if hasCurrent && current.RetentionDays >= MinMailRetentionDays && current.RetentionDays <= MaxMailRetentionDays {
			value = current.RetentionDays
		}
		input.RetentionDays = &value
	}
	return input
}

func valueOrDefault(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

// CurrentMailer resolves SMTP settings from the shared persistence layer on
// every delivery. It deliberately does not return the process-local runtime
// sender: another API instance may have committed a newer revision.
func (s *SettingsManagement) CurrentMailer(ctx context.Context) (Mailer, error) {
	if s == nil || s.store == nil {
		return nil, ErrDependencyUnavailable
	}
	record, err := s.store.GetEmailSettings(ctx)
	if errors.Is(err, ErrMailNotConfigured) {
		return nil, ErrMailNotConfigured
	}
	if err != nil {
		return nil, dependencyError(err)
	}
	mailer, err := s.mailerForRecord(record)
	if err != nil {
		return nil, dependencyError(err)
	}
	if mailer == nil {
		return nil, ErrDependencyUnavailable
	}
	return mailer, nil
}

func (s *SettingsManagement) DefaultMailLocale(ctx context.Context) (domain.Locale, error) {
	view, err := s.GetEmailSettings(ctx)
	if err != nil {
		return "", err
	}
	if !view.Configured || !view.DefaultLocale.Valid() {
		return "", ErrMailNotConfigured
	}
	return view.DefaultLocale, nil
}

func (s *SettingsManagement) EnsureMailConfigured(ctx context.Context) error {
	view, err := s.GetEmailSettings(ctx)
	if err != nil {
		return err
	}
	if !view.Configured || !view.DefaultLocale.Valid() {
		return ErrMailNotConfigured
	}
	return nil
}

func (s *SettingsManagement) OperationalWarnings(ctx context.Context) ([]OperationalWarning, error) {
	view, err := s.GetEmailSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !view.Configured {
		return []OperationalWarning{{Key: "email_not_configured", Severity: "warning"}}, nil
	}
	return nil, nil
}

// LoadRuntime applies a persisted configuration during process startup. An
// empty table is a supported first-run state and leaves the runtime mailer
// disabled until an administrator saves settings.
func (s *SettingsManagement) LoadRuntime(ctx context.Context) error {
	if s == nil || s.store == nil {
		return ErrDependencyUnavailable
	}
	record, err := s.store.GetEmailSettings(ctx)
	if errors.Is(err, ErrMailNotConfigured) {
		return nil
	}
	if err != nil {
		return dependencyError(err)
	}
	return s.applyRuntime(record)
}

// TestEmailSettings submits a normal durable test task using the saved
// configuration. It intentionally does not send synchronously.
func (s *SettingsManagement) TestEmailSettings(ctx context.Context, input EmailSettingsInput, recipient string) error {
	_, err := s.TestEmailSettingsTask(ctx, "", input, recipient)
	return err
}

// TestEmailSettingsForActor applies the authenticated actor to the durable
// test task so rate limits and operation ownership remain tied to the caller.
func (s *SettingsManagement) TestEmailSettingsForActor(ctx context.Context, actorID string, input EmailSettingsInput, recipient string) error {
	_, err := s.TestEmailSettingsTask(ctx, actorID, input, recipient)
	return err
}

func (s *SettingsManagement) TestEmailSettingsTask(ctx context.Context, actorID string, input EmailSettingsInput, recipient string) (MailTask, error) {
	if s == nil {
		return MailTask{}, ErrDependencyUnavailable
	}
	if s.mailTasks == nil {
		return MailTask{}, ErrDependencyUnavailable
	}
	if s.store == nil {
		return MailTask{}, ErrDependencyUnavailable
	}
	input = normalizeEmailSettingsInput(input)
	recipient = strings.TrimSpace(recipient)
	parsedRecipient, err := mail.ParseAddress(recipient)
	if err != nil || parsedRecipient.Address != recipient || strings.ContainsAny(recipient, "\r\n") {
		return MailTask{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "recipient", Code: "invalid_email"}}}
	}
	canonicalRecipient, canonicalErr := domain.NewEmail(recipient)
	if canonicalErr != nil {
		return MailTask{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "recipient", Code: "invalid_email"}}}
	}
	current, err := s.store.GetEmailSettings(ctx)
	if errors.Is(err, ErrMailNotConfigured) {
		return MailTask{}, ErrMailNotConfigured
	}
	if err != nil {
		return MailTask{}, dependencyError(err)
	}
	// The public test endpoint submits only a recipient. Resolve the current
	// revision from the saved row instead of requiring clients to mirror SMTP
	// form fields or accidentally test an unsaved draft.
	if input.Revision == 0 && strings.TrimSpace(input.Host) == "" && input.Port == 0 && strings.TrimSpace(input.Security) == "" && strings.TrimSpace(input.Username) == "" && strings.TrimSpace(input.FromAddress) == "" && strings.TrimSpace(input.FromName) == "" && strings.TrimSpace(input.DefaultLocale) == "" {
		input.Revision = current.Revision
	}
	if input.Revision < 0 || input.Revision != current.Revision {
		return MailTask{}, ErrStaleRevision
	}
	input = savedEmailSettingsInput(input, current)
	if input.Password != nil || input.ClearPassword || !sameSavedEmailSettings(input, current) {
		return MailTask{}, ErrMailSettingsNotSaved
	}
	if err := validateSavedMailSettings(current, s.production); err != nil {
		return MailTask{}, err
	}
	if actorID != "" && s.mailLimiter != nil {
		allowed, limitErr := s.mailLimiter.AllowTestEmail(ctx, actorID, canonicalRecipient.Canonical)
		if limitErr != nil {
			return MailTask{}, dependencyError(limitErr)
		}
		if !allowed {
			return MailTask{}, ErrRateLimited
		}
	}
	systemName := DefaultSystemName
	if s.identity != nil {
		identity, identityErr := s.identity.CurrentSystemIdentity(ctx)
		if identityErr != nil {
			return MailTask{}, identityErr
		}
		systemName = identity.DisplayName()
	}
	locale := current.DefaultLocale
	message := testMail(OutgoingMail{Kind: MailTest, SystemName: systemName, Name: "administrator", To: recipient, Locale: locale})
	expires := time.Now().Add(time.Duration(current.RetentionDays) * 24 * time.Hour)
	if current.RetentionDays < MinMailRetentionDays {
		expires = time.Now().Add(time.Duration(DefaultMailRetentionDays) * 24 * time.Hour)
	}
	task, enqueueErr := s.mailTasks.EnqueueMailTask(ctx, MailTaskInput{Kind: MailTest, RecipientEmail: recipient, RecipientName: "administrator", Locale: locale, SystemName: systemName, ExpiresAt: expires, SubmittedBy: actorID, Message: &message})
	if enqueueErr != nil {
		return MailTask{}, dependencyError(enqueueErr)
	}
	return task, nil
}

func isEmptyEmailSettingsProjection(input EmailSettingsInput) bool {
	return strings.TrimSpace(input.Host) == "" && input.Port == 0 && strings.TrimSpace(input.Security) == "" && strings.TrimSpace(input.Username) == "" && strings.TrimSpace(input.FromAddress) == "" && strings.TrimSpace(input.FromName) == "" && strings.TrimSpace(input.DefaultLocale) == ""
}

func savedEmailSettingsInput(input EmailSettingsInput, current EmailSettingsRecord) EmailSettingsInput {
	if isEmptyEmailSettingsProjection(input) {
		input.Host = current.Host
		input.Port = current.Port
		input.Security = current.Security
		input.Username = current.Username
		input.FromAddress = current.FromAddress
		input.FromName = current.FromName
		input.DefaultLocale = string(current.DefaultLocale)
	}
	return input
}

func sameSavedEmailSettings(input EmailSettingsInput, current EmailSettingsRecord) bool {
	return strings.TrimSpace(input.Host) == strings.TrimSpace(current.Host) &&
		input.Port == current.Port &&
		strings.ToLower(strings.TrimSpace(input.Security)) == strings.ToLower(strings.TrimSpace(current.Security)) &&
		strings.TrimSpace(input.Username) == strings.TrimSpace(current.Username) &&
		strings.TrimSpace(input.FromAddress) == strings.TrimSpace(current.FromAddress) &&
		strings.TrimSpace(input.FromName) == strings.TrimSpace(current.FromName) &&
		domain.Locale(input.DefaultLocale) == current.DefaultLocale
}

func validateSavedMailSettings(record EmailSettingsRecord, production bool) error {
	input := EmailSettingsInput{Host: record.Host, Port: record.Port, Security: record.Security, Username: record.Username, FromAddress: record.FromAddress, FromName: record.FromName, DefaultLocale: string(record.DefaultLocale), AutoRetryCount: &record.AutoRetryCount, RetentionDays: &record.RetentionDays}
	if err := validateEmailSettingsInput(input, production); err != nil {
		return err
	}
	if !validSMTPCredentials(record.Username, len(record.PasswordCiphertext) > 0) {
		return ErrInvalidMailSettings
	}
	return nil
}

type OperationalWarning struct {
	Key      string `json:"key"`
	Severity string `json:"severity"`
}

func (s *SettingsManagement) applyRuntime(record EmailSettingsRecord) error {
	if s.runtime == nil || s.factory == nil {
		return nil
	}
	mailer, err := s.mailerForRecord(record)
	if err != nil {
		return err
	}
	if mailer != nil {
		s.runtime.Reload(mailer)
	}
	return nil
}

func (s *SettingsManagement) mailerForRecord(record EmailSettingsRecord) (Mailer, error) {
	password := ""
	if len(record.PasswordCiphertext) > 0 {
		if s.cipher == nil {
			return nil, ErrDependencyUnavailable
		}
		plain, err := s.cipher.Decrypt(record.PasswordCiphertext)
		if err != nil {
			return nil, dependencyError(err)
		}
		password = string(plain)
	}
	return s.mailerForSMTPSettings(record, password)
}

func (s *SettingsManagement) mailerForSMTPSettings(record EmailSettingsRecord, password string) (Mailer, error) {
	if s.factory == nil {
		return nil, nil
	}
	mailer, err := s.factory(SMTPSettings{Host: record.Host, Port: record.Port, Security: record.Security, Username: record.Username, Password: password, FromAddress: record.FromAddress, FromName: record.FromName})
	if err != nil {
		return nil, dependencyError(err)
	}
	return mailer, nil
}

func emailSettingsView(record EmailSettingsRecord) EmailSettingsView {
	autoRetry := record.AutoRetryCount
	if autoRetry < MinAutoRetryCount || autoRetry > MaxAutoRetryCount {
		autoRetry = DefaultAutoRetryCount
	}
	retention := record.RetentionDays
	if retention < MinMailRetentionDays || retention > MaxMailRetentionDays {
		retention = DefaultMailRetentionDays
	}
	return EmailSettingsView{Configured: true, Host: record.Host, Port: record.Port, Security: record.Security, Username: record.Username, PasswordSet: len(record.PasswordCiphertext) > 0, FromAddress: record.FromAddress, FromName: record.FromName, DefaultLocale: record.DefaultLocale, AutoRetryCount: autoRetry, RetentionDays: retention, Revision: record.Revision, UpdatedAt: record.UpdatedAt}
}

func normalizeEmailSettingsInput(input EmailSettingsInput) EmailSettingsInput {
	// An empty password from the form means "leave the saved credential
	// alone" unless the caller explicitly requested a clear. Both save and
	// test go through this normalization so they cannot disagree about the
	// meaning of an omitted password.
	if input.Password != nil && *input.Password == "" && !input.ClearPassword {
		input.Password = nil
	}
	return input
}

// sameSMTPIdentity defines the boundary at which an omitted password may be
// reused. Security mode and sender metadata are deliberately not part of the
// identity: changing those values does not send the saved secret to another
// host or login account. Hosts are compared by their supplied names rather
// than DNS resolution, and usernames use the existing trim-only normalization.
func sameSMTPIdentity(input EmailSettingsInput, current EmailSettingsRecord) bool {
	return strings.TrimSpace(input.Host) == strings.TrimSpace(current.Host) &&
		input.Port == current.Port &&
		strings.TrimSpace(input.Username) == strings.TrimSpace(current.Username)
}

// resolveSMTPPassword resolves the submitted credential exactly once for both
// the persistence and test paths. A password is reused only when the SMTP
// host, port, and username are unchanged. Changing that identity while a saved
// password exists requires an explicit replacement or clear. The resolved
// credentials must still satisfy validSMTPCredentials before use.
func (s *SettingsManagement) resolveSMTPPassword(input EmailSettingsInput, current EmailSettingsRecord, hasCurrent, persist bool) (string, []byte, error) {
	if input.ClearPassword {
		return "", nil, nil
	}
	if input.Password != nil {
		password := *input.Password
		if !persist {
			return password, nil, nil
		}
		if s.cipher == nil {
			return "", nil, ErrDependencyUnavailable
		}
		ciphertext, err := s.cipher.Encrypt([]byte(password))
		if err != nil {
			return "", nil, dependencyError(err)
		}
		return password, ciphertext, nil
	}
	if hasCurrent && !sameSMTPIdentity(input, current) && len(current.PasswordCiphertext) > 0 {
		// Dropping an old secret while changing the connection must be an
		// explicit action. This keeps the existing no-auth configuration
		// contract (clearPassword plus an empty username) visible to callers
		// instead of silently turning a credentialed connection into one.
		return "", nil, ErrInvalidMailSettings
	}
	if hasCurrent && strings.TrimSpace(input.Username) != "" && sameSMTPIdentity(input, current) && len(current.PasswordCiphertext) > 0 {
		if s.cipher == nil {
			return "", nil, ErrDependencyUnavailable
		}
		plain, err := s.cipher.Decrypt(current.PasswordCiphertext)
		if err != nil {
			return "", nil, dependencyError(err)
		}
		return string(plain), append([]byte(nil), current.PasswordCiphertext...), nil
	}
	return "", nil, nil
}

func validateEmailSettingsInput(input EmailSettingsInput, production bool) error {
	if input.AutoRetryCount == nil || *input.AutoRetryCount < MinAutoRetryCount || *input.AutoRetryCount > MaxAutoRetryCount {
		return &domain.ValidationErrors{Items: []domain.FieldError{{Field: "autoRetryCount", Code: "invalid_retry_count"}}}
	}
	if input.RetentionDays == nil || *input.RetentionDays < MinMailRetentionDays || *input.RetentionDays > MaxMailRetentionDays {
		return &domain.ValidationErrors{Items: []domain.FieldError{{Field: "retentionDays", Code: "invalid_retention_days"}}}
	}
	if strings.TrimSpace(input.Host) == "" || input.Port < 1 || input.Port > 65535 {
		return ErrInvalidMailSettings
	}
	security := strings.ToLower(strings.TrimSpace(input.Security))
	if security != "none" && security != "starttls" && security != "tls" {
		return ErrInvalidMailSettings
	}
	if production && security == "none" {
		return ErrInvalidMailSettings
	}
	if input.ClearPassword && input.Password != nil {
		return ErrInvalidMailSettings
	}
	if strings.TrimSpace(input.Username) == "" && input.Password != nil && *input.Password != "" {
		return ErrInvalidMailSettings
	}
	if strings.TrimSpace(input.Username) != "" && input.Password != nil && *input.Password == "" {
		return ErrInvalidMailSettings
	}
	if strings.TrimSpace(input.FromAddress) == "" || strings.ContainsAny(input.FromAddress, "\r\n") || strings.ContainsAny(input.FromName, "\r\n") || strings.TrimSpace(input.FromName) == "" {
		return ErrInvalidMailSettings
	}
	parsed, err := mail.ParseAddress(strings.TrimSpace(input.FromAddress))
	if err != nil || parsed.Address != strings.TrimSpace(input.FromAddress) {
		return ErrInvalidMailSettings
	}
	if !domain.Locale(input.DefaultLocale).Valid() {
		return &domain.ValidationErrors{Items: []domain.FieldError{{Field: "defaultLocale", Code: "invalid_locale"}}}
	}
	if len([]rune(input.Host)) > 255 || len([]rune(input.Username)) > 255 || len([]rune(input.FromName)) > 200 {
		return ErrInvalidMailSettings
	}
	return nil
}

// validSMTPCredentials keeps the authentication pair all-or-nothing after the
// requested preserve/replace/clear operation has been resolved. An encrypted
// password without a username would otherwise make the SMTP adapter enable
// authentication with an empty username.
func validSMTPCredentials(username string, passwordSet bool) bool {
	return (strings.TrimSpace(username) == "") == !passwordSet
}

// SecretBox is the encryption boundary for persisted settings.
type SecretBox interface {
	Encrypt([]byte) ([]byte, error)
	Decrypt([]byte) ([]byte, error)
}

type AESGCMSecretBox struct{ aead cipher.AEAD }

func NewAESGCMSecretBox(key []byte) (*AESGCMSecretBox, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &AESGCMSecretBox{aead: aead}, nil
}

func (b *AESGCMSecretBox) Encrypt(plaintext []byte) ([]byte, error) {
	if b == nil || b.aead == nil {
		return nil, ErrDependencyUnavailable
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (b *AESGCMSecretBox) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, nil
	}
	if b == nil || b.aead == nil || len(ciphertext) < b.aead.NonceSize() {
		return nil, ErrDependencyUnavailable
	}
	nonce, data := ciphertext[:b.aead.NonceSize()], ciphertext[b.aead.NonceSize():]
	return b.aead.Open(nil, nonce, data, nil)
}

// ReloadableMailer gives outbox workers a stable pointer while allowing a
// settings save to atomically replace SMTP credentials for subsequent sends.
type ReloadableMailer struct {
	mu     sync.RWMutex
	mailer Mailer
}

func NewReloadableMailer() *ReloadableMailer     { return &ReloadableMailer{} }
func (m *ReloadableMailer) Reload(mailer Mailer) { m.mu.Lock(); m.mailer = mailer; m.mu.Unlock() }
func (m *ReloadableMailer) Send(ctx context.Context, message OutgoingMail) error {
	m.mu.RLock()
	mailer := m.mailer
	m.mu.RUnlock()
	if mailer == nil {
		return &MailDeliveryError{Code: "not_configured", Temporary: false}
	}
	return mailer.Send(ctx, message)
}
