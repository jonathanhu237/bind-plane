package application

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

const personalTestUserID = "019535d9-3df7-79fb-b466-fa907fa17f9e"

type personalSettingsFake struct {
	account       domain.Account
	avatar        domain.Avatar
	request       domain.EmailChangeRequest
	passwordHash  string
	requestCalls  int
	completeCalls int
	presented     []byte
	expectedAuth  int64
	locale        domain.Locale
	email         domain.Email
}

func (f *personalSettingsFake) FindByCanonicalEmail(_ context.Context, canonical string) (domain.Account, error) {
	if canonical != f.account.User.Email {
		return domain.Account{}, ErrAccountNotFound
	}
	return f.account, nil
}

func (f *personalSettingsFake) FindPublicByID(_ context.Context, id string) (domain.User, error) {
	if id != f.account.User.ID {
		return domain.User{}, ErrAccountNotFound
	}
	return f.account.User, nil
}

func (f *personalSettingsFake) FindPublicAccountByID(_ context.Context, id string) (domain.Account, error) {
	if id != f.account.User.ID {
		return domain.Account{}, ErrAccountNotFound
	}
	return f.account, nil
}

func (f *personalSettingsFake) UpdateUserName(_ context.Context, id string, name domain.Name) (domain.User, error) {
	if id != f.account.User.ID {
		return domain.User{}, ErrAccountNotFound
	}
	f.account.User.Name = string(name)
	return f.account.User, nil
}

func (f *personalSettingsFake) UpdateUserLocale(_ context.Context, id string, locale domain.Locale) (domain.User, error) {
	if id != f.account.User.ID {
		return domain.User{}, ErrAccountNotFound
	}
	f.account.User.Locale = locale
	f.locale = locale
	return f.account.User, nil
}

func (f *personalSettingsFake) SaveUserAvatar(_ context.Context, id string, data []byte, mediaType string) (domain.Avatar, error) {
	if id != f.account.User.ID {
		return domain.Avatar{}, ErrAccountNotFound
	}
	f.avatar = domain.Avatar{Bytes: append([]byte(nil), data...), MediaType: mediaType, Version: f.avatar.Version + 1}
	if f.avatar.Version == 1 {
		f.avatar.Version = 1
	}
	return f.avatar, nil
}

func (f *personalSettingsFake) DeleteUserAvatar(_ context.Context, id string) error {
	if id != f.account.User.ID {
		return ErrAccountNotFound
	}
	f.avatar = domain.Avatar{}
	return nil
}

func (f *personalSettingsFake) GetUserAvatar(_ context.Context, id string) (domain.Avatar, error) {
	if id != f.account.User.ID {
		return domain.Avatar{}, ErrAccountNotFound
	}
	if len(f.avatar.Bytes) == 0 {
		return domain.Avatar{}, ErrAvatarNotFound
	}
	return f.avatar, nil
}

func (f *personalSettingsFake) ChangePassword(_ context.Context, id string, expectedAuthVersion int64, hash string, locale domain.Locale, _ time.Duration) (domain.User, time.Time, error) {
	if id != f.account.User.ID {
		return domain.User{}, time.Time{}, ErrAccountNotFound
	}
	if expectedAuthVersion != f.account.AuthVersion {
		return domain.User{}, time.Time{}, ErrStaleRevision
	}
	f.expectedAuth = expectedAuthVersion
	f.passwordHash = hash
	f.account.AuthVersion++
	f.locale = locale
	return f.account.User, time.Unix(100, 0), nil
}

func (f *personalSettingsFake) GetEmailChangeRequest(_ context.Context, id string) (domain.EmailChangeRequest, error) {
	if id != f.account.User.ID {
		return domain.EmailChangeRequest{}, ErrAccountNotFound
	}
	if f.request.ID == "" {
		return domain.EmailChangeRequest{}, ErrEmailChangeNotFound
	}
	return f.request, nil
}

