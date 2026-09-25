package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	PasswordResetTokenVersion  = "v1"
	PasswordResetSelectorBytes = 16
	PasswordResetVerifierBytes = 32
	passwordResetContext       = "temvia-password-reset-v1"

	EmailChangeSelectorBytes = 16
	emailChangeContext       = "temvia-email-change-code-v1"
)

// PasswordResetMaterial contains the non-printable pieces needed by the
// request transaction. Verifier is intentionally a byte slice rather than a
// string so it cannot accidentally be included in logs or formatted structs.
type PasswordResetMaterial struct {
	Selector       []byte
	VerifierDigest []byte
}

// NewPasswordResetMaterial derives the verifier from the process secret and a
// freshly generated selector. The selector is public lookup data; the
// verifier is the actual recovery authority.
func NewPasswordResetMaterial(key, selector []byte) (PasswordResetMaterial, error) {
	verifier, err := derivePasswordResetVerifier(key, selector)
	if err != nil {
		return PasswordResetMaterial{}, err
	}
	digest := sha256.Sum256(verifier)
	return PasswordResetMaterial{
		Selector:       append([]byte(nil), selector...),
		VerifierDigest: append([]byte(nil), digest[:]...),
	}, nil
}

// NewPasswordResetToken derives and encodes the one-time credential for an
// email template. The raw verifier exists only in this function's stack frame;
// the returned material and all persisted projections contain only its digest.
func NewPasswordResetToken(key, selector []byte) (string, error) {
	verifier, err := derivePasswordResetVerifier(key, selector)
	if err != nil {
		return "", err
	}
	return PasswordResetTokenVersion + "." + base64.RawURLEncoding.EncodeToString(selector) + "." + base64.RawURLEncoding.EncodeToString(verifier), nil
}

func derivePasswordResetVerifier(key, selector []byte) ([]byte, error) {
	if len(key) != PasswordResetVerifierBytes || len(selector) != PasswordResetSelectorBytes {
		return nil, fmt.Errorf("invalid password reset key or selector length")
	}
	message := make([]byte, 0, len(passwordResetContext)+len(selector))
	message = append(message, passwordResetContext...)
	message = append(message, selector...)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(message)
	return mac.Sum(nil), nil
}

// ParsePasswordResetToken performs only syntactic parsing and returns the
// selector plus a digest of the presented verifier. It never returns or stores
// the raw verifier, and the database compares the digest in constant time.
func ParsePasswordResetToken(value string) (selector, verifierDigest []byte, ok bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || parts[0] != PasswordResetTokenVersion {
		return nil, nil, false
	}
	selector, ok = decodeCanonicalBase64URL(parts[1], PasswordResetSelectorBytes)
	if !ok {
		return nil, nil, false
	}
	verifier, ok := decodeCanonicalBase64URL(parts[2], PasswordResetVerifierBytes)
	if !ok {
		return nil, nil, false
	}
	digest := sha256.Sum256(verifier)
	return selector, append([]byte(nil), digest[:]...), true
}

// NewEmailChangeMaterial derives a six-digit verification code and the
// keyed verifier persisted by the email-change workflow. The code is never
// persisted; a mail worker can deterministically reconstruct it from the
// selector and the same process secret. The returned digest is deliberately
// keyed, so a database reader cannot cheaply enumerate six-digit values.
func NewEmailChangeMaterial(key, selector []byte) (code string, verifierDigest []byte, err error) {
	verifier, err := deriveEmailChangeVerifier(key, selector)
	if err != nil {
		return "", nil, err
	}
	codeValue := binary.BigEndian.Uint32(verifier[:4]) % 1_000_000
	code = fmt.Sprintf("%06d", codeValue)
	digest, err := EmailChangePresentedDigest(key, selector, code)
	if err != nil {
		return "", nil, err
	}
	return code, digest, nil
}

// EmailChangeCode reconstructs the code for the asynchronous mail worker. It
// is intentionally separate from the stored verifier projection.
func EmailChangeCode(key, selector []byte) (string, error) {
	verifier, err := deriveEmailChangeVerifier(key, selector)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", binary.BigEndian.Uint32(verifier[:4])%1_000_000), nil
}

// EmailChangePresentedDigest derives the digest used for an entered code. It
// is keyed and binds the presentation to the selector, while the expected
// digest persisted for a request is derived from the server-generated code.
func EmailChangePresentedDigest(key, selector []byte, code string) ([]byte, error) {
	if len(key) != PasswordResetVerifierBytes || len(selector) != EmailChangeSelectorBytes {
		return nil, fmt.Errorf("invalid email change key or selector length")
	}
	message := make([]byte, 0, len(emailChangeContext)+len(selector)+len(code)+1)
	message = append(message, emailChangeContext...)
	message = append(message, selector...)
	message = append(message, 0)
	message = append(message, code...)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(message)
	value := mac.Sum(nil)
	digest := sha256.Sum256(value)
	return append([]byte(nil), digest[:]...), nil
}

