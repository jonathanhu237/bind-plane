package application

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"math"
	"strings"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	imageDraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	// Avatar uploads are deliberately small and are never retained in their
	// original form. The limits apply before decoding as well as to decoded
	// dimensions so a compressed image cannot exhaust process memory.
	MaxAvatarBytes     = 5 * 1024 * 1024
	MaxAvatarDimension = 8192
	MaxAvatarPixels    = 16 * 1024 * 1024
	AvatarSide         = 256

	EmailChangeValidity       = 10 * time.Minute
	EmailChangeResendInterval = 60 * time.Second
	EmailChangeMaxAttempts    = 5
)

// PersonalProfileStore contains only self-service profile mutations. The
// target user is always supplied by the authenticated session at the HTTP
// boundary; no method accepts an administrator-selected target.
type PersonalProfileStore interface {
	UpdateUserName(context.Context, string, domain.Name) (domain.User, error)
	SaveUserAvatar(context.Context, string, []byte, string) (domain.Avatar, error)
	DeleteUserAvatar(context.Context, string) error
	GetUserAvatar(context.Context, string) (domain.Avatar, error)
}

// PasswordChangeStore performs the password replacement and auth-version
// increment in one transaction. It also queues the notification in the same
// transaction, so a committed password always has a durable notification task.
type PersonalLocaleStore interface {
	UpdateUserLocale(context.Context, string, domain.Locale) (domain.User, error)
}

type PasswordChangeStore interface {
	ChangePassword(context.Context, string, int64, string, domain.Locale, time.Duration) (domain.User, time.Time, error)
}

// EmailChangeStore owns the independent email-change authority. Its request
// rows are not session rows and therefore survive logout, force sign-out, and
// password changes until expiry, replacement, consumption, or deletion.
type EmailChangeStore interface {
	GetEmailChangeRequest(context.Context, string) (domain.EmailChangeRequest, error)
	RequestEmailChange(context.Context, string, domain.Email, []byte, []byte, time.Duration, domain.Locale) (domain.EmailChangeRequest, error)
	ResendEmailChange(context.Context, string, []byte, []byte, time.Duration, domain.Locale) (domain.EmailChangeRequest, error)
	CompleteEmailChange(context.Context, string, string, []byte, domain.Locale, time.Duration) (domain.User, time.Time, error)
}

// EmailChangeLimiter is an optional authenticated abuse-protection seam. The
// PostgreSQL adapter implements it with actor and recipient buckets; small
// embedders may omit it without changing the core workflow.
type EmailChangeLimiter interface {
	AllowEmailChange(context.Context, string, string) (bool, error)
}

type PersonalSettings struct {
	accounts        AccountStore
	profile         PersonalProfileStore
	passwords       PasswordChangeStore
	email           EmailChangeStore
	hasher          PasswordHasher
	random          RandomSource
	codeKey         []byte
	mailSettings    MailSettingsProvider
	notificationTTL time.Duration
	emailChangeTTL  time.Duration
	emailLimiter    EmailChangeLimiter
}

func NewPersonalSettings(accounts AccountStore, profile PersonalProfileStore, passwords PasswordChangeStore, email EmailChangeStore, hasher PasswordHasher, random RandomSource, codeKey []byte, notificationTTL time.Duration, mailSettings ...MailSettingsProvider) *PersonalSettings {
	service := &PersonalSettings{
		accounts:        accounts,
		profile:         profile,
		passwords:       passwords,
		email:           email,
		hasher:          hasher,
		random:          random,
		codeKey:         append([]byte(nil), codeKey...),
		notificationTTL: notificationTTL,
		emailChangeTTL:  EmailChangeValidity,
	}
	if len(mailSettings) > 0 {
		service.mailSettings = mailSettings[0]
	}
	return service
}

func (s *PersonalSettings) SetEmailChangeLimiter(limiter EmailChangeLimiter) {
	if s != nil {
		s.emailLimiter = limiter
	}
}

func (s *PersonalSettings) Profile(ctx context.Context, userID string) (domain.User, error) {
	account, err := s.account(ctx, userID)
	if err != nil {
		return domain.User{}, err
	}
	return account.User, nil
}

