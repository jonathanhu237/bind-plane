package release

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

func TestAssetEditsAreVersionedAuditedAndFenceReleases(t *testing.T) {
	dsn := os.Getenv("BIND_PLANE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated test database required")
	}
	if _, err := base64.RawURLEncoding.DecodeString(os.Getenv("BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY")); err != nil {
		t.Fatal("isolated encryption key required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	var actor string
	if err = db.QueryRow(`INSERT INTO auth_users(name,email,email_canonical,password_hash) VALUES ('Admin','edit@test.invalid','edit@test.invalid','test') RETURNING id::text`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	h, err := NewAdminHandler(db, fakeIdentity{principal: domain.Principal{User: domain.User{ID: actor, Email: "edit@test.invalid"}, SuperAdmin: true}}, "session", "http://localhost:5173")
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body string, want int) map[string]any {
		t.Helper()
		r := httptest.NewRequest(method, "/api/releases/admin/"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost:5173")
		r.AddCookie(&http.Cookie{Name: "session", Value: "token"})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, want, w.Body.String())
		}
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	events := func(id string) (count int, latest string) {
		t.Helper()
		if err := db.QueryRow(`SELECT count(*) FROM bp_asset_events WHERE object_id=$1`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT changes::text FROM bp_asset_events WHERE object_id=$1 ORDER BY id DESC LIMIT 1`, id).Scan(&latest); err != nil {
			t.Fatal(err)
		}
		return count, latest
	}

	credential := call("POST", "credentials", `{"Name":"edit-cred","Username":"device","Password":"first-switch-secret"}`, 201)["id"].(string)
	profile := call("POST", "profiles", `{"name":"edit-profile","command_templates":{"single_arp_query":"show arp $ip"},"parser_rules":{},"login_prompt_patterns":{},"prompt_patterns":{},"pagination_rules":{},"success_patterns":[],"error_patterns":[]}`, 201)["id"].(string)
	switchA := call("POST", "switches", fmt.Sprintf(`{"name":"edit-switch-a","management_ip":"192.0.2.21","credential_id":%q,"command_profile_id":%q}`, credential, profile), 201)["id"].(string)
	switchB := call("POST", "switches", fmt.Sprintf(`{"name":"edit-switch-b","management_ip":"192.0.2.22","credential_id":%q,"command_profile_id":%q}`, credential, profile), 201)["id"].(string)
	network := call("POST", "networks", fmt.Sprintf(`{"switch_id":%q,"cidr":"10.30.0.0/16","is_validated":true}`, switchA), 201)["id"].(string)

	// A rotated password is audited as replaced, never as plaintext or ciphertext.
	if got := call("PATCH", "credentials/"+credential, `{"version":1,"name":"edit-cred-renamed","password":"rotated-switch-secret"}`, 200); got["version"] != float64(2) {
		t.Fatalf("version not bumped: %v", got)
	}
	count, changes := events(credential)
	if count != 2 || !strings.Contains(changes, `"encrypted_password": "replaced"`) || !strings.Contains(changes, "edit-cred-renamed") || strings.Contains(changes, "rotated-switch-secret") || strings.Contains(changes, "v1:") {
		t.Fatalf("credential audit %d %s", count, changes)
	}
	call("PATCH", "credentials/"+credential, `{"version":1,"description":"stale"}`, 409)
	// Unchanged values and reformatted JSON neither bump the version nor write audit.
	if got := call("PATCH", "credentials/"+credential, `{"version":2,"name":" edit-cred-renamed "}`, 200); got["version"] != float64(2) {
		t.Fatalf("no-op edit bumped version: %v", got)
	}
	if got := call("PATCH", "profiles/"+profile, `{"version":1,"command_templates":{ "single_arp_query" : "show arp $ip" }}`, 200); got["version"] != float64(1) {
		t.Fatalf("reformatted JSON bumped version: %v", got)
	}
	if count, _ = events(credential); count != 2 {
		t.Fatalf("no-op edit audited: %d", count)
	}
	call("PATCH", "switches/"+switchB, `{"version":1,"management_ip":"192.0.2.21"}`, 409)

	store := NewSQLStore(db)
	start := func(ip string) Job {
		t.Helper()
		started, err := store.Start(ctx, StartInput{TargetIP: ip, Reason: "temporary_test", ActorID: actor, ActorEmail: "edit@test.invalid"})
		if err != nil || started.JobID == "" {
			t.Fatalf("start %+v %v", started, err)
		}
		return started
	}

	// A job pins the switch revision it was queued against; the worker refuses it once that revision is stale.
	queued := start("10.30.1.5")
	call("PATCH", "switches/"+switchA, `{"version":1,"management_ip":"192.0.2.23"}`, 200)
	var pinned int
	var snapshot string
	if err = db.QueryRow(`SELECT switch_version,host(switch_management_ip) FROM bp_release_jobs WHERE id=$1`, queued.JobID).Scan(&pinned, &snapshot); err != nil || pinned != 1 || snapshot != "192.0.2.21" {
		t.Fatalf("job pin %d %q %v", pinned, snapshot, err)
	}
	moved := start("10.30.1.6")
	if moved.SwitchManagementIP != "192.0.2.23" {
		t.Fatalf("job snapshot %+v", moved)
	}
	call("PATCH", "networks/"+network, fmt.Sprintf(`{"version":1,"switch_id":%q}`, switchB), 200)

	// A retry resolves again and pins the new owner's current revision.
	if _, err = db.ExecContext(ctx, `UPDATE bp_release_jobs SET status='failed',phase='finished' WHERE id=$1`, moved.JobID); err != nil {
		t.Fatal(err)
	}
	retried, err := store.Retry(ctx, moved.JobID, actor, "edit@test.invalid", false)
	if err != nil || retried.Kind != "release" || retried.SwitchID != switchB || retried.SwitchName != "edit-switch-b" || retried.SwitchManagementIP != "192.0.2.22" {
		t.Fatalf("retry %+v %v", retried, err)
	}

	// A disabled switch no longer resolves.
	call("PATCH", "switches/"+switchB, `{"version":1,"is_enabled":false}`, 200)
	if stopped, err := store.Start(ctx, StartInput{TargetIP: "10.30.1.7", Reason: "temporary_test", ActorID: actor, ActorEmail: "edit@test.invalid"}); err != nil || stopped.Status != "stopped_no_switch" {
		t.Fatalf("disabled switch resolved: %+v %v", stopped, err)
	}
	if _, latest := events(switchB); !strings.Contains(latest, `"is_enabled": {"to": false, "from": true}`) {
		t.Fatalf("disable audit %s", latest)
	}
}
