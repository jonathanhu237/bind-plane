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
	profile := obj(call(assets, "POST", "/api/releases/admin/profiles", `{"name":"ops-profile","vendor":"huawei","command_templates":{"config_backup":"display current-configuration"},"parser_rules":{},"login_prompt_patterns":{},"prompt_patterns":{},"pagination_rules":{},"success_patterns":[],"error_patterns":[]}`, 201))["id"].(string)
	h3cProfile := obj(call(assets, "POST", "/api/releases/admin/profiles", `{"name":"ops-h3c","vendor":"h3c","command_templates":{},"parser_rules":{},"login_prompt_patterns":{},"prompt_patterns":{},"pagination_rules":{},"success_patterns":[],"error_patterns":[]}`, 201))["id"].(string)
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

	// A command profile speaks one vendor's commands. An imported row without a profile takes the
	// default of its vendor or the vendor's only profile, so the H3C switch has the H3C one.
	var profileB string
	if err = db.QueryRow(`SELECT command_profile_id::text FROM bp_switches WHERE id=$1`, switchB).Scan(&profileB); err != nil || profileB != h3cProfile {
		t.Fatalf("the H3C switch uses profile %q, want %q: %v", profileB, h3cProfile, err)
	}
	if rows := fmt.Sprint(importCSV("switches", "name,management_ip,vendor,profile\nOPS-R,192.0.2.120,ruijie,\nOPS-X,192.0.2.121,h3c,ops-profile\n", true)["rows"]); !strings.Contains(rows, "no single command profile for this vendor") || !strings.Contains(rows, errProfileVendor) {
		t.Fatalf("rows without a profile of their vendor: %s", rows)
	}
	// The API holds switches to their profile's vendor, and a switch without a vendor takes it.
	call(assets, "POST", "/api/releases/admin/switches", jsonBody(map[string]any{"name": "VENDOR-MIX", "management_ip": "192.0.2.130", "credential_id": credential, "command_profile_id": profile, "vendor": "h3c"}), 422)
	taken := obj(call(assets, "POST", "/api/releases/admin/switches", jsonBody(map[string]any{"name": "VENDOR-TAKEN", "management_ip": "192.0.2.131", "credential_id": credential, "command_profile_id": h3cProfile}), 201))["id"].(string)
	var takenVendor string
	var versionB int
	if err = db.QueryRow(`SELECT t.vendor,b.version FROM bp_switches t, bp_switches b WHERE t.id=$1 AND b.id=$2`, taken, switchB).Scan(&takenVendor, &versionB); err != nil || takenVendor != "h3c" {
		t.Fatalf("a switch without a vendor has %q: %v", takenVendor, err)
	}
	if body := fmt.Sprint(call(assets, "PATCH", "/api/releases/admin/switches/"+switchB, jsonBody(map[string]any{"version": versionB, "command_profile_id": profile}), 422)); !strings.Contains(body, errProfileVendor) {
		t.Fatalf("moving the H3C switch to a Huawei profile: %s", body)
	}
	if body := fmt.Sprint(call(assets, "PATCH", "/api/releases/admin/profiles/"+h3cProfile, `{"version":1,"vendor":"cisco"}`, 422)); !strings.Contains(body, errProfileInUse) {
		t.Fatalf("changing the vendor of a profile in use: %s", body)
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
	// A backup is overdue a week after the last success, and "never" means no success at all.
	// Neither switch has a network that resolution can use.
	if _, err = db.Exec(`UPDATE bp_switches SET config_succeeded_at=now()-interval '8 days' WHERE id=$1`, switchA); err != nil {
		t.Fatal(err)
	}
	listed := func(filter string) float64 {
		t.Helper()
		return obj(call(ops, "GET", "/api/ops/switches?q=OPS-&"+filter, "", 200))["total"].(float64)
	}
	for filter, want := range map[string]float64{"backup=never": 1, "backup=overdue": 1, "networks=none": 2, "site=SZ": 2, "group=none": 0} {
		if got := listed(filter); got != want {
			t.Fatalf("%s lists %v switches, want %v", filter, got, want)
		}
	}
	call(assets, "POST", "/api/releases/admin/networks", jsonBody(map[string]any{"switch_id": switchA, "cidr": "10.78.0.0/24", "is_validated": true, "vlan": "78", "description": "Lab"}), 201)
	if got := listed("networks=none"); got != 1 {
		t.Fatalf("a validated network still lists switch A as having none (%v)", got)
	}

	// NetSwitch Pro's template imports, updating the switches that exist. Empty cells keep their
	// values, and a cell an export quoted against formulas loses the quote.
	updated := obj(call(assets, "POST", "/api/releases/admin/import", jsonBody(map[string]any{"kind": "switches", "mode": "update", "default_credential_id": credential, "default_profile_id": profile,
		"csv": "name,ip_address,vendor,model,device_type,group_name,protocol,ssh_port,snmp_community_ro,region,tags\n" +
			"OPS-A,192.0.2.101,huawei,S5720,switch,Edge,ssh,2222,public,SZ,x\n,192.0.2.102,,,,,,,,'-Lab,\nOPS-C,192.0.2.103,huawei,,,,,,,,\n"}), 200))
	if updated["updated"] != float64(2) || updated["created"] != float64(1) || updated["committed"] != true || !strings.Contains(fmt.Sprint(updated["rows"]), "changes: group, model, port") {
		t.Fatalf("update import %v", updated)
	}
	var siteB, nameB string
	if err = db.QueryRow(`SELECT site,name FROM bp_switches WHERE id=$1`, switchB).Scan(&siteB, &nameB); err != nil || siteB != "-Lab" || nameB != "OPS-B" {
		t.Fatalf("switch B after the update import: %q %q %v", siteB, nameB, err)
	}
	exported := httptest.NewRecorder()
	exportRequest := httptest.NewRequest("GET", "/api/ops/switches/export?q=OPS-", nil)
	exportRequest.AddCookie(&http.Cookie{Name: "session", Value: "token"})
	ops.ServeHTTP(exported, exportRequest)
	if body := exported.Body.String(); exported.Code != 200 || !strings.HasPrefix(body, "\ufeffname,management_ip,vendor") || !strings.Contains(body, "OPS-B,192.0.2.102,h3c,,Core,'-Lab,telnet") || strings.Count(body, "\n") != 4 {
		t.Fatalf("export %d: %s", exported.Code, body)
	}

	// One change on many switches, recorded per switch; anything but exactly one change is refused.
	if bulk := obj(call(assets, "POST", "/api/releases/admin/switches/bulk", jsonBody(map[string]any{"ids": []string{switchA, switchB, switchB}, "site": "HQ"}), 200)); bulk["changed"] != float64(2) || bulk["unchanged"] != float64(0) {
		t.Fatalf("bulk %v", bulk)
	}
	if bulk := obj(call(assets, "POST", "/api/releases/admin/switches/bulk", jsonBody(map[string]any{"ids": []string{switchA}, "site": "HQ"}), 200)); bulk["changed"] != float64(0) || bulk["unchanged"] != float64(1) {
		t.Fatalf("repeated bulk %v", bulk)
	}
	call(assets, "POST", "/api/releases/admin/switches/bulk", jsonBody(map[string]any{"ids": []string{switchA}, "site": "HQ", "is_enabled": false}), 422)

	// A group a rule targets stays; deleting another ungroups its switches, one audit record each.
	var edge string
	if err = db.QueryRow(`SELECT id::text FROM bp_switch_groups WHERE name='Edge'`).Scan(&edge); err != nil {
		t.Fatal(err)
	}
	call(ops, "POST", "/api/ops/compliance/rules", jsonBody(map[string]any{"name": "Edge only", "pattern": "^sysname", "mode": "must_contain", "group_id": edge}), 201)
	call(assets, "DELETE", "/api/releases/admin/groups/"+edge, "", 409)
	if deleted := obj(call(assets, "DELETE", "/api/releases/admin/groups/"+groupID, "", 200)); deleted["ungrouped"] != float64(1) {
		t.Fatalf("group delete %v", deleted)
	}
	if got := listed("group=none"); got != 2 {
		t.Fatalf("group=none lists %v switches, want switch B and the new switch C", got)
	}
	detail := obj(call(ops, "GET", "/api/ops/switches/"+switchB, "", 200))
	if history := fmt.Sprint(detail["history"]); strings.Count(history, "switch.update") != 3 || !strings.Contains(history, "group_id") {
		t.Fatalf("switch B history %v", history)
	}

	// A connection test starts by hand; schedules keep to port checks and backups.
	if test := obj(call(ops, "POST", "/api/ops/tasks", jsonBody(map[string]any{"kind": "login_check", "scope": "switches", "switch_ids": []string{switchA}}), 202)); test["queued"] != float64(1) {
		t.Fatalf("connection test %v", test)
	}
	call(ops, "POST", "/api/ops/schedules", `{"name":"Sign in hourly","kind":"login_check","cron":"0 * * * *","scope":"all"}`, 422)
	operator, err := NewOpsHandler(db, fakeIdentity{principal: domain.Principal{User: domain.User{ID: actorID}, Roles: []domain.Role{{Name: "Operator"}}}}, "session", "http://localhost:5173", zone)
	if err != nil {
		t.Fatal(err)
	}
	call(operator, "GET", "/api/ops/overview", "", 403)
	call(operator, "GET", fmt.Sprintf("/api/ops/backups/%d", first), "", 403)
}
