package domain

import (
	"bytes"
	"strings"
	"testing"
)

func TestPasswordResetTokenRoundTripOnlyExposesDigest(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, PasswordResetVerifierBytes)
	selector := bytes.Repeat([]byte{0x13}, PasswordResetSelectorBytes)
	material, err := NewPasswordResetMaterial(key, selector)
	if err != nil {
		t.Fatal(err)
	}
	token, err := NewPasswordResetToken(key, selector)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "v1.") || len(strings.Split(token, ".")) != 3 {
		t.Fatalf("token = %q", token)
	}
	gotSelector, gotDigest, ok := ParsePasswordResetToken(token)
	if !ok || !bytes.Equal(gotSelector, material.Selector) || !bytes.Equal(gotDigest, material.VerifierDigest) {
		t.Fatalf("ParsePasswordResetToken() = %x, %x, %t", gotSelector, gotDigest, ok)
	}
	if _, _, ok := ParsePasswordResetToken(token + "="); ok {
		t.Fatal("padded verifier accepted")
	}
}

func TestPasswordResetTokenRejectsMalformedAndNonCanonicalValues(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, PasswordResetVerifierBytes)
	selector := bytes.Repeat([]byte{0x13}, PasswordResetSelectorBytes)
	token, err := NewPasswordResetToken(key, selector)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	for _, token := range []string{
		"",
		"v2." + parts[1] + "." + parts[2],
		"v1." + parts[1] + "." + parts[2] + ".extra",
		"v1." + parts[1][:21] + "." + parts[2],
		"v1." + parts[1] + "." + parts[2][:42] + "=",
	} {
		if _, _, ok := ParsePasswordResetToken(token); ok {
			t.Errorf("ParsePasswordResetToken(%q) accepted malformed token", token)
		}
	}
}

func TestEmailChangeCodeMaterialIsKeyedAndReconstructible(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, PasswordResetVerifierBytes)
	selector := bytes.Repeat([]byte{0x13}, EmailChangeSelectorBytes)
	code, digest, err := NewEmailChangeMaterial(key, selector)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 || code < "000000" || code > "999999" {
		t.Fatalf("code = %q, want six decimal digits", code)
	}
	reconstructed, err := EmailChangeCode(key, selector)
	if err != nil || reconstructed != code {
		t.Fatalf("EmailChangeCode() = %q, %v; want %q", reconstructed, err, code)
	}
	presented, err := EmailChangePresentedDigest(key, selector, code)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(presented, digest) {
		t.Fatal("presented code digest does not match the persisted keyed verifier")
	}
	for _, invalid := range []string{"", "short", "12345", "1234567", "１２３４５６"} {
		if _, err := EmailChangePresentedDigest(key, selector, invalid); err != nil {
			// The digest helper is deliberately format-agnostic; the application
			// boundary validates the six-digit presentation.
			t.Fatal(err)
		}
	}
	if _, _, err := NewEmailChangeMaterial(key[:31], selector); err == nil {
		t.Fatal("invalid key length accepted")
	}
	if _, err := EmailChangeCode(key, selector[:15]); err == nil {
		t.Fatal("invalid selector length accepted")
	}
}

func TestName(t *testing.T) {
	got, err := NewName(" \u0065\u0301  Ada  ")
	if err != nil || got != "é  Ada" {
		t.Fatalf("NewName() = %q, %v", got, err)
	}
	for _, value := range []string{"", "\nname", "\u2028name"} {
		if _, err := NewName(value); err == nil {
			t.Errorf("NewName(%q) accepted invalid value", value)
		}
	}
}

func TestEmail(t *testing.T) {
	got, err := NewEmail(" Ada.Example+tag@Example.COM ")
	if err != nil || got.Display != "Ada.Example+tag@Example.COM" || got.Canonical != "ada.example+tag@example.com" {
		t.Fatalf("NewEmail() = %#v, %v", got, err)
	}
	for _, value := range []string{"a..b@example.com", "a@example..com", "Ada <a@example.com>", "a@-example.com", "é@example.com"} {
		if _, err := NewEmail(value); err == nil {
			t.Errorf("NewEmail(%q) accepted invalid value", value)
		}
	}
}

