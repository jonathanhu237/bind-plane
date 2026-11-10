package release

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// OpsHandler serves device operations: inventory status, configuration backups,
// task runs, schedules, compliance, alerts and the audit trail. Every endpoint is
// admin-only; operators keep the release workflow and nothing else.
type OpsHandler struct {
	*AdminHandler
	mux *http.ServeMux
	loc *time.Location
}

func NewOpsHandler(db *sql.DB, identity Identity, cookie, origin string, loc *time.Location) (*OpsHandler, error) {
	core, err := newAdminCore(db, identity, cookie, origin)
	if err != nil {
		return nil, err
	}
	o := &OpsHandler{AdminHandler: core, mux: http.NewServeMux(), loc: loc}
	routes := map[string]http.HandlerFunc{
		"GET /api/ops/overview":                 o.overview,
		"GET /api/ops/switches":                 o.switches,
		"GET /api/ops/switches/export":          o.exportSwitches,
		"GET /api/ops/switches/{id}":            o.switchDetail,
		"GET /api/ops/backups/{id}":             o.backup,
		"GET /api/ops/backups/{id}/diff":        o.backupDiff,
		"GET /api/ops/tasks":                    o.tasks,
		"POST /api/ops/tasks":                   o.runTask,
		"GET /api/ops/tasks/{id}":               o.task,
		"GET /api/ops/schedules":                o.schedules,
		"POST /api/ops/schedules":               o.createSchedule,
		"PATCH /api/ops/schedules/{id}":         o.editSchedule,
		"POST /api/ops/schedules/{id}/run":      o.runSchedule,
		"GET /api/ops/compliance/rules":         o.rules,
		"POST /api/ops/compliance/rules":        o.createRule,
		"PATCH /api/ops/compliance/rules/{id}":  o.editRule,
		"GET /api/ops/compliance/results":       o.results,
		"GET /api/ops/alerts":                   o.alerts,
		"POST /api/ops/alerts/{id}/acknowledge": o.acknowledge,
		"GET /api/ops/audit":                    o.audit,
	}
	for pattern, route := range routes {
		o.mux.HandleFunc(pattern, route)
	}
	return o, nil
}

func (o *OpsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) { o.mux.ServeHTTP(w, r) }

// jsonColumns are read as text and passed through as the JSON they hold.
var jsonColumns = map[string]bool{"changes": true}