func (s *PersonalSettings) UpdateName(ctx context.Context, userID, value string) (domain.User, error) {
	if s == nil || s.profile == nil {
		return domain.User{}, ErrDependencyUnavailable
	}
	name, err := domain.NewName(value)
	if err != nil {
		return domain.User{}, err
	}
	if !domain.IsCanonicalUUID(userID) {
		return domain.User{}, ErrAccountNotFound
	}
	updated, err := s.profile.UpdateUserName(ctx, userID, name)
	if err != nil {
		return domain.User{}, dependencyError(err)
	}
	return updated, nil
}

func (s *PersonalSettings) UpdateLocale(ctx context.Context, userID string, locale domain.Locale) (domain.User, error) {
	if s == nil || s.profile == nil {
		return domain.User{}, ErrDependencyUnavailable
	}
	localized, ok := s.profile.(PersonalLocaleStore)
	if !ok {
		return domain.User{}, ErrDependencyUnavailable
	}
	if !domain.IsCanonicalUUID(userID) {
		return domain.User{}, ErrAccountNotFound
	}
	if !locale.Valid() {
		return domain.User{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "locale", Code: "invalid_locale"}}}
	}
	updated, err := localized.UpdateUserLocale(ctx, userID, locale)
	if err != nil {
		return domain.User{}, dependencyError(err)
	}
	return updated, nil
}

func (s *PersonalSettings) SaveAvatar(ctx context.Context, userID string, data []byte, mediaType string) (domain.User, error) {
	if s == nil || s.profile == nil {
		return domain.User{}, ErrDependencyUnavailable
	}
	if !domain.IsCanonicalUUID(userID) {
		return domain.User{}, ErrAccountNotFound
	}
	encoded, normalizedType, err := NormalizeAvatar(data, mediaType)
	if err != nil {
		return domain.User{}, err
	}
	profile, err := s.account(ctx, userID)
	if err != nil {
		return domain.User{}, err
	}
	avatar, err := s.profile.SaveUserAvatar(ctx, userID, encoded, normalizedType)
	if err != nil {
		return domain.User{}, dependencyError(err)
	}
	profile.User.HasAvatar = true
	profile.User.AvatarVersion = avatar.Version
	return profile.User, nil
}

func (s *PersonalSettings) RemoveAvatar(ctx context.Context, userID string) (domain.User, error) {
	if s == nil || s.profile == nil {
		return domain.User{}, ErrDependencyUnavailable
	}
	if !domain.IsCanonicalUUID(userID) {
		return domain.User{}, ErrAccountNotFound
	}
	profile, err := s.account(ctx, userID)
	if err != nil {
		return domain.User{}, err
	}
	if err := s.profile.DeleteUserAvatar(ctx, userID); err != nil {
		return domain.User{}, dependencyError(err)
	}
	profile.User.HasAvatar = false
	profile.User.AvatarVersion = 0
	return profile.User, nil
}

func (s *PersonalSettings) Avatar(ctx context.Context, userID string) (domain.Avatar, error) {
	if s == nil || s.profile == nil {
		return domain.Avatar{}, ErrDependencyUnavailable
	}
	if !domain.IsCanonicalUUID(userID) {
		return domain.Avatar{}, ErrAccountNotFound
	}
	avatar, err := s.profile.GetUserAvatar(ctx, userID)
	if errors.Is(err, ErrAvatarNotFound) {
		return domain.Avatar{}, err
	}
	if err != nil {
		return domain.Avatar{}, dependencyError(err)
	}
	return avatar, nil
}

// ChangePassword verifies the current password before doing the expensive hash
// and then lets the store compare the auth version under its transaction lock.
// A concurrent password/email/session revocation therefore cannot be silently
// overwritten by a stale update.
func (s *PersonalSettings) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) (domain.User, time.Time, error) {
	if s == nil || s.accounts == nil || s.passwords == nil || s.hasher == nil {
		return domain.User{}, time.Time{}, ErrDependencyUnavailable
	}
	account, err := s.account(ctx, userID)
	if err != nil {
		return domain.User{}, time.Time{}, err
	}
	current, err := domain.NewLoginPassword(currentPassword)
	if err != nil {
		return domain.User{}, time.Time{}, err
	}
	valid, err := s.hasher.Verify(ctx, account.PasswordHash, string(current))
	if err != nil {
		if errors.Is(err, ErrPasswordHashBusy) {
			return domain.User{}, time.Time{}, ErrDependencyUnavailable
		}
		return domain.User{}, time.Time{}, dependencyError(err)
	}
	if !valid {
		return domain.User{}, time.Time{}, ErrInvalidCredentials
	}
	password, err := domain.NewPassword(newPassword)
	if err != nil {
		return domain.User{}, time.Time{}, err
	}
	hash, err := s.hasher.Hash(ctx, string(password))
	if err != nil {
		if errors.Is(err, ErrPasswordHashBusy) {
			return domain.User{}, time.Time{}, ErrDependencyUnavailable
		}
		return domain.User{}, time.Time{}, dependencyError(err)
	}
	locale := s.securityMailLocale(ctx)
	changed, changedAt, err := s.passwords.ChangePassword(ctx, userID, account.AuthVersion, hash, locale, s.notificationTTL)
	if err != nil {
		return domain.User{}, time.Time{}, dependencyError(err)
	}
	return changed, changedAt, nil
}

