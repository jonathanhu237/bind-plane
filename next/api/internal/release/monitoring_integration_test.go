package release

import (
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

func TestDeviceMonitoringThroughTheAPI(t *testing.T) {
	dsn := os.Getenv("BIND_PLANE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated test database required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var actorID string
	if err = db.QueryRow(`INSERT INTO auth_users(name,email,email_canonical,password_hash) VALUES ('Mon','mon@test.invalid','mon@test.invalid','x') RETURNING id::text`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	admin := fakeIdentity{principal: domain.Principal{User: domain.User{ID: actorID, Email: "mon@test.invalid", Name: "Mon"}, SuperAdmin: true}}
	ops, err := NewOpsHandler(db, admin, "session", "http://localhost:5173", time.UTC)
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
	scalar := func(query string, args ...any) any {
		t.Helper()
		var value any
		if err := db.QueryRow(query, args...).Scan(&value); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return value
	}

	// SNMP credentials: a v2c community, or a v3 user with passwords of at least eight characters,
	// and privacy only on top of authentication. Secrets are never listed.
	for body, problem := range map[string]string{
		`{"name":"","snmp_version":"v2c","community":"x"}`:                                                     errSnmpName,
		`{"name":"bad","snmp_version":"v2c"}`:                                                                  errSnmpCommunity,
		`{"name":"bad","snmp_version":"v3"}`:                                                                   errSnmpUser,
		`{"name":"bad","snmp_version":"v3","username":"u","auth_protocol":"sha","auth_password":"short"}`:      errSnmpPassword,
		`{"name":"bad","snmp_version":"v3","username":"u","priv_protocol":"aes","priv_password":"longenough"}`: errSnmpPrivacy,
		`{"name":"bad","snmp_version":"v1","community":"x"}`:                                                   errSnmpProtocol,
	} {
		if got := fmt.Sprint(call(assets, "POST", "/api/releases/admin/snmp-credentials", body, 422)); !strings.Contains(got, problem) {
			t.Fatalf("%s: %s, want %q", body, got, problem)
		}
	}
	community := obj(call(assets, "POST", "/api/releases/admin/snmp-credentials", `{"name":"mon-v2c","snmp_version":"v2c","community":"campus-ro"}`, 201))["id"].(string)
	secure := obj(call(assets, "POST", "/api/releases/admin/snmp-credentials",
		`{"name":"mon-v3","snmp_version":"v3","username":"monitor","auth_protocol":"sha","auth_password":"authpass1","priv_protocol":"aes","priv_password":"privpass1"}`, 201))["id"].(string)
	listed := fmt.Sprint(obj(call(assets, "GET", "/api/releases/admin/assets", "", 200))["snmp_credentials"])
	if !strings.Contains(listed, "mon-v3") || strings.Contains(listed, "campus-ro") || strings.Contains(listed, "authpass1") || strings.Contains(listed, "v1:") {
		t.Fatalf("listed SNMP credentials: %s", listed)
	}
	// Moving to v3 needs a user; the community it no longer uses is removed, and the audit record says so.
	call(assets, "PATCH", "/api/releases/admin/snmp-credentials/"+community, `{"version":1,"snmp_version":"v3"}`, 422)
	call(assets, "PATCH", "/api/releases/admin/snmp-credentials/"+community, `{"version":1,"snmp_version":"v3","username":"ro","auth_protocol":"md5"}`, 422)
	call(assets, "PATCH", "/api/releases/admin/snmp-credentials/"+community, `{"version":1,"snmp_version":"v3","username":"ro"}`, 200)
	if changes := fmt.Sprint(scalar(`SELECT changes::text FROM bp_asset_events WHERE object_id=$1 AND action='snmp_credential.update'`, community)); !strings.Contains(changes, `"encrypted_community": "cleared"`) {
		t.Fatalf("audit of the version change: %s", changes)
	}
	call(assets, "PATCH", "/api/releases/admin/snmp-credentials/"+community, `{"version":2,"snmp_version":"v2c","community":"campus-ro"}`, 200)
	// A v3 password is kept while authentication stays on, even with another protocol.
	call(assets, "PATCH", "/api/releases/admin/snmp-credentials/"+secure, `{"version":1,"auth_protocol":"sha256"}`, 200)
	if kept := scalar(`SELECT encrypted_auth_password IS NOT NULL AND auth_protocol='sha256' FROM bp_snmp_credentials WHERE id=$1`, secure); kept != true {
		t.Fatalf("auth password after a protocol change: %v", kept)
	}

	// Starters know their vendor's OIDs; a profile created without any reads standard MIBs only.
	if rules := fmt.Sprint(scalar(`SELECT snmp_rules->'cpu'->>0 FROM bp_command_profiles WHERE seed='huawei-vrp'`)); rules != "1.3.6.1.4.1.2011.5.25.31.1.1.1.1.5" {
		t.Fatalf("huawei starter cpu OID %q", rules)
	}
	credential := obj(call(assets, "POST", "/api/releases/admin/credentials", `{"Name":"mon-login","Username":"netops","Password":"secret"}`, 201))["id"].(string)
	profile := obj(call(assets, "POST", "/api/releases/admin/profiles", `{"name":"mon-profile","vendor":"huawei","command_templates":{},"parser_rules":{},"login_prompt_patterns":{},"prompt_patterns":{},"pagination_rules":{},"success_patterns":[],"error_patterns":[]}`, 201))["id"].(string)
	if rules := fmt.Sprint(scalar(`SELECT snmp_rules::text FROM bp_command_profiles WHERE id=$1`, profile)); rules != "{}" {
		t.Fatalf("profile without SNMP rules: %s", rules)
	}
	call(assets, "PATCH", "/api/releases/admin/profiles/"+profile, `{"version":1,"snmp_rules":["1.3.6"]}`, 422)

	newSwitch := func(name, ip, snmp string) string {
		return obj(call(assets, "POST", "/api/releases/admin/switches", jsonBody(map[string]any{"name": name, "management_ip": ip, "credential_id": credential,
			"command_profile_id": profile, "snmp_credential_id": snmp}), 201))["id"].(string)
	}
	watched := newSwitch("MON-A", "198.51.100.11", community)
	quiet := newSwitch("MON-B", "198.51.100.12", "")
	page := func(query string) map[string]any { return obj(call(ops, "GET", "/api/ops/monitoring?"+query, "", 200)) }
	names := func(p map[string]any) string {
		var out []string
		for _, item := range p["items"].([]any) {
			out = append(out, obj(item)["name"].(string))
		}
		return strings.Join(out, ",")
	}
	if p := page("q=MON-"); names(p) != "MON-A" || obj(p["summary"])["waiting"].(float64) < 1 {
		t.Fatalf("monitored switches %s, summary %v", names(p), p["summary"])
	}
	if p := page("q=MON-&state=unmonitored"); names(p) != "MON-B" {
		t.Fatalf("unmonitored switches %s", names(p))
	}
	call(ops, "POST", "/api/ops/switches/"+watched+"/poll", "", 202)
	call(ops, "POST", "/api/ops/switches/"+quiet+"/poll", "", 409)
	if due := scalar(`SELECT next_poll_at <= now() FROM bp_switch_monitoring WHERE switch_id=$1`, watched); due != true {
		t.Fatalf("poll now left the switch due: %v", due)
	}

	// What a poll writes, read back through the device list, the details and the history.
	if _, err = db.Exec(`UPDATE bp_switch_monitoring SET polled_at=now(),succeeded_at=now(),response_ms=12,sys_name='MON-A-SNMP',uptime_seconds=3600,
 cpu_percent=91,memory_percent=40,temperature_c=45,restarted_at=now()-interval '1 hour' WHERE switch_id=$1`, watched); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO bp_switch_interfaces(switch_id,if_index,name,admin_up,oper_up,speed_mbps,in_bps,out_bps,sampled_at)
 VALUES ($1,1,'GE0/0/1',true,true,1000,8000,16000,now()),($1,2,'GE0/0/2',true,false,1000,NULL,NULL,now())`, watched); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO bp_switch_metrics(switch_id,sampled_at,cpu_percent,memory_percent,temperature_c,response_ms)
 SELECT $1,now()-n*interval '1 minute',50+n,40,45,10 FROM generate_series(1,10) n`, watched); err != nil {
		t.Fatal(err)
	}
	if p := page("q=MON-&state=high&sort=cpu"); names(p) != "MON-A" || obj(p["items"].([]any)[0])["interfaces_up"] != float64(1) {
		t.Fatalf("switches over a threshold %s: %v", names(p), p["items"])
	}
	if p := page("q=MON-&state=restarted"); names(p) != "MON-A" {
		t.Fatalf("recently restarted %s", names(p))
	}
	detail := obj(call(ops, "GET", "/api/ops/switches/"+watched, "", 200))
	if detail["monitored"] != true || detail["snmp_credential_name"] != "mon-v2c" || obj(detail["monitoring"])["cpu_percent"] != float64(91) || len(detail["interfaces"].([]any)) != 2 {
		t.Fatalf("details %v %v %v", detail["monitored"], detail["monitoring"], detail["interfaces"])
	}
	history := obj(call(ops, "GET", "/api/ops/switches/"+watched+"/metrics?range=1d", "", 200))
	if points := history["points"].([]any); history["bucket_seconds"] != float64(300) || len(points) < 2 || len(points) > 4 {
		t.Fatalf("1d history buckets five minutes: %v", history)
	}
	if obj(call(ops, "GET", "/api/ops/switches/"+quiet, "", 200))["monitoring"] != nil {
		t.Fatal("an unmonitored switch has monitoring state")
	}

	// Settings: whole and in range, versioned and audited; a shorter interval applies at once.
	settings := obj(page("")["settings"])
	version := int(settings["version"].(float64))
	valid := map[string]any{"version": version, "interval_minutes": 2, "cpu_threshold": 95, "memory_threshold": nil, "temperature_threshold": 70, "consecutive_polls": 2, "alert_on_restart": false}
	for key, value := range map[string]any{"interval_minutes": 0, "cpu_threshold": 101, "consecutive_polls": 11} {
		invalid := map[string]any{}
		for k, v := range valid {
			invalid[k] = v
		}
		invalid[key] = value
		call(ops, "PATCH", "/api/ops/monitoring/settings", jsonBody(invalid), 422)
	}
	if _, err = db.Exec(`UPDATE bp_switch_monitoring SET next_poll_at=now()+interval '1 hour' WHERE switch_id=$1`, watched); err != nil {
		t.Fatal(err)
	}
	call(ops, "PATCH", "/api/ops/monitoring/settings", jsonBody(valid), 200)
	call(ops, "PATCH", "/api/ops/monitoring/settings", jsonBody(valid), 409)
	if soon := scalar(`SELECT next_poll_at <= now()+interval '2 minutes' FROM bp_switch_monitoring WHERE switch_id=$1`, watched); soon != true {
		t.Fatal("a shorter interval did not bring the next poll forward")
	}
	if changes := fmt.Sprint(scalar(`SELECT changes::text FROM bp_asset_events WHERE action='monitoring.update' ORDER BY id DESC LIMIT 1`)); !strings.Contains(changes, `"memory_threshold": {"to": null, "from": 80}`) {
		t.Fatalf("settings audit %s", changes)
	}
	if p := page("q=MON-&state=high"); names(p) != "" {
		t.Fatalf("over a threshold of 95: %s", names(p))
	}

	// A switch no longer monitored forgets its live state and its monitoring alerts, but keeps its samples.
	if _, err = db.Exec(`INSERT INTO bp_alerts(kind,severity,switch_id,message,dedupe_key) VALUES ('cpu_high','warning',$1::uuid,'CPU at 91%','cpu_high:'||$1::text)`, watched); err != nil {
		t.Fatal(err)
	}
	version = int(scalar(`SELECT version FROM bp_switches WHERE id=$1`, watched).(int64))
	call(assets, "PATCH", "/api/releases/admin/switches/"+watched, jsonBody(map[string]any{"version": version, "snmp_credential_id": ""}), 200)
	if left := fmt.Sprint(scalar(`SELECT (SELECT count(*) FROM bp_switch_monitoring WHERE switch_id=$1)+(SELECT count(*) FROM bp_switch_interfaces WHERE switch_id=$1)
 +(SELECT count(*) FROM bp_alerts WHERE switch_id=$1 AND status<>'resolved')`, watched)); left != "0" {
		t.Fatalf("state left after monitoring stopped: %s", left)
	}
	if samples := scalar(`SELECT count(*) FROM bp_switch_metrics WHERE switch_id=$1`, watched); samples != int64(10) {
		t.Fatalf("samples after monitoring stopped: %v", samples)
	}

	// Bulk: an active SNMP credential for many switches, or none; a disabled one is refused.
	if bulk := obj(call(assets, "POST", "/api/releases/admin/switches/bulk", jsonBody(map[string]any{"ids": []string{watched, quiet}, "snmp_credential_id": secure}), 200)); bulk["changed"] != float64(2) {
		t.Fatalf("bulk SNMP credential %v", bulk)
	}
	if p := page("q=MON-"); names(p) != "MON-A,MON-B" {
		t.Fatalf("monitored after the bulk change %s", names(p))
	}
	call(ops, "POST", "/api/ops/switches/"+quiet+"/poll", "", 202)
	call(assets, "PATCH", "/api/releases/admin/snmp-credentials/"+secure, `{"version":2,"is_active":false}`, 200)
	if left := scalar(`SELECT count(*) FROM bp_switch_monitoring WHERE switch_id IN ($1,$2)`, watched, quiet); left != int64(0) {
		t.Fatalf("monitoring rows left after the SNMP credential was disabled: %v", left)
	}
	call(assets, "POST", "/api/releases/admin/switches/bulk", jsonBody(map[string]any{"ids": []string{watched}, "snmp_credential_id": secure}), 409)
	call(assets, "POST", "/api/releases/admin/switches/bulk", jsonBody(map[string]any{"ids": []string{watched}, "snmp_credential_id": "nope"}), 422)
	if bulk := obj(call(assets, "POST", "/api/releases/admin/switches/bulk", jsonBody(map[string]any{"ids": []string{watched, quiet}, "snmp_credential_id": ""}), 200)); bulk["changed"] != float64(2) {
		t.Fatalf("bulk removal %v", bulk)
	}

	// Import and export carry the SNMP credential by name.
	report := obj(call(assets, "POST", "/api/releases/admin/import", jsonBody(map[string]any{"kind": "switches", "mode": "update", "default_credential_id": credential, "default_profile_id": profile,
		"csv": "name,management_ip,snmp_credential\nMON-A,198.51.100.11,mon-v2c\nMON-C,198.51.100.13,mon-v2c\nMON-D,198.51.100.14,missing\n"}), 200))
	if rows := fmt.Sprint(report["rows"]); !strings.Contains(rows, "SNMP credential not found") || report["committed"] != false {
		t.Fatalf("import with an unknown SNMP credential: %v", report)
	}
	report = obj(call(assets, "POST", "/api/releases/admin/import", jsonBody(map[string]any{"kind": "switches", "mode": "update", "default_credential_id": credential, "default_profile_id": profile,
		"csv": "name,management_ip,snmp_credential\nMON-A,198.51.100.11,mon-v2c\nMON-C,198.51.100.13,mon-v2c\n"}), 200))
	if report["created"] != float64(1) || report["updated"] != float64(1) || !strings.Contains(fmt.Sprint(report["rows"]), "snmp_credential") {
		t.Fatalf("import with SNMP credentials: %v", report)
	}
	exported := httptest.NewRecorder()
	exportRequest := httptest.NewRequest("GET", "/api/ops/switches/export?q=MON-C", nil)
	exportRequest.AddCookie(&http.Cookie{Name: "session", Value: "token"})
	ops.ServeHTTP(exported, exportRequest)
	if body := exported.Body.String(); !strings.Contains(body, "handles_release,snmp_credential,enabled") || !strings.Contains(body, ",no,mon-v2c,true,") {
		t.Fatalf("export %s", body)
	}
}