// rows scans a result set into maps keyed by column; "-" discards a column.
func (o *OpsHandler) rows(ctx context.Context, keys []string, query string, args ...any) ([]map[string]any, error) {
	rows, err := o.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		values, ptrs := make([]any, len(keys)), make([]any, len(keys))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		item := make(map[string]any, len(keys))
		for i, key := range keys {
			if key == "-" {
				continue
			}
			item[key] = values[i]
			if text, ok := values[i].(string); ok && jsonColumns[key] {
				item[key] = json.RawMessage(text)
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// paged pops the window-function total that list queries carry in their last column.
func paged(items []map[string]any) (int64, []map[string]any) {
	var total int64
	for _, item := range items {
		if n, ok := item["total"].(int64); ok {
			total = n
		}
		delete(item, "total")
	}
	return total, items
}

func pageArgs(r *http.Request) (limit, offset int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	return 50, (page - 1) * 50
}

func (o *OpsHandler) unavailable(w http.ResponseWriter, err error) {
	log.Printf("device operations: %v", err)
	fail(w, 503, "device operations store unavailable")
}

func (o *OpsHandler) audit(w http.ResponseWriter, r *http.Request) {
	if _, ok := o.authorize(w, r, false); !ok {
		return
	}
	q := r.URL.Query()
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	items, err := o.rows(r.Context(), []string{"id", "actor_name", "actor_email", "action", "object_id", "object_name", "changes", "created_at"},
		`SELECT id,actor_name,actor_email,action,object_id::text,object_name,changes::text,created_at FROM bp_asset_events
 WHERE ($1='' OR action LIKE $1||'.%') AND ($2='' OR actor_email ILIKE '%'||$2||'%' OR actor_name ILIKE '%'||$2||'%') AND ($3='' OR object_name ILIKE '%'||$3||'%') AND ($4=0 OR id<$4)
 ORDER BY id DESC LIMIT 51`, q.Get("action"), q.Get("actor"), q.Get("q"), before)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	var next any
	if len(items) > 50 {
		items = items[:50]
		next = items[49]["id"]
	}
	write(w, 200, map[string]any{"events": items, "next_before": next})
}

func (o *OpsHandler) overview(w http.ResponseWriter, r *http.Request) {
	if _, ok := o.authorize(w, r, false); !ok {
		return
	}
	ctx := r.Context()
	var total, enabled, reachable, unreachable, unknown, backedUp, failing, never, versions24h, passed, failed, failingSwitches int64
	err := o.db.QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE is_enabled),
 count(*) FILTER (WHERE is_enabled AND reachable), count(*) FILTER (WHERE is_enabled AND NOT reachable), count(*) FILTER (WHERE is_enabled AND reachable IS NULL),
 count(*) FILTER (WHERE is_enabled AND EXISTS (SELECT 1 FROM bp_config_backups b WHERE b.switch_id=s.id)),
 count(*) FILTER (WHERE is_enabled AND config_error IS NOT NULL),
 count(*) FILTER (WHERE is_enabled AND NOT EXISTS (SELECT 1 FROM bp_config_backups b WHERE b.switch_id=s.id)),
 (SELECT count(*) FROM bp_config_backups WHERE captured_at > now()-interval '24 hours'),
 (SELECT count(*) FROM bp_compliance_results WHERE passed), (SELECT count(*) FROM bp_compliance_results WHERE NOT passed),
 (SELECT count(DISTINCT switch_id) FROM bp_compliance_results WHERE NOT passed)
 FROM bp_switches s`).Scan(&total, &enabled, &reachable, &unreachable, &unknown, &backedUp, &failing, &never, &versions24h, &passed, &failed, &failingSwitches)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	alerts, err := o.rows(ctx, []string{"severity", "count"}, `SELECT severity,count(*) FROM bp_alerts WHERE status<>'resolved' GROUP BY severity`)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	tasks, err := o.taskSummaries(ctx, "", 6)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	changes, err := o.rows(ctx, []string{"id", "switch_id", "switch_name", "management_ip", "captured_at", "line_count"},
		`SELECT b.id,b.switch_id::text,s.name,host(s.management_ip),b.captured_at,b.line_count FROM bp_config_backups b JOIN bp_switches s ON s.id=b.switch_id ORDER BY b.captured_at DESC,b.id DESC LIMIT 8`)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	upcoming, err := o.rows(ctx, []string{"id", "name", "kind", "next_run_at"},
		`SELECT id::text,name,kind,next_run_at FROM bp_schedules WHERE is_enabled AND next_run_at IS NOT NULL ORDER BY next_run_at LIMIT 5`)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 200, map[string]any{
		"switches":       map[string]int64{"total": total, "enabled": enabled, "reachable": reachable, "unreachable": unreachable, "unknown": unknown},
		"backups":        map[string]int64{"backed_up": backedUp, "failing": failing, "never": never, "versions_24h": versions24h},
		"compliance":     map[string]int64{"passed": passed, "failed": failed, "failing_switches": failingSwitches},
		"alerts":         alerts,
		"recent_tasks":   tasks,
		"recent_changes": changes,
		"upcoming":       upcoming,
	})
}

// switchFilter is the condition the device list and its export share; its arguments start at $1.
// A backup is overdue when the last successful one is more than seven days old. The release
// filter picks the switches that handle releases ("release"), those of them without a network
// release resolution can use ("none"), or the rest ("other").
const switchFilter = `($1='' OR s.name ILIKE '%'||$1||'%' OR host(s.management_ip) LIKE $1||'%' OR s.site ILIKE '%'||$1||'%')
 AND ($2='' OR $2='none' AND s.group_id IS NULL OR s.group_id::text=$2) AND ($3='' OR s.vendor=$3) AND ($4='' OR s.site=$4)
 AND ($5='' OR $5='reachable' AND s.reachable OR $5='unreachable' AND NOT s.reachable OR $5='unknown' AND s.reachable IS NULL
  OR $5='login_failed' AND s.reachable AND NOT s.login_ok)
 AND ($6='' OR $6='failed' AND s.config_error IS NOT NULL OR $6='never' AND s.config_succeeded_at IS NULL
  OR $6='ok' AND s.config_error IS NULL AND s.config_succeeded_at IS NOT NULL
  OR $6='overdue' AND s.config_succeeded_at < now()-interval '7 days')
 AND ($7='' OR $7='release' AND s.handles_release OR $7='other' AND NOT s.handles_release
  OR $7='none' AND s.handles_release AND NOT EXISTS (SELECT 1 FROM bp_networks n WHERE n.switch_id=s.id AND n.is_enabled AND n.is_validated))
 AND ($8='' OR $8='enabled' AND s.is_enabled OR $8='disabled' AND NOT s.is_enabled)`

func switchFilterArgs(q url.Values) []any {
	return []any{strings.TrimSpace(q.Get("q")), q.Get("group"), q.Get("vendor"), q.Get("site"), q.Get("reachability"), q.Get("backup"), q.Get("networks"), q.Get("state")}
}

func (o *OpsHandler) switches(w http.ResponseWriter, r *http.Request) {
	if _, ok := o.authorize(w, r, false); !ok {
		return
	}
	limit, offset := pageArgs(r)
	items, err := o.rows(r.Context(), []string{"id", "name", "management_ip", "vendor", "model", "site", "description", "group_id", "group_name",
		"protocol", "port", "is_enabled", "version", "credential_id", "credential_name", "command_profile_id", "profile_name",
		"reachable", "reachability_checked_at", "reachability_latency_ms", "reachability_error", "login_ok", "login_checked_at", "login_error",
		"config_checked_at", "config_succeeded_at", "config_error", "compliance_failed", "networks",
		"handles_release", "networks_checked_at", "networks_succeeded_at", "networks_error", "total"},
		`SELECT s.id::text,s.name,host(s.management_ip),s.vendor,s.model,s.site,s.description,s.group_id::text,g.name,
 s.protocol,s.port,s.is_enabled,s.version,s.credential_id::text,c.name,s.command_profile_id::text,p.name,
 s.reachable,s.reachability_checked_at,s.reachability_latency_ms,s.reachability_error,s.login_ok,s.login_checked_at,s.login_error,
 s.config_checked_at,s.config_succeeded_at,s.config_error,(SELECT count(*) FROM bp_compliance_results r WHERE r.switch_id=s.id AND NOT r.passed),
 (SELECT count(*) FROM bp_networks n WHERE n.switch_id=s.id AND n.is_enabled AND n.is_validated),
 s.handles_release,s.networks_checked_at,s.networks_succeeded_at,s.networks_error,count(*) OVER()
 FROM bp_switches s JOIN bp_credentials c ON c.id=s.credential_id JOIN bp_command_profiles p ON p.id=s.command_profile_id
 LEFT JOIN bp_switch_groups g ON g.id=s.group_id
 WHERE `+switchFilter+` ORDER BY s.name,s.id LIMIT $9 OFFSET $10`, append(switchFilterArgs(r.URL.Query()), limit, offset)...)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	total, items := paged(items)
	sites := []string{}
	rows, err := o.db.QueryContext(r.Context(), `SELECT DISTINCT site FROM bp_switches WHERE site IS NOT NULL ORDER BY site`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var site string
			if err = rows.Scan(&site); err != nil {
				break
			}
			sites = append(sites, site)
		}
		if err == nil {
			err = rows.Err()
		}
	}
	if err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total, "page_size": limit, "sites": sites})
}

var exportHeader = []string{"name", "management_ip", "vendor", "model", "group", "site", "protocol", "port", "credential", "profile", "description",
	"handles_release", "enabled", "reachable", "login", "last_backup"}

// spreadsheetSafe keeps a cell from being read as a formula; the importer drops the quote again.
func spreadsheetSafe(value string) string {
	if value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])) {
		return "'" + value
	}
	return value
}

// exportSwitches writes the filtered inventory as CSV. Its first twelve columns are the import
// template's, so an edited export imports back as updates; the status columns are only read.
func (o *OpsHandler) exportSwitches(w http.ResponseWriter, r *http.Request) {
	if _, ok := o.authorize(w, r, false); !ok {
		return
	}
	rows, err := o.db.QueryContext(r.Context(), `SELECT s.name,host(s.management_ip),s.vendor,COALESCE(s.model,''),COALESCE(g.name,''),COALESCE(s.site,''),
 s.protocol,COALESCE(s.port::text,''),c.name,p.name,COALESCE(s.description,''),CASE WHEN s.handles_release THEN 'yes' ELSE 'no' END,
 s.is_enabled,s.reachable,s.login_ok,s.config_succeeded_at
 FROM bp_switches s JOIN bp_credentials c ON c.id=s.credential_id JOIN bp_command_profiles p ON p.id=s.command_profile_id
 LEFT JOIN bp_switch_groups g ON g.id=s.group_id
 WHERE `+switchFilter+` ORDER BY s.name,s.id`, switchFilterArgs(r.URL.Query())...)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	defer rows.Close()
	var body bytes.Buffer
	body.WriteString("\ufeff") // spreadsheets read UTF-8 only with a BOM
	out := csv.NewWriter(&body)
	_ = out.Write(exportHeader)
	yesNo := func(value sql.NullBool, yes, no string) string {
		if !value.Valid {
			return ""
		}
		if value.Bool {
			return yes
		}
		return no
	}
	for rows.Next() {
		record := make([]string, 12)
		var enabled bool
		var reachable, login sql.NullBool
		var backup sql.NullTime
		targets := []any{}
		for i := range record {
			targets = append(targets, &record[i])
		}
		if err = rows.Scan(append(targets, &enabled, &reachable, &login, &backup)...); err != nil {
			o.unavailable(w, err)
			return
		}
		last := ""
		if backup.Valid {
			last = backup.Time.In(o.loc).Format("2006-01-02 15:04")
		}
		record = append(record, strconv.FormatBool(enabled), yesNo(reachable, "yes", "no"), yesNo(login, "ok", "failed"), last)
		for i := range record {
			record[i] = spreadsheetSafe(record[i])
		}
		_ = out.Write(record)
	}
	if err = rows.Err(); err != nil {
		o.unavailable(w, err)
		return
	}
	out.Flush()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="switches-`+time.Now().In(o.loc).Format("20060102")+`.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(body.Bytes())
}

func (o *OpsHandler) switchDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := o.authorize(w, r, false); !ok {
		return
	}
	id := r.PathValue("id")
	if !domain.IsCanonicalUUID(id) {
		fail(w, 404, ErrNotFound.Error())
		return
	}
	ctx := r.Context()
	found, err := o.rows(ctx, []string{"id", "name", "management_ip", "vendor", "model", "site", "description", "group_name", "protocol", "port", "is_enabled",
		"credential_name", "profile_name", "reachable", "reachability_checked_at", "reachability_changed_at", "reachability_latency_ms", "reachability_error",
		"login_ok", "login_checked_at", "login_error", "config_checked_at", "config_succeeded_at", "config_error",
		"handles_release", "networks_checked_at", "networks_succeeded_at", "networks_error"},
		`SELECT s.id::text,s.name,host(s.management_ip),s.vendor,s.model,s.site,s.description,g.name,s.protocol,s.port,s.is_enabled,c.name,p.name,
 s.reachable,s.reachability_checked_at,s.reachability_changed_at,s.reachability_latency_ms,s.reachability_error,
 s.login_ok,s.login_checked_at,s.login_error,s.config_checked_at,s.config_succeeded_at,s.config_error,
 s.handles_release,s.networks_checked_at,s.networks_succeeded_at,s.networks_error
 FROM bp_switches s JOIN bp_credentials c ON c.id=s.credential_id JOIN bp_command_profiles p ON p.id=s.command_profile_id
 LEFT JOIN bp_switch_groups g ON g.id=s.group_id WHERE s.id=$1`, id)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	if len(found) == 0 {
		fail(w, 404, ErrNotFound.Error())
		return
	}
	detail := found[0]
	sections := []struct {
		key, query string
		columns    []string
	}{
		{"backups", `SELECT id,captured_at,size_bytes,line_count,content_sha256 FROM bp_config_backups WHERE switch_id=$1 ORDER BY captured_at DESC,id DESC LIMIT 200`,
			[]string{"id", "captured_at", "size_bytes", "line_count", "sha256"}},
		{"compliance", `SELECT r.rule_id::text,ru.name,ru.severity,ru.mode,r.passed,r.detail,r.evaluated_at,r.backup_id FROM bp_compliance_results r
 JOIN bp_compliance_rules ru ON ru.id=r.rule_id WHERE r.switch_id=$1 ORDER BY r.passed,ru.name`,
			[]string{"rule_id", "rule_name", "severity", "mode", "passed", "detail", "evaluated_at", "backup_id"}},
		{"recent_items", `SELECT id,kind,status,message,changed,backup_id,created_at,finished_at FROM bp_device_task_items WHERE switch_id=$1 ORDER BY id DESC LIMIT 20`,
			[]string{"id", "kind", "status", "message", "changed", "backup_id", "created_at", "finished_at"}},
		{"alerts", `SELECT id,kind,severity,message,status,opened_at FROM bp_alerts WHERE switch_id=$1 AND status<>'resolved' ORDER BY opened_at DESC`,
			[]string{"id", "kind", "severity", "message", "status", "opened_at"}},
		{"networks", `SELECT id::text,switch_id::text,cidr::text,vlan,description,interface,is_enabled,is_validated,version FROM bp_networks WHERE switch_id=$1 ORDER BY cidr`,
			[]string{"id", "switch_id", "cidr", "vlan", "description", "interface", "is_enabled", "is_validated", "version"}},
		// Changes to the switch and its networks, including networks typed in before they were read
		// from configuration; a network moved away takes its history along.
		{"history", `SELECT e.id,e.action,e.actor_name,e.actor_email,e.object_name,e.changes::text,e.created_at FROM bp_asset_events e
 WHERE e.object_id=$1 AND e.action IN ('switch.create','switch.import','switch.update','switch.networks')
 OR e.action IN ('network.create','network.import','network.update') AND e.object_id IN (SELECT id FROM bp_networks WHERE switch_id=$1)
 ORDER BY e.id DESC LIMIT 50`,
			[]string{"id", "action", "actor_name", "actor_email", "object_name", "changes", "created_at"}},
	}
	for _, section := range sections {
		if detail[section.key], err = o.rows(ctx, section.columns, section.query, id); err != nil {
			o.unavailable(w, err)
			return
		}
	}
	write(w, 200, detail)
}

type sealedBackup struct {
	id                   int64
	switchID, switchName string
	capturedAt           time.Time
	content              string
}

func (o *OpsHandler) openBackup(ctx context.Context, raw string) (sealedBackup, error) {
	var b sealedBackup
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return b, ErrNotFound
	}
	var sealed string
	err = o.db.QueryRowContext(ctx, `SELECT b.id,b.switch_id::text,s.name,b.captured_at,b.encrypted_content FROM bp_config_backups b JOIN bp_switches s ON s.id=b.switch_id WHERE b.id=$1`, id).
		Scan(&b.id, &b.switchID, &b.switchName, &b.capturedAt, &sealed)
	if err == sql.ErrNoRows {
		return b, ErrNotFound
	}
	if err != nil {
		return b, err
	}
	b.content, err = openConfig(o.key, sealed)
	return b, err
}

// auditRead records who looked at sensitive configuration; the view fails closed without it.
func (o *OpsHandler) auditRead(ctx context.Context, actor domain.Principal, action, objectID, name string) error {
	_, err := o.db.ExecContext(ctx, `INSERT INTO bp_asset_events(actor_id,actor_email,actor_name,action,object_id,object_name) VALUES ($1,$2,$6,$3,$4,$5)`, actor.User.ID, actor.User.Email, action, objectID, name, actor.User.Name)
	return err
}

func (o *OpsHandler) backup(w http.ResponseWriter, r *http.Request) {
	actor, ok := o.authorize(w, r, false)
	if !ok {
		return
	}
	b, err := o.openBackup(r.Context(), r.PathValue("id"))
	if err == ErrNotFound {
		fail(w, 404, err.Error())
		return
	}
	if err == nil {
		err = o.auditRead(r.Context(), actor, "config.view", b.switchID, fmt.Sprintf("%s (version %d)", b.switchName, b.id))
	}
	if err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 200, map[string]any{"id": b.id, "switch_id": b.switchID, "switch_name": b.switchName, "captured_at": b.capturedAt, "content": b.content})
}

func configLines(content string) []string {
	return strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
}

func (o *OpsHandler) backupDiff(w http.ResponseWriter, r *http.Request) {
	actor, ok := o.authorize(w, r, false)
	if !ok {
		return
	}
	target, err := o.openBackup(r.Context(), r.PathValue("id"))
	var base sealedBackup
	if err == nil {
		base, err = o.openBackup(r.Context(), r.URL.Query().Get("base"))
	}
	if err == ErrNotFound || err == nil && base.switchID != target.switchID {
		fail(w, 404, "both versions must exist and belong to one switch")
		return
	}
	if err == nil {
		err = o.auditRead(r.Context(), actor, "config.diff", target.switchID, fmt.Sprintf("%s (version %d against %d)", target.switchName, target.id, base.id))
	}
	if err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 200, map[string]any{
		"switch_name": target.switchName,
		"base":        map[string]any{"id": base.id, "captured_at": base.capturedAt},
		"target":      map[string]any{"id": target.id, "captured_at": target.capturedAt},
		"diff":        DiffLines(configLines(base.content), configLines(target.content), 3),
	})
}

var taskSummaryColumns = []string{"id", "kind", "scope_label", "actor_name", "actor_email", "schedule_name", "created_at",
	"total", "queued", "running", "succeeded", "failed", "skipped", "changed", "finished_at"}

func (o *OpsHandler) taskSummaries(ctx context.Context, id string, limit int) ([]map[string]any, error) {
	return o.rows(ctx, taskSummaryColumns, `SELECT t.id::text,t.kind,t.scope_label,t.actor_name,t.actor_email,sc.name,t.created_at,
 count(i.id),count(i.id) FILTER (WHERE i.status='queued'),count(i.id) FILTER (WHERE i.status='running'),
 count(i.id) FILTER (WHERE i.status='succeeded'),count(i.id) FILTER (WHERE i.status='failed'),count(i.id) FILTER (WHERE i.status='skipped'),
 count(i.id) FILTER (WHERE i.changed),max(i.finished_at)
 FROM bp_device_tasks t LEFT JOIN bp_device_task_items i ON i.task_id=t.id LEFT JOIN bp_schedules sc ON sc.id=t.schedule_id
 WHERE $1='' OR t.id::text=$1 GROUP BY t.id,sc.name ORDER BY t.created_at DESC LIMIT $2`, id, limit)
}

func (o *OpsHandler) tasks(w http.ResponseWriter, r *http.Request) {
	if _, ok := o.authorize(w, r, false); !ok {
		return
	}
	items, err := o.taskSummaries(r.Context(), "", 50)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 200, items)
}

func (o *OpsHandler) task(w http.ResponseWriter, r *http.Request) {
	if _, ok := o.authorize(w, r, false); !ok {
		return
	}
	id := r.PathValue("id")
	if !domain.IsCanonicalUUID(id) {
		fail(w, 404, ErrNotFound.Error())
		return
	}
	summary, err := o.taskSummaries(r.Context(), id, 1)
	if err == nil && len(summary) == 0 {
		fail(w, 404, ErrNotFound.Error())
		return
	}
	var items []map[string]any
	if err == nil {
		items, err = o.rows(r.Context(), []string{"id", "switch_id", "switch_name", "management_ip", "status", "message", "changed", "backup_id", "started_at", "finished_at"},
			`SELECT i.id,i.switch_id::text,s.name,host(s.management_ip),i.status,i.message,i.changed,i.backup_id,i.started_at,i.finished_at
 FROM bp_device_task_items i JOIN bp_switches s ON s.id=i.switch_id WHERE i.task_id=$1
 ORDER BY CASE i.status WHEN 'failed' THEN 0 WHEN 'running' THEN 1 WHEN 'queued' THEN 2 ELSE 3 END,s.name LIMIT 5000`, id)
	}
	if err != nil {
		o.unavailable(w, err)
		return
	}
	detail := summary[0]
	detail["items"] = items
	write(w, 200, detail)
}

// deviceTaskKinds can be started by hand. A connection test signs in to every switch it covers,
// so schedules keep to the port check and backups (scheduleKinds). Network reads run by themselves
// every day (queueNetworkReads) and only on release switches.
var (
	deviceTaskKinds = map[string]bool{"config_backup": true, "reachability": true, "login_check": true, "network_read": true}
	scheduleKinds   = map[string]bool{"config_backup": true, "reachability": true}
)

// enqueueDeviceTask queues one item per enabled switch in scope. A switch that already has a
// pending operation of this kind is recorded as skipped instead of queued twice.
func enqueueDeviceTask(ctx context.Context, tx *sql.Tx, kind string, scheduleID, actorID, actorName, actorEmail any, scope, groupID string, switchIDs []string) (string, string, int64, int64, error) {
	label := "All enabled switches"
	filter := `TRUE`
	param := ""
	switch scope {
	case "group":
		var name string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM bp_switch_groups WHERE id=$1`, groupID).Scan(&name); err == sql.ErrNoRows {
			return "", "", 0, 0, ErrNotFound
		} else if err != nil {
			return "", "", 0, 0, err
		}
		label, filter, param = "Group "+name, `s.group_id=$3::uuid`, groupID
	case "switches":
		label, filter, param = fmt.Sprintf("%d selected switches", len(switchIDs)), `s.id=ANY(string_to_array($3,',')::uuid[])`, strings.Join(switchIDs, ",")
		if len(switchIDs) == 1 {
			var name string
			if err := tx.QueryRowContext(ctx, `SELECT name FROM bp_switches WHERE id=$1`, switchIDs[0]).Scan(&name); err == nil {
				label = "Switch " + name
			}
		}
	}
	var taskID string
	if err := tx.QueryRowContext(ctx, `INSERT INTO bp_device_tasks(kind,schedule_id,actor_id,actor_name,actor_email,scope_label) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id::text`,
		kind, scheduleID, actorID, actorName, actorEmail, label).Scan(&taskID); err != nil {
		return "", "", 0, 0, err
	}
	if param == "" {
		filter += ` AND $3=''`
	}
	if kind == "network_read" {
		filter += ` AND s.handles_release`
	}
	queued, err := tx.ExecContext(ctx, `INSERT INTO bp_device_task_items(task_id,kind,switch_id) SELECT $1,$2,s.id FROM bp_switches s WHERE s.is_enabled AND `+filter+`
 ON CONFLICT (switch_id,kind) WHERE status IN ('queued','running') DO NOTHING`, taskID, kind, param)
	if err != nil {
		return "", "", 0, 0, err
	}
	skipped, err := tx.ExecContext(ctx, `INSERT INTO bp_device_task_items(task_id,kind,switch_id,status,message,finished_at)
 SELECT $1,$2,s.id,'skipped','Already queued or running',now() FROM bp_switches s WHERE s.is_enabled AND `+filter+`
 AND NOT EXISTS (SELECT 1 FROM bp_device_task_items i WHERE i.task_id=$1 AND i.switch_id=s.id)`, taskID, kind, param)
	if err != nil {
		return "", "", 0, 0, err
	}
	nQueued, _ := queued.RowsAffected()
	nSkipped, _ := skipped.RowsAffected()
	return taskID, label, nQueued, nSkipped, nil
}