func deriveEmailChangeVerifier(key, selector []byte) ([]byte, error) {
	if len(key) != PasswordResetVerifierBytes || len(selector) != EmailChangeSelectorBytes {
		return nil, fmt.Errorf("invalid email change key or selector length")
	}
	message := make([]byte, 0, len(emailChangeContext)+len(selector))
	message = append(message, emailChangeContext...)
	message = append(message, selector...)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(message)
	return mac.Sum(nil), nil
}

func decodeCanonicalBase64URL(value string, decodedBytes int) ([]byte, bool) {
	if len(value) != (decodedBytes*8+5)/6 {
		return nil, false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return nil, false
		}
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != decodedBytes || base64.RawURLEncoding.EncodeToString(decoded) != value {
		return nil, false
	}
	return decoded, true
}

// FieldError is a stable, transport-independent validation error.
type FieldError struct {
	Field  string
	Code   string
	Params map[string]any
}

// ValidationErrors groups field errors without coupling the domain to HTTP.
type ValidationErrors struct {
	Items []FieldError
}

func (e *ValidationErrors) Error() string {
	if len(e.Items) == 0 {
		return "validation failed"
	}
	return fmt.Sprintf("validation failed for %d field(s)", len(e.Items))
}

func (e *ValidationErrors) add(field, code string) {
	e.Items = append(e.Items, FieldError{Field: field, Code: code})
}

type Name string

func NewName(value string) (Name, error) {
	if !utf8.ValidString(value) {
		return "", &ValidationErrors{Items: []FieldError{{Field: "name", Code: "invalid_name"}}}
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return "", &ValidationErrors{Items: []FieldError{{Field: "name", Code: "invalid_name"}}}
		}
	}
	value = norm.NFC.String(strings.TrimFunc(value, unicode.IsSpace))
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 100 {
		return "", &ValidationErrors{Items: []FieldError{{Field: "name", Code: "invalid_name"}}}
	}
	return Name(value), nil
}

type Email struct {
	Display   string
	Canonical string
}

func NewEmail(value string) (Email, error) {
	value = strings.TrimFunc(value, unicode.IsSpace)
	validation := &ValidationErrors{}
	if !utf8.ValidString(value) || len(value) == 0 || len(value) > 254 {
		validation.add("email", "invalid_email")
		return Email{}, validation
	}
	for _, r := range value {
		if r > 127 {
			validation.add("email", "invalid_email")
			return Email{}, validation
		}
	}
	at := strings.IndexByte(value, '@')
	if at <= 0 || at != strings.LastIndexByte(value, '@') || at > 64 || at == len(value)-1 {
		validation.add("email", "invalid_email")
		return Email{}, validation
	}
	local, host := value[:at], value[at+1:]
	if local[0] == '.' || local[len(local)-1] == '.' || strings.Contains(local, "..") {
		validation.add("email", "invalid_email")
		return Email{}, validation
	}
	for _, r := range local {
		if r == '.' || isEmailAtom(r) {
			continue
		}
		validation.add("email", "invalid_email")
		return Email{}, validation
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			validation.add("email", "invalid_email")
			return Email{}, validation
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			validation.add("email", "invalid_email")
			return Email{}, validation
		}
	}
	return Email{Display: value, Canonical: strings.ToLower(value)}, nil
}

func isEmailAtom(r rune) bool {
	if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
		return true
	}
	return strings.ContainsRune("!#$%&'*+-/=?^_`{|}~", r)
}

type Password string

func NewPassword(value string) (Password, error) {
	return newPassword(value, "invalid_password", true)
}

// NewLoginPassword validates only the input boundary needed before password
// verification. It deliberately does not apply the creation policy so that a
// password accepted by an earlier policy remains usable after an upgrade.
func NewLoginPassword(value string) (Password, error) {
	return newPassword(value, "invalid_login_password", false)
}

func newPassword(value, code string, requireComposition bool) (Password, error) {
	if !utf8.ValidString(value) {
		return "", passwordValidationError(code)
	}
	value = norm.NFC.String(value)
	runes := utf8.RuneCountInString(value)
	if runes == 0 || runes > 128 || (requireComposition && runes < 8) {
		return "", passwordValidationError(code)
	}
	if requireComposition {
		var upper, lower, digit, special bool
		for _, r := range value {
			switch {
			case r >= 'A' && r <= 'Z':
				upper = true
			case r >= 'a' && r <= 'z':
				lower = true
			case r >= '0' && r <= '9':
				digit = true
			case isPasswordSpecial(r):
				special = true
			}
		}
		if !upper || !lower || !digit || !special {
			return "", passwordValidationError(code)
		}
	}
	return Password(value), nil
}

func passwordValidationError(code string) error {
	return &ValidationErrors{Items: []FieldError{{Field: "password", Code: code}}}
}

func isPasswordSpecial(r rune) bool {
	return (r >= 0x21 && r <= 0x2f) ||
		(r >= 0x3a && r <= 0x40) ||
		(r >= 0x5b && r <= 0x60) ||
		(r >= 0x7b && r <= 0x7e)
}
