package release

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestAssetEditRejectsInvalidInputBeforeStorage(t *testing.T) {
	t.Setenv("BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	const id = "00000000-0000-4000-8000-000000000001"
	admin := domain.Principal{SuperAdmin: true}
	for _, tc := range []struct {
		name, path, body string
		principal        domain.Principal
		want             int
	}{
		{"operator", "switches/" + id, `{"version":1,"is_enabled":false}`, domain.Principal{Roles: []domain.Role{{Name: "Operator"}}}, 403},
		{"malformed id", "switches/not-a-uuid", `{"version":1}`, admin, 404},
		{"missing version", "switches/" + id, `{"is_enabled":false}`, admin, 422},
		{"blank name", "switches/" + id, `{"version":1,"name":"  "}`, admin, 422},
		{"non-canonical address", "switches/" + id, `{"version":1,"management_ip":"10.0.0.01"}`, admin, 422},
		{"non-canonical cidr", "networks/" + id, `{"version":1,"cidr":"10.0.0.1/24"}`, admin, 422},
		{"profile rule type", "profiles/" + id, `{"version":1,"command_templates":[]}`, admin, 422},
		{"secret replaced and cleared", "credentials/" + id, `{"version":1,"secret":"x","clear_secret":true}`, admin, 422},
		{"unknown field", "credentials/" + id, `{"version":1,"encrypted_password":"v1:x"}`, admin, 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A nil database proves these requests are refused before any storage access.
			h, err := NewAdminHandler(nil, fakeIdentity{principal: tc.principal}, "session", "http://localhost:5173")
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("PATCH", "/api/releases/admin/"+tc.path, strings.NewReader(tc.body))
			r.Header.Set("Origin", "http://localhost:5173")
			r.Header.Set("Content-Type", "application/json")
			r.AddCookie(&http.Cookie{Name: "session", Value: "token"})
			response := httptest.NewRecorder()
			h.ServeHTTP(response, r)
			if response.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", response.Code, tc.want, response.Body.String())
			}
		})
	}
}

func TestAuditValueIgnoresJSONFormatting(t *testing.T) {
	if !reflect.DeepEqual(auditValue(`{"a": [1, 2]}`, true), auditValue(`{"a":[1,2]}`, true)) {
		t.Fatal("reformatted JSON reported as a change")
	}
	if reflect.DeepEqual(auditValue(`{"a":1}`, false), auditValue(`{"a": 1}`, false)) {
		t.Fatal("plain text compared as JSON")
	}
}