func (f *personalSettingsFake) RequestEmailChange(_ context.Context, id string, email domain.Email, selector, digest []byte, ttl time.Duration, _ domain.Locale) (domain.EmailChangeRequest, error) {
	if id != f.account.User.ID {
		return domain.EmailChangeRequest{}, ErrAccountNotFound
	}
	f.requestCalls++
	f.request = domain.EmailChangeRequest{ID: "019535d9-3df7-79fb-b466-fa907fa17f9f", UserID: id, OldEmail: f.account.User.Email, NewEmail: email.Display, ExpiresAt: time.Now().Add(ttl), ResendAfter: time.Now().Add(time.Minute), AttemptsRemaining: 5, Revision: 1, Selector: append([]byte(nil), selector...), VerifierDigest: append([]byte(nil), digest...)}
	return f.request, nil
}

func (f *personalSettingsFake) ResendEmailChange(_ context.Context, id string, selector, digest []byte, _ time.Duration, _ domain.Locale) (domain.EmailChangeRequest, error) {
	if id != f.account.User.ID {
		return domain.EmailChangeRequest{}, ErrAccountNotFound
	}
	f.request.Selector = append([]byte(nil), selector...)
	f.request.VerifierDigest = append([]byte(nil), digest...)
	f.request.AttemptsRemaining = EmailChangeMaxAttempts
	f.request.Revision++
	return f.request, nil
}

func (f *personalSettingsFake) CompleteEmailChange(_ context.Context, id, requestID string, presentedDigest []byte, _ domain.Locale, _ time.Duration) (domain.User, time.Time, error) {
	if id != f.account.User.ID || requestID != f.request.ID {
		return domain.User{}, time.Time{}, ErrInvalidEmailChange
	}
	f.completeCalls++
	f.presented = append([]byte(nil), presentedDigest...)
	if !bytes.Equal(presentedDigest, f.request.VerifierDigest) {
		f.request.AttemptsRemaining--
		return domain.User{}, time.Time{}, ErrInvalidEmailChangeCode
	}
	f.account.User.Email = f.request.NewEmail
	f.request = domain.EmailChangeRequest{}
	return f.account.User, time.Unix(101, 0), nil
}

type personalSettingsHasher struct{}

func (personalSettingsHasher) Hash(context.Context, string) (string, error) { return "new-hash", nil }
func (personalSettingsHasher) Verify(context.Context, string, string) (bool, error) {
	return true, nil
}

type personalSettingsRandom struct{ value byte }

func (r personalSettingsRandom) Read(dst []byte) error {
	for i := range dst {
		dst[i] = r.value + byte(i)
	}
	return nil
}

