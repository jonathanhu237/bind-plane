package domain

import (
	"bytes"
	"testing"
)

func TestInvitationTokenRoundTripUsesDistinctAuthorityContext(t *testing.T) {
	key := bytes.Repeat([]byte{0x2a}, 32)
	selector := bytes.Repeat([]byte{0x11}, 16)
	material, err := NewInvitationMaterial(key, selector)
	if err != nil {
		t.Fatal(err)
	}
	token, err := NewInvitationToken(key, selector)
	if err != nil {
		t.Fatal(err)
	}
	gotSelector, gotDigest, ok := ParseInvitationToken(token)
	if !ok || !bytes.Equal(gotSelector, selector) || !bytes.Equal(gotDigest, material.VerifierDigest) {
		t.Fatalf("ParseInvitationToken() = %x, %x, %t", gotSelector, gotDigest, ok)
	}
	resetMaterial, err := NewPasswordResetMaterial(key, selector)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(gotDigest, resetMaterial.VerifierDigest) {
		t.Fatal("invitation and password-reset HMAC contexts are not distinct")
	}
}

func TestInvitationTokenRejectsMalformedValues(t *testing.T) {
	key := bytes.Repeat([]byte{0x2a}, 32)
	token, err := NewInvitationToken(key, bytes.Repeat([]byte{0x11}, 16))
	if err != nil {
		t.Fatal(err)
	}
	parts := splitToken(token)
	for _, candidate := range []string{
		"",
		"v2." + parts[1] + "." + parts[2],
		token + ".extra",
		"v1." + parts[1][:21] + "." + parts[2],
		"v1." + parts[1] + "." + parts[2][:42] + "=",
	} {
		if _, _, ok := ParseInvitationToken(candidate); ok {
			t.Fatalf("malformed invitation token accepted: %q", candidate)
		}
	}
}

func splitToken(value string) [3]string {
	var result [3]string
	parts := []byte(value)
	start := 0
	index := 0
	for i, character := range parts {
		if character == '.' && index < 2 {
			result[index] = string(parts[start:i])
			start = i + 1
			index++
		}
	}
	result[index] = string(parts[start:])
	return result
}
