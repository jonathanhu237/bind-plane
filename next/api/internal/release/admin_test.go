package release

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

func TestAssetMutationRequiresAdminAndOrigin(t *testing.T) {
	t.Setenv("BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	for _, tc := range []struct {
		origin    string
		principal domain.Principal
		want      int
	}{
		{"http://bad.invalid", domain.Principal{SuperAdmin: true}, 403},
		{"http://localhost:5173", domain.Principal{Roles: []domain.Role{{Name: "Operator"}}}, 403},
	} {
		h, err := NewAdminHandler(nil, fakeIdentity{principal: tc.principal}, "session", "http://localhost:5173")
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/api/releases/admin/credentials", strings.NewReader(`{"Name":"test","Username":"test","Password":"secret"}`))
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: "session", Value: "token"})
		response := httptest.NewRecorder()
		h.ServeHTTP(response, r)
		if response.Code != tc.want {
			t.Fatalf("status %d, want %d", response.Code, tc.want)
		}
	}
}
func TestCredentialSealingUsesAuthenticatedEncryption(t *testing.T) {
	t.Setenv("BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	_, err := NewAdminHandler(nil, fakeIdentity{}, "session", "http://localhost:5173")
	if err != nil {
		t.Fatal(err)
	}
	// Constructor must not accept a missing or weak key.
	t.Setenv("BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY", "")
	if _, err = NewAdminHandler(nil, fakeIdentity{}, "session", "http://localhost:5173"); err == nil {
		t.Fatal("empty encryption key accepted")
	}
	a := &AdminHandler{key: make([]byte, 32)}
	encrypted, err := a.seal("switch-secret")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, "switch-secret") {
		t.Fatal("plaintext credential stored")
	}
	packed, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encrypted, "v1:"))
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(a.key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := gcm.Open(nil, packed[:gcm.NonceSize()], packed[gcm.NonceSize():], nil)
	if err != nil || string(plain) != "switch-secret" {
		t.Fatalf("cannot decrypt ciphertext: %v", err)
	}
}