func (s *PersonalSettings) EmailChangeStatus(ctx context.Context, userID string) (domain.EmailChangeRequest, error) {
	if s == nil || s.email == nil {
		return domain.EmailChangeRequest{}, ErrDependencyUnavailable
	}
	if !domain.IsCanonicalUUID(userID) {
		return domain.EmailChangeRequest{}, ErrAccountNotFound
	}
	request, err := s.email.GetEmailChangeRequest(ctx, userID)
	if errors.Is(err, ErrEmailChangeNotFound) {
		return domain.EmailChangeRequest{}, nil
	}
	if err != nil {
		return domain.EmailChangeRequest{}, dependencyError(err)
	}
	return sanitizeEmailChangeRequest(request), nil
}

func (s *PersonalSettings) RequestEmailChange(ctx context.Context, userID, currentPassword, newEmail string) (domain.EmailChangeRequest, error) {
	if s == nil || s.accounts == nil || s.email == nil || s.hasher == nil || s.random == nil || len(s.codeKey) != 32 {
		return domain.EmailChangeRequest{}, ErrDependencyUnavailable
	}
	account, err := s.account(ctx, userID)
	if err != nil {
		return domain.EmailChangeRequest{}, err
	}
	current, err := domain.NewLoginPassword(currentPassword)
	if err != nil {
		return domain.EmailChangeRequest{}, err
	}
	valid, err := s.hasher.Verify(ctx, account.PasswordHash, string(current))
	if err != nil {
		if errors.Is(err, ErrPasswordHashBusy) {
			return domain.EmailChangeRequest{}, ErrDependencyUnavailable
		}
		return domain.EmailChangeRequest{}, dependencyError(err)
	}
	if !valid {
		return domain.EmailChangeRequest{}, ErrInvalidCredentials
	}
	email, err := domain.NewEmail(newEmail)
	if err != nil {
		return domain.EmailChangeRequest{}, err
	}
	if strings.EqualFold(email.Canonical, account.User.Email) {
		return domain.EmailChangeRequest{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "newEmail", Code: "same_email"}}}
	}
	if err := s.requireMailConfigured(ctx); err != nil {
		return domain.EmailChangeRequest{}, err
	}
	if err := s.allowEmailChange(ctx, userID, email.Canonical); err != nil {
		return domain.EmailChangeRequest{}, err
	}
	selector := make([]byte, domain.EmailChangeSelectorBytes)
	if err := s.random.Read(selector); err != nil {
		return domain.EmailChangeRequest{}, dependencyError(err)
	}
	_, digest, err := domain.NewEmailChangeMaterial(s.codeKey, selector)
	if err != nil {
		return domain.EmailChangeRequest{}, dependencyError(err)
	}
	request, err := s.email.RequestEmailChange(ctx, userID, email, selector, digest, s.emailChangeTTL, s.securityMailLocale(ctx))
	if err != nil {
		return domain.EmailChangeRequest{}, dependencyError(err)
	}
	return sanitizeEmailChangeRequest(request), nil
}

