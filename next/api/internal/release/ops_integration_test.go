package release

import (
	"archive/zip"
	"bytes"
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
	// A new install starts with a profile per command dialect; H3C has exactly one.
	var starterH3C string
	var starters int
	if err = db.QueryRow(`SELECT (SELECT id::text FROM bp_command_profiles WHERE seed='h3c-comware'),(SELECT count(*) FROM bp_command_profiles WHERE seed IS NOT NULL)`).Scan(&starterH3C, &starters); err != nil || starters != 5 {
		t.Fatalf("starter profiles: %d, %v", starters, err)
	}
	importCSV := func(kind, csv string, dryRun bool) map[string]any {
		return obj(call(assets, "POST", "/api/releases/admin/import", jsonBody(map[string]any{"kind": kind, "csv": csv, "dry_run": dryRun, "default_credential_id": credential, "default_profile_id": profile}), 200))
	}

	// An error anywhere blocks the commit; a dry run writes nothing; a re-import skips.
	good := "\ufeff设备名称,IP地址,厂商,分组,校区,协议,负责解绑\nOPS-A,192.0.2.101,Huawei,Core,SZ,ssh,是\nOPS-B,192.0.2.102,华三,Core,SZ,telnet,\n"
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
	// default of its vendor or the vendor's only profile, so the H3C switch has the starter one.
	var profileB string
	if err = db.QueryRow(`SELECT command_profile_id::text FROM bp_switches WHERE id=$1`, switchB).Scan(&profileB); err != nil || profileB != starterH3C {
		t.Fatalf("the H3C switch uses profile %q, want %q: %v", profileB, starterH3C, err)
	}
	if rows := fmt.Sprint(importCSV("switches", "name,management_ip,vendor,profile\nOPS-R,192.0.2.120,cisco,\nOPS-X,192.0.2.121,h3c,ops-profile\n", true)["rows"]); !strings.Contains(rows, "no single command profile for this vendor") || !strings.Contains(rows, errProfileVendor) {
		t.Fatalf("rows without a profile of their vendor: %s", rows)
	}
	// Starters are ordinary profiles that people can edit.
	call(assets, "PATCH", "/api/releases/admin/profiles/"+starterH3C, `{"version":1,"name":"H3C Comware（本校）"}`, 200)
	h3cProfile := obj(call(assets, "POST", "/api/releases/admin/profiles", `{"name":"ops-h3c","vendor":"h3c","command_templates":{},"parser_rules":{},"login_prompt_patterns":{},"prompt_patterns":{},"pagination_rules":{},"success_patterns":[],"error_patterns":[]}`, 201))["id"].(string)
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
	// Networks are read from configuration, never typed in or imported.
	call(assets, "POST", "/api/releases/admin/import", jsonBody(map[string]any{"kind": "networks", "csv": "cidr,switch\n10.77.0.0/24,OPS-A\n"}), 422)
	call(assets, "POST", "/api/releases/admin/networks", jsonBody(map[string]any{"switch_id": switchA, "cidr": "10.77.0.0/24"}), 404)
	// A release switch never read gets a network read at once; other switches get none, by hand either.
	if err = ops.queueNetworkReads(ctx); err != nil {
		t.Fatal(err)
	}
	var readA, readB int
	if err = db.QueryRow(`SELECT count(*) FILTER (WHERE switch_id=$1),count(*) FILTER (WHERE switch_id=$2) FROM bp_device_task_items WHERE kind='network_read' AND status='queued'`, switchA, switchB).Scan(&readA, &readB); err != nil || readA != 1 || readB != 0 {
		t.Fatalf("network reads queued: A %d, B %d, %v", readA, readB, err)
	}
	if err = ops.queueNetworkReads(ctx); err != nil {
		t.Fatal(err)
	}
	if byHand := obj(call(ops, "POST", "/api/ops/tasks", jsonBody(map[string]any{"kind": "network_read", "scope": "switches", "switch_ids": []string{switchA, switchB}}), 202)); byHand["queued"] != float64(0) || byHand["skipped"] != float64(1) {
		t.Fatalf("network read by hand %v", byHand)
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

	// Configurations download one version as a file, or the newest version of every switch the
	// device list filter matches as one zip. Each download lands in the switch's history first.
	fetch := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: "token"})
		w := httptest.NewRecorder()
		ops.ServeHTTP(w, r)
		return w
	}
	file := fetch(fmt.Sprintf("/api/ops/backups/%d/download", first))
	if disposition := file.Header().Get("Content-Disposition"); file.Code != 200 || !strings.HasPrefix(file.Header().Get("Content-Type"), "text/plain") ||
		!strings.Contains(disposition, "filename=OPS-A_192.0.2.101_") || !strings.HasSuffix(disposition, ".cfg") || !strings.Contains(file.Body.String(), "community read public") {
		t.Fatalf("download %d %v: %s", file.Code, file.Header(), file.Body.String())
	}
	call(ops, "GET", "/api/ops/backups/999999/download", "", 404)
	bundle := fetch("/api/ops/backups/export?q=OPS-")
	if bundle.Code != 200 || bundle.Header().Get("Content-Type") != "application/zip" || !strings.Contains(bundle.Header().Get("Content-Disposition"), ".zip") {
		t.Fatalf("export %d %v", bundle.Code, bundle.Header())
	}
	archive, err := zip.NewReader(bytes.NewReader(bundle.Body.Bytes()), int64(bundle.Body.Len()))
	if err != nil || len(archive.File) != 1 || !strings.HasPrefix(archive.File[0].Name, "OPS-A_192.0.2.101_") {
		t.Fatalf("export holds %v: %v", archive, err) // only OPS-A has versions, and only its newest goes
	}
	entry, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	var newest bytes.Buffer
	if _, err = newest.ReadFrom(entry); err != nil || !strings.Contains(newest.String(), "%^private") {
		t.Fatalf("exported %q: %v", newest.String(), err)
	}
	if got := fmt.Sprint(call(ops, "GET", "/api/ops/backups/export?ids="+switchB, "", 404)); !strings.Contains(got, "no configuration backups to export") {
		t.Fatalf("export of a switch without versions: %s", got)
	}
	call(ops, "GET", "/api/ops/backups/export?ids=not-a-switch", "", 422)
	if err = db.QueryRow(`SELECT count(*) FROM bp_asset_events WHERE object_id=$1 AND action IN ('config.download','config.export')`, switchA).Scan(&count); err != nil || count != 2 {
		t.Fatalf("downloads recorded %d times on the switch: %v", count, err)
	}

	// The audit log exports every event its filters match, and the export is recorded too.
	exportedAudit := obj(call(ops, "GET", "/api/ops/audit/export?action=config", "", 200))
	if events := exportedAudit["events"].([]any); len(events) != 4 || exportedAudit["truncated"] != false || obj(events[0])["action"] != "config.export" {
		t.Fatalf("audit export %v", exportedAudit)
	}
	if err = db.QueryRow(`SELECT count(*) FROM bp_asset_events WHERE action='audit.export' AND object_id=$1 AND object_name='4 events'`, actorID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit export recorded %d times: %v", count, err)
	}

	// The home page's trend counts, per day, the switches whose configuration changed (a first
	// backup is a baseline, not a change); a day lists each change with the version before it.
	trend := obj(call(ops, "GET", "/api/ops/overview", "", 200))["trend"].([]any)
	today := time.Now().UTC().Format("2006-01-02")
	if last := obj(trend[len(trend)-1]); len(trend) != 30 || last["date"] != today || last["changed"] != float64(1) {
		t.Fatalf("trend ends %v (%d days)", last, len(trend))
	}
	day := obj(call(ops, "GET", "/api/ops/changes?date="+today, "", 200))
	if changed := day["changed"].([]any); len(changed) != 1 || obj(changed[0])["backup_id"] != float64(second) || obj(changed[0])["previous_id"] != float64(first) {
		t.Fatalf("changes on %s: %v", today, day)
	}
	call(ops, "GET", "/api/ops/changes?date=yesterday", "", 422)

	// Configuration search reads the newest version of each switch, signs in to nothing, and is
	// recorded with its text, since matched lines can hold secrets.
	found := obj(call(ops, "GET", "/api/ops/config-search?q=OPS-&text=COMMUNITY", "", 200))
	if found["matched"] != float64(1) || found["searched"] != float64(1) || found["lines"] != float64(1) || found["without_backup"].(float64) < 1 {
		t.Fatalf("search %v", found)
	}
	hit := obj(found["results"].([]any)[0])
	if line := obj(hit["matches"].([]any)[0]); hit["switch_name"] != "OPS-A" || hit["backup_id"] != float64(second) || line["line"] != float64(3) || !strings.Contains(line["text"].(string), "%^private") {
		t.Fatalf("search hit %v", hit) // the newest version only: the first one said "public"
	}
	if none := obj(call(ops, "GET", "/api/ops/config-search?q=OPS-&text=public", "", 200)); none["matched"] != float64(0) {
		t.Fatalf("an older version matched: %v", none)
	}
	if pattern := obj(call(ops, "GET", "/api/ops/config-search?q=OPS-&mode=regex&text=%5Esnmp-agent+community+read+%5CS%2B%24", "", 200)); pattern["matched"] != float64(1) {
		t.Fatalf("pattern search %v", pattern)
	}
	call(ops, "GET", "/api/ops/config-search?text=x", "", 422)
	if bad := fmt.Sprint(call(ops, "GET", "/api/ops/config-search?mode=regex&text=%28unclosed", "", 422)); !strings.Contains(bad, "invalid pattern") {
		t.Fatalf("bad pattern: %s", bad)
	}
	if err = db.QueryRow(`SELECT count(*) FROM bp_asset_events WHERE action='config.search' AND object_id=$1`, actorID).Scan(&count); err != nil || count != 3 {
		t.Fatalf("searches recorded %d times: %v", count, err)
	}

	// Schedules fire once when due and move to the next future match. They only back up: whether
	// switches are up is Zabbix's to watch, so there is no port check to schedule or run.
	call(ops, "POST", "/api/ops/schedules", `{"name":"bad cron","kind":"config_backup","cron":"61 * * * *","scope":"all"}`, 422)
	call(ops, "POST", "/api/ops/schedules", `{"name":"Port check","kind":"reachability","cron":"*/5 * * * *","scope":"all"}`, 422)
	call(ops, "POST", "/api/ops/tasks", `{"kind":"reachability","scope":"all"}`, 422)
	schedule := obj(call(ops, "POST", "/api/ops/schedules", `{"name":"Every five minutes","kind":"config_backup","cron":"*/5 * * * *","scope":"all"}`, 201))
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
	if err = upsertAlert(ctx, db, "backup_failed", "warning", switchB, nil, "Backup failed: Timed out", "backup_failed:"+switchB); err != nil {
		t.Fatal(err)
	}
	var alertID int64
	if err = db.QueryRow(`SELECT id FROM bp_alerts WHERE dedupe_key=$1`, "backup_failed:"+switchB).Scan(&alertID); err != nil {
		t.Fatal(err)
	}
	call(ops, "POST", fmt.Sprintf("/api/ops/alerts/%d/acknowledge", alertID), "", 200)
	call(ops, "POST", fmt.Sprintf("/api/ops/alerts/%d/acknowledge", alertID), "", 409)
	overview := obj(call(ops, "GET", "/api/ops/overview", "", 200))
	if obj(overview["compliance"])["passed"].(float64) < 1 {
		t.Fatalf("overview %v", overview)
	}
	// The connection a switch shows is its last connection test or sign-in: a port that did not
	// answer is a failed connection, the same as a refused password.
	if _, err = db.Exec(`UPDATE bp_switches SET login_ok=false,login_error='Port 23 unreachable: Timed out',login_checked_at=now() WHERE id=$1`, switchB); err != nil {
		t.Fatal(err)
	}
	if failing := obj(call(ops, "GET", "/api/ops/switches?q=OPS-&connection=failed", "", 200)); failing["total"] != float64(1) || obj(failing["items"].([]any)[0])["id"] != switchB {
		t.Fatalf("connection filter %v", failing)
	}
	if counts := obj(obj(call(ops, "GET", "/api/ops/overview", "", 200))["switches"]); counts["login_failed"].(float64) < 1 || counts["reachable"] != nil {
		t.Fatalf("overview switches %v", counts)
	}
	// The software version the worker reads from backups lists, filters and offers its values.
	if _, err = db.Exec(`UPDATE bp_switches SET os_version='V200R019C10SPC500' WHERE id=$1`, switchA); err != nil {
		t.Fatal(err)
	}
	byVersion := obj(call(ops, "GET", "/api/ops/switches?q=OPS-&version=V200R019C10SPC500", "", 200))
	if byVersion["total"] != float64(1) || obj(byVersion["items"].([]any)[0])["os_version"] != "V200R019C10SPC500" ||
		!strings.Contains(fmt.Sprint(byVersion["versions"]), "V200R019C10SPC500") {
		t.Fatalf("version filter %v", byVersion)
	}
	if unknown := obj(call(ops, "GET", "/api/ops/switches?q=OPS-&version=unknown", "", 200)); unknown["total"] != float64(1) || obj(unknown["items"].([]any)[0])["id"] != switchB {
		t.Fatalf("unknown version filter %v", unknown)
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
	for filter, want := range map[string]float64{"backup=never": 1, "backup=overdue": 1, "networks=none": 1, "networks=release": 1, "networks=other": 1, "site=SZ": 2, "group=none": 0} {
		if got := listed(filter); got != want {
			t.Fatalf("%s lists %v switches, want %v", filter, got, want)
		}
	}
	if _, err = db.Exec(`INSERT INTO bp_networks(switch_id,cidr,is_validated,vlan,description,interface) VALUES ($1,'10.78.0.0/24',true,'78','Lab','Vlanif78')`, switchA); err != nil {
		t.Fatal(err)
	}
	if got := listed("networks=none"); got != 0 {
		t.Fatalf("a network read still lists release switch A as having none (%v)", got)
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

	// Many switches move to another credential; a disabled or missing one is refused.
	other := obj(call(assets, "POST", "/api/releases/admin/credentials", `{"Name":"ops-cred-2","Username":"netops2","Password":"secret2"}`, 201))["id"].(string)
	if bulk := obj(call(assets, "POST", "/api/releases/admin/switches/bulk", jsonBody(map[string]any{"ids": []string{switchA}, "credential_id": other}), 200)); bulk["changed"] != float64(1) {
		t.Fatalf("bulk credential %v", bulk)
	}
	var credentialA string
	if err = db.QueryRow(`SELECT credential_id::text FROM bp_switches WHERE id=$1`, switchA).Scan(&credentialA); err != nil || credentialA != other {
		t.Fatalf("switch A credential %q %v", credentialA, err)
	}
	if _, err = db.Exec(`UPDATE bp_credentials SET is_active=false WHERE id=$1`, credential); err != nil {
		t.Fatal(err)
	}
	call(assets, "POST", "/api/releases/admin/switches/bulk", jsonBody(map[string]any{"ids": []string{switchA}, "credential_id": credential}), 409)
	call(assets, "POST", "/api/releases/admin/switches/bulk", jsonBody(map[string]any{"ids": []string{switchA}, "credential_id": "00000000-0000-4000-8000-000000000000"}), 409)
	call(assets, "POST", "/api/releases/admin/switches/bulk", jsonBody(map[string]any{"ids": []string{switchA}, "credential_id": ""}), 422)
	if _, err = db.Exec(`UPDATE bp_credentials SET is_active=true WHERE id=$1`, credential); err != nil {
		t.Fatal(err)
	}

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