func (o *OpsHandler) runTask(w http.ResponseWriter, r *http.Request) {
	actor, ok := o.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Kind      string   `json:"kind"`
		Scope     string   `json:"scope"`
		GroupID   string   `json:"group_id"`
		SwitchIDs []string `json:"switch_ids"`
	}
	err := decodeLimit(r, &input, 1<<20)
	valid := err == nil && deviceTaskKinds[input.Kind] && (input.Scope == "all" ||
		input.Scope == "group" && domain.IsCanonicalUUID(input.GroupID) ||
		input.Scope == "switches" && len(input.SwitchIDs) > 0 && len(input.SwitchIDs) <= importMaxRows)
	for _, id := range input.SwitchIDs {
		valid = valid && domain.IsCanonicalUUID(id)
	}
	if !valid {
		fail(w, 422, "kind and a scope (all, group or switches) required")
		return
	}
	tx, err := o.db.BeginTx(r.Context(), nil)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	defer tx.Rollback()
	taskID, label, queued, skipped, err := enqueueDeviceTask(r.Context(), tx, input.Kind, nil, actor.User.ID, actor.User.Name, actor.User.Email, input.Scope, input.GroupID, input.SwitchIDs)
	if err == ErrNotFound {
		fail(w, 404, "group not found")
		return
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO bp_asset_events(actor_id,actor_email,actor_name,action,object_id,object_name,changes) VALUES ($1,$2,$7,'task.run',$3,$4,jsonb_build_object('kind',$5::text,'queued',$6::int))`,
			actor.User.ID, actor.User.Email, taskID, label, input.Kind, queued, actor.User.Name)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 202, map[string]any{"task_id": taskID, "queued": queued, "skipped": skipped})
}

func (o *OpsHandler) alerts(w http.ResponseWriter, r *http.Request) {
	if _, ok := o.authorize(w, r, false); !ok {
		return
	}
	status := r.URL.Query().Get("status")
	if status != "resolved" {
		status = "active"
	}
	items, err := o.rows(r.Context(), []string{"id", "kind", "severity", "switch_id", "switch_name", "management_ip", "rule_id", "rule_name", "message", "status",
		"opened_at", "last_seen_at", "acknowledged_at", "acknowledged_by", "resolved_at"},
		`SELECT a.id,a.kind,a.severity,a.switch_id::text,s.name,host(s.management_ip),a.rule_id::text,ru.name,a.message,a.status,
 a.opened_at,a.last_seen_at,a.acknowledged_at,a.acknowledged_by,a.resolved_at
 FROM bp_alerts a JOIN bp_switches s ON s.id=a.switch_id LEFT JOIN bp_compliance_rules ru ON ru.id=a.rule_id
 WHERE ($1='active' AND a.status<>'resolved') OR ($1='resolved' AND a.status='resolved')
 ORDER BY CASE a.severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END,a.opened_at DESC LIMIT 500`, status)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 200, items)
}

func (o *OpsHandler) acknowledge(w http.ResponseWriter, r *http.Request) {
	actor, ok := o.authorize(w, r, true)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, 404, ErrNotFound.Error())
		return
	}
	tx, err := o.db.BeginTx(r.Context(), nil)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	defer tx.Rollback()
	var switchID, kind string
	err = tx.QueryRowContext(r.Context(), `UPDATE bp_alerts SET status='acknowledged',acknowledged_at=now(),acknowledged_by=$2 WHERE id=$1 AND status='open' RETURNING switch_id::text,kind`, id, actor.User.Email).Scan(&switchID, &kind)
	if err == sql.ErrNoRows {
		fail(w, 409, "alert is not open")
		return
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO bp_asset_events(actor_id,actor_email,actor_name,action,object_id,object_name) VALUES ($1,$2,$5,'alert.acknowledge',$3,$4)`, actor.User.ID, actor.User.Email, switchID, fmt.Sprintf("alert %d (%s)", id, kind), actor.User.Name)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 200, map[string]any{"id": id, "status": "acknowledged"})
}
