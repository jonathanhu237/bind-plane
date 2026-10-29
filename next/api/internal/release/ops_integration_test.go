package release

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

func TestDeviceOperationsThroughTheAPI(t *testing.T) {
	dsn := os.Getenv("BIND_PLANE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated test database required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	var actorID string
	if err = db.QueryRow(`INSERT INTO auth_users(name,email,email_canonical,password_hash) VALUES ('Ops','ops@test.invalid','ops@test.invalid','x') RETURNING id::text`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	admin := fakeIdentity{principal: domain.Principal{User: domain.User{ID: actorID, Email: "ops@test.invalid"}, SuperAdmin: true}}
	zone := time.FixedZone("CST", 8*3600)
	ops, err := NewOpsHandler(db, admin, "session", "http://localhost:5173", zone)
	if err != nil {
		t.Fatal(err)
	}
	assets, err := NewAdminHandler(db, admin, "session", "http://localhost:5173")
	if err != nil {
		t.Fatal(err)
	}
	call := func(h http.Handler, method, path, body string, want int) any {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost:5173")
		r.AddCookie(&http.Cookie{Name: "session", Value: "token"})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, want, w.Body.String())
		}
		var out any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	obj := func(v any) map[string]any { return v.(map[string]any) }
	jsonBody := func(v any) string { b, _ := json.Marshal(v); return string(b) }

	credential := obj(call(assets, "POST", "/api/releases/admin/credentials", `{"Name":"ops-cred","Username":"netops","Password":"secret"}`, 201))["id"].(string)
	profile := obj(call(assets, "POST", "/api/releases/admin/profiles", `{"name":"ops-profile","command_templates":{"config_backup":"display current-configuration"},"parser_rules":{},"login_prompt_patterns":{},"prompt_patterns":{},"pagination_rules":{},"success_patterns":[],"error_patterns":[]}`, 201))["id"].(string)
	importCSV := func(kind, csv string, dryRun bool) map[string]any {
		return obj(call(assets, "POST", "/api/releases/admin/import", jsonBody(map[string]any{"kind": kind, "csv": csv, "dry_run": dryRun, "default_credential_id": credential, "default_profile_id": profile}), 200))
	}

	// An error anywhere blocks the commit; a dry run writes nothing; a re-import skips.
	good := "\ufeff设备名称,IP地址,厂商,分组,校区,协议\nOPS-A,192.0.2.101,Huawei,Core,SZ,ssh\nOPS-B,192.0.2.102,华三,Core,SZ,telnet\n"
	if report := importCSV("switches", good+"bad,999.1.1.1,huawei,,,\n", false); report["errors"] != float64(1) || report["committed"] != false {
		t.Fatalf("import with an error committed: %v", report)
	}
	if report := importCSV("switches", good, true); report["created"] != float64(2) || report["committed"] != false {
		t.Fatalf("dry run: %v", report)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM bp_switches WHERE name LIKE 'OPS-%'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("dry run wrote %d switches: %v", count, err)
	}
	if report := importCSV("switches", good, false); report["created"] != float64(2) || report["committed"] != true || len(report["new_groups"].([]any)) != 1 {
		t.Fatalf("commit: %v", report)
	}
	if report := importCSV("switches", good, false); report["skipped"] != float64(2) || report["created"] != float64(0) {
		t.Fatalf("re-import: %v", report)
	}
	var switchA, switchB, groupID, vendorB, protocolA string
	if err = db.QueryRow(`SELECT a.id::text,b.id::text,a.group_id::text,b.vendor,a.protocol FROM bp_switches a, bp_switches b WHERE a.name='OPS-A' AND b.name='OPS-B'`).Scan(&switchA, &switchB, &groupID, &vendorB, &protocolA); err != nil || vendorB != "h3c" || protocolA != "ssh" {
		t.Fatalf("imported fields %q %q: %v", vendorB, protocolA, err)
	}
	networks := importCSV("networks", "cidr,switch,vlan,validated\n10.77.0.0/24,192.0.2.101,77,true\n10.77.1.5/24,OPS-B,,\n", false)
	if networks["errors"] != float64(1) || !strings.Contains(fmt.Sprint(networks["rows"]), "10.77.1.0/24") {
		t.Fatalf("network import: %v", networks)
	}

	// Manual runs never queue a second pending operation for a switch.
	run := obj(call(ops, "POST", "/api/ops/tasks", jsonBody(map[string]any{"kind": "config_backup", "scope": "group", "group_id": groupID}), 202))
	if run["queued"] != float64(2) {
		t.Fatalf("first run %v", run)
	}
	if again := obj(call(ops, "POST", "/api/ops/tasks", jsonBody(map[string]any{"kind": "config_backup", "scope": "switches", "switch_ids": []string{switchA}}), 202)); again["queued"] != float64(0) || again["skipped"] != float64(1) {
		t.Fatalf("duplicate run %v", again)
	}
	task := obj(call(ops, "GET", "/api/ops/tasks/"+run["task_id"].(string), "", 200))
	if task["queued"] != float64(2) || len(task["items"].([]any)) != 2 {
		t.Fatalf("task detail %v", task)
	}
	call(ops, "POST", "/api/ops/tasks", `{"kind":"config_backup","scope":"switches","switch_ids":["not-a-uuid"]}`, 422)

	// Compliance runs on the latest sealed backup; a critical failure opens an alert.
	store := func(content string) int64 {
		t.Helper()
		sealed, err := sealConfig(ops.key, content)
		if err != nil {
			t.Fatal(err)
		}
		var id int64
		if err = db.QueryRow(`INSERT INTO bp_config_backups(switch_id,content_sha256,size_bytes,line_count,encrypted_content) VALUES ($1,md5($2),length($2),3,$3) RETURNING id`, switchA, content, sealed).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	first := store("sysname OPS-A\n#\nsnmp-agent community read public\n#\nreturn")
	call(ops, "POST", "/api/ops/compliance/rules", `{"name":"bad","pattern":"(unclosed","mode":"must_not_contain"}`, 422)
	rule := obj(call(ops, "POST", "/api/ops/compliance/rules", `{"name":"No public community","pattern":"^snmp-agent community read public","mode":"must_not_contain","severity":"critical"}`, 201))["id"].(string)
	if n, err := ops.evaluateCompliance(ctx); err != nil || n < 1 {
		t.Fatalf("evaluate %d %v", n, err)
	}
	failed := obj(call(ops, "GET", "/api/ops/compliance/results?status=failed", "", 200))
	if !strings.Contains(fmt.Sprint(failed["items"]), "Line 3: snmp-agent community read public") {
		t.Fatalf("failed results %v", failed)
	}
	activeAlerts := func() string { return fmt.Sprint(call(ops, "GET", "/api/ops/alerts", "", 200)) }
	if !strings.Contains(activeAlerts(), "compliance_violation") {
		t.Fatal("critical failure raised no alert")
	}
	second := store("sysname OPS-A\n#\nsnmp-agent community read %^private\n#\nreturn")
	if _, err = ops.evaluateCompliance(ctx); err != nil {
		t.Fatal(err)
	}
	var passed bool
	if err = db.QueryRow(`SELECT passed FROM bp_compliance_results WHERE switch_id=$1 AND rule_id=$2`, switchA, rule).Scan(&passed); err != nil || !passed {
		t.Fatalf("fixed configuration still fails: %v", err)
	}
	if strings.Contains(activeAlerts(), "compliance_violation") {
		t.Fatal("alert not resolved after the fix")
	}

	// Viewing and diffing sealed configurations is audited.
	diff := obj(call(ops, "GET", fmt.Sprintf("/api/ops/backups/%d/diff?base=%d", second, first), "", 200))
	if d := obj(diff["diff"]); d["added"] != float64(1) || d["removed"] != float64(1) {
		t.Fatalf("diff %v", diff)
	}
	if view := obj(call(ops, "GET", fmt.Sprintf("/api/ops/backups/%d", first), "", 200)); !strings.Contains(view["content"].(string), "public") {
		t.Fatalf("view %v", view)
	}
	call(ops, "GET", fmt.Sprintf("/api/ops/backups/%d/diff?base=999999", second), "", 404)
	if err = db.QueryRow(`SELECT count(*) FROM bp_asset_events WHERE actor_id=$1 AND action IN ('config.view','config.diff')`, actorID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("configuration reads audited %d times: %v", count, err)
	}
	if audit := obj(call(ops, "GET", "/api/ops/audit?action=config", "", 200)); len(audit["events"].([]any)) != 2 {
		t.Fatalf("audit filter %v", audit)
	}

	// Schedules fire once when due and move to the next future match.
	call(ops, "POST", "/api/ops/schedules", `{"name":"bad cron","kind":"reachability","cron":"61 * * * *","scope":"all"}`, 422)
	schedule := obj(call(ops, "POST", "/api/ops/schedules", `{"name":"Every five minutes","kind":"reachability","cron":"*/5 * * * *","scope":"all"}`, 201))
	scheduleID := schedule["id"].(string)
	if _, err = db.Exec(`UPDATE bp_schedules SET next_run_at=now()-interval '1 hour' WHERE id=$1`, scheduleID); err != nil {
		t.Fatal(err)
	}
	if err = ops.runDueSchedules(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	var fired int
	var nextRun time.Time
	if err = db.QueryRow(`SELECT (SELECT count(*) FROM bp_device_tasks WHERE schedule_id=$1),next_run_at FROM bp_schedules WHERE id=$1`, scheduleID).Scan(&fired, &nextRun); err != nil || fired != 1 || !nextRun.After(time.Now()) {
		t.Fatalf("schedule fired %d times, next %v: %v", fired, nextRun, err)
	}
	call(ops, "PATCH", "/api/ops/schedules/"+scheduleID, `{"version":1,"is_enabled":false}`, 200)
	var next sql.NullTime
	if err = db.QueryRow(`SELECT next_run_at FROM bp_schedules WHERE id=$1`, scheduleID).Scan(&next); err != nil || next.Valid {
		t.Fatalf("disabled schedule still due at %v: %v", next, err)
	}

	// Alerts are acknowledged once; operators see none of this.
	if err = upsertAlert(ctx, db, "device_unreachable", "warning", switchB, nil, "Port 23 unreachable", "device_unreachable:"+switchB); err != nil {
		t.Fatal(err)
	}
	var alertID int64
	if err = db.QueryRow(`SELECT id FROM bp_alerts WHERE dedupe_key=$1`, "device_unreachable:"+switchB).Scan(&alertID); err != nil {
		t.Fatal(err)
	}
	call(ops, "POST", fmt.Sprintf("/api/ops/alerts/%d/acknowledge", alertID), "", 200)
	call(ops, "POST", fmt.Sprintf("/api/ops/alerts/%d/acknowledge", alertID), "", 409)
	overview := obj(call(ops, "GET", "/api/ops/overview", "", 200))
	if obj(overview["compliance"])["passed"].(float64) < 1 {
		t.Fatalf("overview %v", overview)
	}
	if list := obj(call(ops, "GET", "/api/ops/switches?q=OPS-&backup=never", "", 200)); list["total"] != float64(1) {
		t.Fatalf("switch filter %v", list)
	}
	operator, err := NewOpsHandler(db, fakeIdentity{principal: domain.Principal{User: domain.User{ID: actorID}, Roles: []domain.Role{{Name: "Operator"}}}}, "session", "http://localhost:5173", zone)
	if err != nil {
		t.Fatal(err)
	}
	call(operator, "GET", "/api/ops/overview", "", 403)
	call(operator, "GET", fmt.Sprintf("/api/ops/backups/%d", first), "", 403)
}