func TestNormalizeAvatarProducesFixedPNGAndRejectsUnsafeInputs(t *testing.T) {
	input := image.NewRGBA(image.Rect(0, 0, 320, 180))
	input.Set(1, 1, color.RGBA{R: 255, A: 255})
	var source bytes.Buffer
	if err := png.Encode(&source, input); err != nil {
		t.Fatal(err)
	}
	encoded, mediaType, err := NormalizeAvatar(source.Bytes(), "image/jpeg")
	if err == nil || mediaType != "" || encoded != nil {
		t.Fatalf("mismatched media type = %q, %d bytes, %v", mediaType, len(encoded), err)
	}
	encoded, mediaType, err = NormalizeAvatar(source.Bytes(), "image/png")
	if err != nil || mediaType != "image/png" {
		t.Fatalf("NormalizeAvatar() = %q, %v", mediaType, err)
	}
	if _, mediaType, err = NormalizeAvatar(source.Bytes(), "application/octet-stream"); err != nil || mediaType != "image/png" {
		t.Fatalf("octet-stream avatar = %q, %v", mediaType, err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(encoded))
	if err != nil || format != "png" || config.Width != AvatarSide || config.Height != AvatarSide {
		t.Fatalf("normalized image = %#v, format=%q, err=%v", config, format, err)
	}
	if bytes.Equal(encoded, source.Bytes()) {
		t.Fatal("normalized avatar unexpectedly retained the source encoding")
	}
	if _, _, err := NormalizeAvatar([]byte("<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>"), "image/svg+xml"); validationCode(err) != "unsupported_type" {
		t.Fatalf("SVG error code = %q", validationCode(err))
	}
	if _, _, err := NormalizeAvatar(bytes.Repeat([]byte{'x'}, MaxAvatarBytes+1), "image/png"); validationCode(err) != "content-too-large" {
		t.Fatalf("oversized error code = %q", validationCode(err))
	}

	wide := image.NewRGBA(image.Rect(0, 0, MaxAvatarDimension, 1))
	var wideSource bytes.Buffer
	if err := png.Encode(&wideSource, wide); err != nil {
		t.Fatal(err)
	}
	if encoded, _, err := NormalizeAvatar(wideSource.Bytes(), "image/png"); err != nil || len(encoded) == 0 {
		t.Fatalf("extreme aspect-ratio avatar = %d bytes, %v", len(encoded), err)
	}
}

func TestPersonalSettingsEmailChangeUsesIndependentKeyedAuthority(t *testing.T) {
	store := &personalSettingsFake{account: domain.Account{User: domain.User{ID: personalTestUserID, Name: "Ada", Email: "ada@example.com"}, PasswordHash: "old", AuthVersion: 4}}
	key := bytes.Repeat([]byte{0x21}, 32)
	service := NewPersonalSettings(store, store, store, store, personalSettingsHasher{}, personalSettingsRandom{value: 9}, key, time.Hour)
	request, err := service.RequestEmailChange(context.Background(), personalTestUserID, "old", "new@example.com")
	if err != nil || request.ID == "" || request.UserID != "" || len(request.Selector) != 0 || len(request.VerifierDigest) != 0 {
		t.Fatalf("sanitized email request = %#v, err=%v", request, err)
	}
	if store.requestCalls != 1 || store.request.NewEmail != "new@example.com" {
		t.Fatalf("request state = %#v, calls=%d", store.request, store.requestCalls)
	}
	store.request.AttemptsRemaining = 0
	if _, err := service.ResendEmailChange(context.Background(), personalTestUserID); !errors.Is(err, ErrEmailChangeAttemptsExceeded) {
		t.Fatalf("exhausted resend error = %v", err)
	}
	store.request.AttemptsRemaining = 1
	request, err = service.ResendEmailChange(context.Background(), personalTestUserID)
	if err != nil || request.AttemptsRemaining != EmailChangeMaxAttempts || request.Revision != 2 {
		t.Fatalf("resend state = %#v, err=%v", request, err)
	}
	code, err := domain.EmailChangeCode(key, store.request.Selector)
	if err != nil {
		t.Fatal(err)
	}
	expectedDigest := append([]byte(nil), store.request.VerifierDigest...)
	updated, _, err := service.CompleteEmailChange(context.Background(), personalTestUserID, request.ID, code)
	if err != nil || updated.Email != "new@example.com" || store.completeCalls != 1 {
		t.Fatalf("CompleteEmailChange() = %#v, %v; calls=%d", updated, err, store.completeCalls)
	}
	if !bytes.Equal(store.presented, expectedDigest) {
		t.Fatalf("presented digest = %x, want persisted keyed digest %x", store.presented, expectedDigest)
	}
}

func TestPersonalSettingsPasswordChangeVerifiesAndPassesAuthVersion(t *testing.T) {
	store := &personalSettingsFake{account: domain.Account{User: domain.User{ID: personalTestUserID, Name: "Ada", Email: "ada@example.com"}, PasswordHash: "old", AuthVersion: 7}}
	service := NewPersonalSettings(store, store, store, store, personalSettingsHasher{}, personalSettingsRandom{value: 1}, bytes.Repeat([]byte{0x22}, 32), time.Hour)
	updated, _, err := service.ChangePassword(context.Background(), personalTestUserID, "old", "Aa1!newpassword")
	if err != nil || updated.ID != personalTestUserID || store.passwordHash != "new-hash" || store.expectedAuth != 7 {
		t.Fatalf("ChangePassword() = %#v, %v; store=%#v", updated, err, store)
	}
}

func TestPersonalSettingsRejectsInvalidAccountIDsBeforeStorage(t *testing.T) {
	store := &personalSettingsFake{}
	service := NewPersonalSettings(store, store, store, store, personalSettingsHasher{}, personalSettingsRandom{value: 1}, bytes.Repeat([]byte{0x22}, 32), time.Hour)
	if _, err := service.UpdateLocale(context.Background(), "not-a-uuid", domain.LocaleEnglish); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("UpdateLocale() error = %v", err)
	}
	if _, err := service.Avatar(context.Background(), "not-a-uuid"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("Avatar() error = %v", err)
	}
}

func validationCode(err error) string {
	validation, ok := err.(*domain.ValidationErrors)
	if !ok || len(validation.Items) != 1 {
		return ""
	}
	return validation.Items[0].Code
}