func (s *PersonalSettings) ResendEmailChange(ctx context.Context, userID string) (domain.EmailChangeRequest, error) {
	if s == nil || s.email == nil || s.random == nil || len(s.codeKey) != 32 {
		return domain.EmailChangeRequest{}, ErrDependencyUnavailable
	}
	if !domain.IsCanonicalUUID(userID) {
		return domain.EmailChangeRequest{}, ErrAccountNotFound
	}
	current, err := s.email.GetEmailChangeRequest(ctx, userID)
	if err != nil {
		return domain.EmailChangeRequest{}, dependencyError(err)
	}
	if current.AttemptsRemaining <= 0 {
		return domain.EmailChangeRequest{}, ErrEmailChangeAttemptsExceeded
	}
	if err := s.requireMailConfigured(ctx); err != nil {
		return domain.EmailChangeRequest{}, err
	}
	newEmail, err := domain.NewEmail(current.NewEmail)
	if err != nil {
		return domain.EmailChangeRequest{}, ErrInvalidEmailChange
	}
	if err := s.allowEmailChange(ctx, userID, newEmail.Canonical); err != nil {
		return domain.EmailChangeRequest{}, err
	}
	selector := make([]byte, domain.EmailChangeSelectorBytes)
	if err := s.random.Read(selector); err != nil {
		return domain.EmailChangeRequest{}, dependencyError(err)
	}
	_, digest, err := domain.NewEmailChangeMaterial(s.codeKey, selector)
	if err != nil {
		return domain.EmailChangeRequest{}, dependencyError(err)
	}
	request, err := s.email.ResendEmailChange(ctx, userID, selector, digest, s.emailChangeTTL, s.securityMailLocale(ctx))
	if err != nil {
		return domain.EmailChangeRequest{}, dependencyError(err)
	}
	return sanitizeEmailChangeRequest(request), nil
}

func (s *PersonalSettings) CompleteEmailChange(ctx context.Context, userID, requestID, code string) (domain.User, time.Time, error) {
	if s == nil || s.email == nil || len(s.codeKey) != 32 {
		return domain.User{}, time.Time{}, ErrDependencyUnavailable
	}
	if !domain.IsCanonicalUUID(userID) {
		return domain.User{}, time.Time{}, ErrAccountNotFound
	}
	if !domain.IsCanonicalUUID(requestID) {
		return domain.User{}, time.Time{}, ErrInvalidEmailChange
	}
	if !validEmailChangeCode(code) {
		return domain.User{}, time.Time{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "code", Code: "invalid_code"}}}
	}
	request, err := s.email.GetEmailChangeRequest(ctx, userID)
	if err != nil {
		return domain.User{}, time.Time{}, dependencyError(err)
	}
	if request.ID == "" || request.ID != requestID || len(request.Selector) != domain.EmailChangeSelectorBytes {
		return domain.User{}, time.Time{}, ErrInvalidEmailChange
	}
	presentedDigest, err := domain.EmailChangePresentedDigest(s.codeKey, request.Selector, code)
	if err != nil {
		return domain.User{}, time.Time{}, dependencyError(err)
	}
	// The store compares the keyed digest supplied here against the locked
	// request row and decrements attempts on a mismatch. The code itself never
	// crosses the persistence boundary.
	updated, changedAt, err := s.email.CompleteEmailChange(ctx, userID, requestID, presentedDigest, s.securityMailLocale(ctx), s.notificationTTL)
	if err != nil {
		return domain.User{}, time.Time{}, dependencyError(err)
	}
	return updated, changedAt, nil
}

func sanitizeEmailChangeRequest(request domain.EmailChangeRequest) domain.EmailChangeRequest {
	request.UserID = ""
	request.Selector = nil
	request.VerifierDigest = nil
	return request
}

func validEmailChangeCode(value string) bool {
	if len(value) != 6 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func (s *PersonalSettings) account(ctx context.Context, userID string) (domain.Account, error) {
	if s == nil || s.accounts == nil {
		return domain.Account{}, ErrDependencyUnavailable
	}
	if !domain.IsCanonicalUUID(userID) {
		return domain.Account{}, ErrAccountNotFound
	}
	accounts, ok := s.accounts.(VersionedAccountStore)
	if !ok {
		return domain.Account{}, ErrDependencyUnavailable
	}
	account, err := accounts.FindPublicAccountByID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			return domain.Account{}, err
		}
		return domain.Account{}, dependencyError(err)
	}
	return account, nil
}

func (s *PersonalSettings) requireMailConfigured(ctx context.Context) error {
	if s.mailSettings == nil {
		return nil
	}
	return s.mailSettings.EnsureMailConfigured(ctx)
}

func (s *PersonalSettings) securityMailLocale(ctx context.Context) domain.Locale {
	if s.mailSettings != nil {
		if locale, err := s.mailSettings.DefaultMailLocale(ctx); err == nil && locale.Valid() {
			return locale
		}
	}
	return domain.LocaleEnglish
}

