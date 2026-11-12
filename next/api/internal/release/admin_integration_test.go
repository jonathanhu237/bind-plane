package release

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

func TestCredentialCreationIsAuditedAtomicallyWithoutPlaintext(t *testing.T) {
	dsn := os.Getenv("BIND_PLANE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated test database required")
	}
	key := os.Getenv("BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY")
	if _, err := base64.RawURLEncoding.DecodeString(key); err != nil || len(key) == 0 {
		t.Fatal("isolated encryption key required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var actorID string
	err = db.QueryRow(`INSERT INTO auth_users(name,email,email_canonical,password_hash) VALUES ('Admin','audit@test.invalid','audit@test.invalid','test') RETURNING id::text`).Scan(&actorID)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewAdminHandler(db, fakeIdentity{principal: domain.Principal{User: domain.User{ID: actorID, Email: "audit@test.invalid"}, SuperAdmin: true}}, "session", "http://localhost:5173")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/releases/admin/credentials", strings.NewReader(`{"Name":"audit-test","Username":"operator","Password":"unique-switch-secret"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://localhost:5173")
	r.AddCookie(&http.Cookie{Name: "session", Value: "token"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var ciphertext string
	var count int
	err = db.QueryRow(`SELECT encrypted_password FROM bp_credentials WHERE id=$1`, body.ID).Scan(&ciphertext)
	if err != nil || !strings.HasPrefix(ciphertext, "v1:") || strings.Contains(ciphertext, "unique-switch-secret") {
		t.Fatalf("credential not encrypted: %v", err)
	}
	err = db.QueryRow(`SELECT count(*) FROM bp_asset_events WHERE object_id=$1 AND actor_id=$2 AND action='credential.create' AND object_name='audit-test'`, body.ID, actorID).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("asset audit missing: %v count %d", err, count)
	}

	// Switches that ask only for a password over Telnet have a credential without a username,
	// and an existing credential can lose its username; a password stays required.
	send := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost:5173")
		r.AddCookie(&http.Cookie{Name: "session", Value: "token"})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := send("POST", "/api/releases/admin/credentials", `{"Name":"password-only","Password":"another-secret"}`); w.Code != 201 {
		t.Fatalf("password-only credential = %d %s", w.Code, w.Body.String())
	}
	if w := send("POST", "/api/releases/admin/credentials", `{"Name":"no-password","Username":"operator"}`); w.Code != 422 {
		t.Fatalf("credential without a password = %d %s", w.Code, w.Body.String())
	}
	var username string
	if err = db.QueryRow(`SELECT username FROM bp_credentials WHERE name='password-only'`).Scan(&username); err != nil || username != "" {
		t.Fatalf("password-only username = %q, %v", username, err)
	}
	if w := send("PATCH", "/api/releases/admin/credentials/"+body.ID, `{"version":1,"username":"  "}`); w.Code != 200 {
		t.Fatalf("clearing the username = %d %s", w.Code, w.Body.String())
	}
	if err = db.QueryRow(`SELECT username FROM bp_credentials WHERE id=$1`, body.ID).Scan(&username); err != nil || username != "" {
		t.Fatalf("cleared username = %q, %v", username, err)
	}
}
