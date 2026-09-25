package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/adapter/password"
	postgresadapter "github.com/jonathanhu237/bind-plane/next/api/internal/auth/adapter/postgres"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

func TestUserLifecycleHTTPIntegration(t *testing.T) {
	db, ctx := openHTTPIntegrationDatabase(t)
	cfg := testConfig()
	cfg.LoginGlobalCapacity = 100
	cfg.LoginEmailCapacity = 100
	cfg.LoginGlobalRefillInterval = time.Millisecond
	cfg.LoginEmailRefillInterval = time.Millisecond
	store := postgresadapter.NewStore(db, cfg)
	mailTaskBox, err := application.NewMailTaskSecretBox(bytes.Repeat([]byte{0x6d}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store.SetMailTaskSecretBox(mailTaskBox)
	hasher, err := password.NewHasher(2)
	if err != nil {
		t.Fatal(err)
	}
	fixture := createOnlineHTTPFixture(t, ctx, db, hasher)
	// Global writer has the existing user-management permission combination,
	// but cannot grant Super Admin; lifecycle authority is intentionally broader.
	if _, err = db.ExecContext(ctx, `INSERT INTO auth_role_permissions(role_id,permission_key) VALUES($1,'users.read'),($1,'users.write'),($1,'roles.read') ON CONFLICT DO NOTHING`, fixture.roleID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO auth_user_roles(user_id,role_id) SELECT $1::uuid,id FROM auth_roles WHERE system_key='super_admin'`, fixture.otherID); err != nil {
		t.Fatal(err)
	}
	auth := application.NewAuthentication(store, hasher, store, store, application.CryptoRandom(), domain.DefaultPermissionCatalog())
	access := application.NewAccessManagement(store, store, domain.DefaultPermissionCatalog())
	logs := application.NewOperationLogService(store)
	h := NewHandlerWithAccessAndOperationLog(&setupFake{status: application.SetupComplete}, auth, cfg, nil, access, nil, nil, logs)
	manager := loginOnlineHTTPUser(t, h, fixture.managerEmail, fixture.managerPassword)
	target := loginOnlineHTTPUser(t, h, fixture.targetEmail, fixture.targetPassword)
	request := func(method, path, body string, cookie *http.Cookie, status int) []byte {
		t.Helper()
		r := onlineHTTPRequest(h, method, path, body, cookie)
		if r.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, r.Code, r.Body.String())
		}
		return r.Body.Bytes()
	}
	mutation := func(action string, version int64, status int) {
		t.Helper()
		method, path := http.MethodPost, "/api/users/"+fixture.targetID+"/"+action
		if action == "delete" {
			method = http.MethodDelete
			path = "/api/users/" + fixture.targetID
		}
		request(method, path, fmt.Sprintf(`{"authVersion":%d}`, version), manager, status)
	}
	request(http.MethodPost, "/api/users/"+fixture.managerID+"/deactivate", `{"authVersion":1}`, manager, 403)
	request(http.MethodPost, "/api/users/not-an-id/deactivate", `{"authVersion":1}`, manager, 404)
	request(http.MethodPost, "/api/users/"+fixture.targetID+"/deactivate", `{}`, manager, 409)
	mutation("deactivate", 1, 200)
	body := request(http.MethodGet, "/api/auth/session-status", "", target, 401)
	if !bytes.Contains(body, []byte(`"code":"account_disabled"`)) {
		t.Fatalf("disabled reason: %s", body)
	}
	payload, _ := json.Marshal(map[string]string{"email": fixture.targetEmail, "password": fixture.targetPassword})
	body = request(http.MethodPost, "/api/auth/login", string(payload), nil, 401)
	if bytes.Contains(body, []byte("account_disabled")) {
		t.Fatalf("anonymous status leak: %s", body)
	}
	body = request(http.MethodGet, "/api/users?status=disabled", "", manager, 200)
	if !bytes.Contains(body, []byte(fixture.targetID)) {
		t.Fatalf("disabled filter: %s", body)
	}
	mutation("deactivate", 1, 409)
	mutation("reactivate", 2, 200)
	request(http.MethodGet, "/api/auth/session-status", "", target, 401)
	fresh := loginOnlineHTTPUser(t, h, fixture.targetEmail, fixture.targetPassword)
	request(http.MethodGet, "/api/auth/me", "", fresh, 200)
	// Previously emitted invitations must stay consumable after creator deletion.
	selector, digest := bytes.Repeat([]byte{41}, 16), bytes.Repeat([]byte{42}, 32)
	inv, err := store.CreateInvitation(ctx, fixture.targetID, "Retained invite", "retained@example.com", domain.LocaleEnglish, []string{fixture.roleID}, selector, digest, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	mutation("delete", 3, 204)
	request(http.MethodGet, "/api/auth/session-status", "", fresh, 401)
	if _, err = store.FindInvitationTarget(ctx, selector, digest); err != nil {
		t.Fatalf("invitation target after deletion: %v", err)
	}
	kept, err := store.FindInvitation(ctx, inv.ID)
	if err != nil || !kept.CreatorDeleted || kept.CreatedBy != fixture.targetID || kept.CreatedByEmail != fixture.targetEmail {
		t.Fatalf("creator snapshot: %+v %v", kept, err)
	}
	if err = store.CompleteInvitation(ctx, selector, digest, "hash"); err != nil {
		t.Fatalf("accept after creator deletion: %v", err)
	}
	body = request(http.MethodGet, "/api/operation-logs?actorId="+fixture.targetID, "", manager, 200)
	if !bytes.Contains(body, []byte(`"deleted":true`)) || !bytes.Contains(body, []byte(fixture.targetEmail)) {
		t.Fatalf("deleted actor: %s", body)
	}
	body = request(http.MethodGet, "/api/operation-logs?action=users.delete&objectId="+fixture.targetID, "", manager, 200)
	if !bytes.Contains(body, []byte(`"objectDeleted":true`)) || !bytes.Contains(body, []byte(`"result":"success"`)) {
		t.Fatalf("delete audit: %s", body)
	}
	// Email reuse creates a new identity and must not revive prior sessions or history.
	selector[0]++
	if _, err = store.CreateInvitation(ctx, fixture.managerID, "New identity", fixture.targetEmail, domain.LocaleEnglish, []string{fixture.roleID}, selector, digest, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err = store.CompleteInvitation(ctx, selector, digest, "hash"); err != nil {
		t.Fatal(err)
	}
	newUser, err := store.FindByCanonicalEmail(ctx, fixture.targetEmail)
	if err != nil || newUser.User.ID == fixture.targetID {
		t.Fatalf("email reuse: %+v %v", newUser, err)
	}
	request(http.MethodGet, "/api/auth/session-status", "", fresh, 401)
	body = request(http.MethodGet, "/api/operation-logs?actorId="+fixture.targetID, "", manager, 200)
	if !bytes.Contains(body, []byte(`"deleted":true`)) {
		t.Fatalf("history rebound on email reuse: %s", body)
	}
	request(http.MethodPost, "/api/users/"+fixture.otherID+"/deactivate", `{"authVersion":1}`, manager, 409)
}