func (s *PersonalSettings) allowEmailChange(ctx context.Context, userID, canonicalEmail string) error {
	if s.emailLimiter == nil {
		return nil
	}
	allowed, err := s.emailLimiter.AllowEmailChange(ctx, userID, canonicalEmail)
	if err != nil {
		return dependencyError(err)
	}
	if !allowed {
		return ErrRateLimited
	}
	return nil
}

// NormalizeAvatar validates the actual bytes, rejects unsupported/animated
// inputs, and emits a fixed-size metadata-free PNG. The supplied media type is
// only an additional assertion; the decoder remains authoritative.
func NormalizeAvatar(data []byte, suppliedMediaType string) ([]byte, string, error) {
	if len(data) == 0 {
		return nil, "", avatarValidation("invalid_avatar")
	}
	if len(data) > MaxAvatarBytes {
		return nil, "", avatarValidation("content-too-large")
	}
	mediaType := normalizeImageMediaType(suppliedMediaType)
	if mediaType == "image/svg+xml" {
		return nil, "", avatarValidation("unsupported_type")
	}
	// multipart writers commonly label an uploaded part as octet-stream when
	// they do not know its media type. In that case the decoded format below is
	// authoritative; an explicit image media type is still checked for a
	// mismatched-content rejection.
	if mediaType == "application/octet-stream" {
		mediaType = ""
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		if looksLikeSVG(data) {
			return nil, "", avatarValidation("unsupported_type")
		}
		return nil, "", avatarValidation("invalid_avatar")
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > MaxAvatarDimension || config.Height > MaxAvatarDimension || config.Width > math.MaxInt/config.Height || config.Width*config.Height > MaxAvatarPixels {
		return nil, "", avatarValidation("invalid_avatar")
	}
	actualType := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "webp": "image/webp"}[format]
	if actualType == "" {
		return nil, "", avatarValidation("unsupported_type")
	}
	if mediaType != "" && mediaType != actualType {
		return nil, "", avatarValidation("invalid_avatar")
	}
	if isAnimatedAvatar(data, format) {
		return nil, "", avatarValidation("unsupported_type")
	}
	decoded, decodedFormat, err := image.Decode(bytes.NewReader(data))
	if err != nil || decodedFormat != format {
		return nil, "", avatarValidation("invalid_avatar")
	}
	bounds := decoded.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return nil, "", avatarValidation("invalid_avatar")
	}
	// Crop to a square before resampling. Scaling the full source to cover a
	// square would make an extreme 8192x1 image produce a multi-million-pixel
	// intermediate target even though the decoded pixel budget passed.
	cropSide := bounds.Dx()
	if bounds.Dy() < cropSide {
		cropSide = bounds.Dy()
	}
	crop := image.Rect(
		bounds.Min.X+(bounds.Dx()-cropSide)/2,
		bounds.Min.Y+(bounds.Dy()-cropSide)/2,
		bounds.Min.X+(bounds.Dx()-cropSide)/2+cropSide,
		bounds.Min.Y+(bounds.Dy()-cropSide)/2+cropSide,
	)
	canvas := image.NewNRGBA(image.Rect(0, 0, AvatarSide, AvatarSide))
	imageDraw.CatmullRom.Scale(canvas, canvas.Bounds(), decoded, crop, imageDraw.Over, nil)
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		return nil, "", avatarValidation("invalid_avatar")
	}
	return output.Bytes(), "image/png", nil
}

func avatarValidation(code string) error {
	return &domain.ValidationErrors{Items: []domain.FieldError{{Field: "avatar", Code: code}}}
}

func normalizeImageMediaType(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	if value == "image/jpg" {
		return "image/jpeg"
	}
	return value
}

func looksLikeSVG(data []byte) bool {
	prefix := data
	if len(prefix) > 4096 {
		prefix = prefix[:4096]
	}
	prefix = bytes.ToLower(prefix)
	return bytes.Contains(prefix, []byte("<svg"))
}

func isAnimatedAvatar(data []byte, format string) bool {
	switch format {
	case "png":
		// APNG stores an animation control chunk before the first frame.
		return containsBytes(data, []byte("acTL"))
	case "webp":
		// Animated WebP uses ANIM/ANMF chunks inside the RIFF container.
		return containsBytes(data, []byte("ANIM")) || containsBytes(data, []byte("ANMF"))
	default:
		return false
	}
}

func containsBytes(haystack, needle []byte) bool {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if string(haystack[index:index+len(needle)]) == string(needle) {
			return true
		}
	}
	return false
}