func TestPassword(t *testing.T) {
	got, err := NewPassword("Aa1!e\u0301xxx")
	if err != nil || got != "Aa1!éxxx" {
		t.Fatalf("NewPassword() = %q, %v", got, err)
	}
	if got, err := NewPassword(" Aa1!xxx"); err != nil || got != " Aa1!xxx" {
		t.Fatalf("NewPassword() should preserve password whitespace, got %q, %v", got, err)
	}
	if got, err := NewPassword("Aa1!😀xxx"); err != nil || got != "Aa1!😀xxx" {
		t.Fatalf("NewPassword() should allow additional Unicode characters, got %q, %v", got, err)
	}
	if got, err := NewPassword("Aa1!" + strings.Repeat("x", 124)); err != nil || len([]rune(got)) != 128 {
		t.Fatalf("NewPassword() accepted 128-rune password as %q, %v", got, err)
	}

	for _, test := range []struct {
		name     string
		value    string
		wantCode string
	}{
		{name: "below minimum", value: "Aa1!xxx", wantCode: "invalid_password"},
		{name: "above maximum", value: "Aa1!" + strings.Repeat("x", 125), wantCode: "invalid_password"},
		{name: "missing uppercase", value: "aa1!xxxx", wantCode: "invalid_password"},
		{name: "missing lowercase", value: "AA1!XXXX", wantCode: "invalid_password"},
		{name: "missing digit", value: "Aax!xxxx", wantCode: "invalid_password"},
		{name: "missing special", value: "Aa1xxxxx", wantCode: "invalid_password"},
		{name: "non ASCII uppercase does not substitute", value: "aá1!xxxx", wantCode: "invalid_password"},
		{name: "non ASCII lowercase does not substitute", value: "AÁ1!XXXX", wantCode: "invalid_password"},
		{name: "non ASCII digit does not substitute", value: "Aa１!xxxx", wantCode: "invalid_password"},
		{name: "non ASCII special does not substitute", value: "Aa1！xxxx", wantCode: "invalid_password"},
		{name: "invalid UTF-8", value: string([]byte{'A', 'a', '1', '!', 0xff, 'x', 'x', 'x'}), wantCode: "invalid_password"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewPassword(test.value); validationCode(err) != test.wantCode {
				t.Fatalf("NewPassword(%q) error code = %q, want %q", test.value, validationCode(err), test.wantCode)
			}
		})
	}
}

func TestLoginPasswordPreservesLegacyValues(t *testing.T) {
	legacy := "correct horse battery"
	got, err := NewLoginPassword(legacy)
	if err != nil || got != Password(legacy) {
		t.Fatalf("NewLoginPassword(%q) = %q, %v", legacy, got, err)
	}
	got, err = NewLoginPassword("e\u0301")
	if err != nil || got != "é" {
		t.Fatalf("NewLoginPassword() did not normalize NFC: %q, %v", got, err)
	}
	if got, err := NewLoginPassword("password"); err != nil || got != "password" {
		t.Fatalf("NewLoginPassword() unexpectedly applied composition policy: %q, %v", got, err)
	}

	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "above maximum", value: strings.Repeat("x", 129)},
		{name: "invalid UTF-8", value: string([]byte{'x', 0xff})},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewLoginPassword(test.value); validationCode(err) != "invalid_login_password" {
				t.Fatalf("NewLoginPassword(%q) error code = %q, want invalid_login_password", test.value, validationCode(err))
			}
		})
	}
}

func validationCode(err error) string {
	validation, ok := err.(*ValidationErrors)
	if !ok || len(validation.Items) != 1 {
		return ""
	}
	return validation.Items[0].Code
}
