package release

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// Device monitoring: the worker polls every monitored switch over SNMP at the configured interval.
// A switch is monitored while it is enabled and has an active SNMP credential.
const monitoredSwitch = `s.is_enabled AND EXISTS (SELECT 1 FROM bp_snmp_credentials c WHERE c.id=s.snmp_credential_id AND c.is_active)`

// monitoringAlertKinds are raised by polls, so a switch no longer polled has them resolved.
const monitoringAlertKinds = `('snmp_unreachable','cpu_high','memory_high','temperature_high','device_restarted')`

// monitoringColumns change how a switch is polled, so a change to one polls it at once.
var monitoringColumns = []string{"management_ip", "snmp_credential_id", "snmp_port", "is_enabled", "command_profile_id"}

var monitoringSettingsFields = assetKind{"bp_monitoring_settings", "monitoring.update", "name", []assetField{
	{column: "interval_minutes", read: "interval_minutes"}, {column: "cpu_threshold", read: "cpu_threshold"},
	{column: "memory_threshold", read: "memory_threshold"}, {column: "temperature_threshold", read: "temperature_threshold"},
	{column: "consecutive_polls", read: "consecutive_polls"}, {column: "alert_on_restart", read: "alert_on_restart"},
}}

// forgetUnmonitored drops the live state and resolves the monitoring alerts of every switch that is
// no longer polled. Its samples stay until they age out.
func forgetUnmonitored(ctx context.Context, tx *sql.Tx) error {
	for _, statement := range []string{
		`DELETE FROM bp_switch_monitoring m USING bp_switches s WHERE m.switch_id=s.id AND NOT (` + monitoredSwitch + `)`,
		`DELETE FROM bp_switch_interfaces i USING bp_switches s WHERE i.switch_id=s.id AND NOT (` + monitoredSwitch + `)`,
		`UPDATE bp_alerts a SET status='resolved',resolved_at=now() FROM bp_switches s
 WHERE a.switch_id=s.id AND a.status<>'resolved' AND a.kind IN ` + monitoringAlertKinds + ` AND NOT (` + monitoredSwitch + `)`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// pollSoon follows an edit of a switch: one now polled differently is polled at once. It reports
// whether the edit touched monitoring, so the caller can forget switches no longer monitored.
func pollSoon(ctx context.Context, tx *sql.Tx, id string, changes map[string]any) (bool, error) {
	for _, column := range monitoringColumns {
		if _, ok := changes[column]; ok {
			_, err := tx.ExecContext(ctx, `UPDATE bp_switch_monitoring SET next_poll_at=now(),failures=0 WHERE switch_id=$1`, id)
			return true, err
		}
	}
	return false, nil
}

// switchChanged is everything that follows an edit of one switch.
func switchChanged(ctx context.Context, tx *sql.Tx, id string, changes map[string]any) error {
	if err := releaseRoleChanged(ctx, tx, id, changes); err != nil {
		return err
	}
	touched, err := pollSoon(ctx, tx, id, changes)
	if err == nil && touched {
		err = forgetUnmonitored(ctx, tx)
	}
	return err
}

const monitoringHigh = `(m.cpu_percent>=st.cpu_threshold OR m.memory_percent>=st.memory_threshold OR m.temperature_c>=st.temperature_threshold)`

func (o *OpsHandler) monitoringSettings(ctx context.Context) (map[string]any, error) {
	found, err := o.rows(ctx, []string{"id", "interval_minutes", "cpu_threshold", "memory_threshold", "temperature_threshold", "consecutive_polls", "alert_on_restart", "version"},
		`SELECT id::text,interval_minutes,cpu_threshold,memory_threshold,temperature_threshold,consecutive_polls,alert_on_restart,version FROM bp_monitoring_settings`)
	if err != nil {
		return nil, err
	}
	if len(found) != 1 {
		return nil, errors.New("monitoring settings row missing")
	}
	return found[0], nil
}

// monitoringSummary counts the enabled switches by how their monitoring stands.
func (o *OpsHandler) monitoringSummary(ctx context.Context) (map[string]int64, error) {
	var monitored, responding, noResponse, waiting, high, restarted, unmonitored int64
	err := o.db.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE x.monitored), count(*) FILTER (WHERE x.monitored AND m.succeeded_at IS NOT NULL AND m.error IS NULL),
 count(*) FILTER (WHERE x.monitored AND m.error IS NOT NULL), count(*) FILTER (WHERE x.monitored AND m.succeeded_at IS NULL AND m.error IS NULL),
 count(*) FILTER (WHERE x.monitored AND `+monitoringHigh+`), count(*) FILTER (WHERE x.monitored AND m.restarted_at > now()-interval '24 hours'),
 count(*) FILTER (WHERE NOT x.monitored)
 FROM (SELECT s.id,`+monitoredSwitch+` AS monitored FROM bp_switches s WHERE s.is_enabled) x
 LEFT JOIN bp_switch_monitoring m ON m.switch_id=x.id CROSS JOIN bp_monitoring_settings st`).
		Scan(&monitored, &responding, &noResponse, &waiting, &high, &restarted, &unmonitored)
	return map[string]int64{"monitored": monitored, "responding": responding, "no_response": noResponse, "waiting": waiting,
		"high": high, "restarted": restarted, "unmonitored": unmonitored}, err
}

var monitoringOrders = map[string]string{
	"name":        "s.name,s.id",
	"cpu":         "m.cpu_percent DESC NULLS LAST,s.name,s.id",
	"memory":      "m.memory_percent DESC NULLS LAST,s.name,s.id",
	"temperature": "m.temperature_c DESC NULLS LAST,s.name,s.id",
	"uptime":      "m.uptime_seconds ASC NULLS LAST,s.name,s.id",
	"response":    "m.response_ms DESC NULLS LAST,s.name,s.id",
}

// monitoring lists the enabled switches with their latest poll. The state filter picks the
// monitored ones ("" for all of them, "no_response", "waiting", "high" or "restarted"), or the
// enabled switches that are not monitored ("unmonitored").
func (o *OpsHandler) monitoring(w http.ResponseWriter, r *http.Request) {
	if _, ok := o.authorize(w, r, false); !ok {
		return
	}
	ctx, q := r.Context(), r.URL.Query()
	limit, offset := pageArgs(r)
	order, ok := monitoringOrders[q.Get("sort")]
	if !ok {
		order = monitoringOrders["name"]
	}
	items, err := o.rows(ctx, []string{"id", "name", "management_ip", "vendor", "model", "group_name", "snmp_credential_name", "monitored",
		"polled_at", "succeeded_at", "error", "failures", "response_ms", "sys_name", "uptime_seconds", "restarted_at",
		"cpu_percent", "memory_percent", "temperature_c", "interfaces", "interfaces_up", "total"},
		`SELECT s.id::text,s.name,host(s.management_ip),s.vendor,s.model,g.name,c.name,c.is_active IS TRUE,
 m.polled_at,m.succeeded_at,m.error,COALESCE(m.failures,0),m.response_ms,m.sys_name,m.uptime_seconds,m.restarted_at,
 m.cpu_percent,m.memory_percent,m.temperature_c,
 (SELECT count(*) FROM bp_switch_interfaces i WHERE i.switch_id=s.id),(SELECT count(*) FROM bp_switch_interfaces i WHERE i.switch_id=s.id AND i.oper_up),
 count(*) OVER()
 FROM bp_switches s CROSS JOIN bp_monitoring_settings st LEFT JOIN bp_snmp_credentials c ON c.id=s.snmp_credential_id
 LEFT JOIN bp_switch_monitoring m ON m.switch_id=s.id LEFT JOIN bp_switch_groups g ON g.id=s.group_id
 WHERE s.is_enabled AND ($1='' OR s.name ILIKE '%'||$1||'%' OR host(s.management_ip) LIKE $1||'%' OR m.sys_name ILIKE '%'||$1||'%')
 AND ($2='' OR $2='none' AND s.group_id IS NULL OR s.group_id::text=$2)
 AND CASE WHEN $3='unmonitored' THEN c.is_active IS NOT TRUE ELSE c.is_active IS TRUE AND ($3=''
  OR $3='no_response' AND m.error IS NOT NULL OR $3='waiting' AND m.succeeded_at IS NULL AND m.error IS NULL
  OR $3='high' AND `+monitoringHigh+` OR $3='restarted' AND m.restarted_at > now()-interval '24 hours') END
 ORDER BY `+order+` LIMIT $4 OFFSET $5`,
		strings.TrimSpace(q.Get("q")), q.Get("group"), q.Get("state"), limit, offset)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	total, items := paged(items)
	summary, err := o.monitoringSummary(ctx)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	settings, err := o.monitoringSettings(ctx)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total, "page_size": limit, "summary": summary, "settings": settings})
}

// metricRanges bucket a switch's samples so every range draws a few hundred points.
var metricRanges = map[string]struct {
	window, bucket string
	seconds        int
}{
	"1d":  {"1 day", "5 minutes", 300},
	"7d":  {"7 days", "30 minutes", 1800},
	"30d": {"30 days", "2 hours", 7200},
}

func (o *OpsHandler) metrics(w http.ResponseWriter, r *http.Request) {
	if _, ok := o.authorize(w, r, false); !ok {
		return
	}
	id := r.PathValue("id")
	if !domain.IsCanonicalUUID(id) {
		fail(w, 404, ErrNotFound.Error())
		return
	}
	name := r.URL.Query().Get("range")
	choice, ok := metricRanges[name]
	if !ok {
		name, choice = "1d", metricRanges["1d"]
	}
	points, err := o.rows(r.Context(), []string{"t", "cpu", "cpu_max", "memory", "temperature", "temperature_max", "response_ms"},
		`SELECT date_bin($3::interval,sampled_at,timestamptz '2000-01-01'),round(avg(cpu_percent))::int,max(cpu_percent)::int,
 round(avg(memory_percent))::int,round(avg(temperature_c))::int,max(temperature_c)::int,round(avg(response_ms))::int
 FROM bp_switch_metrics WHERE switch_id=$1 AND sampled_at > now()-$2::interval GROUP BY 1 ORDER BY 1`, id, choice.window, choice.bucket)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 200, map[string]any{"range": name, "bucket_seconds": choice.seconds, "points": points})
}

// pollNow brings a monitored switch's next poll forward to now.
func (o *OpsHandler) pollNow(w http.ResponseWriter, r *http.Request) {
	if _, ok := o.authorize(w, r, true); !ok {
		return
	}
	id := r.PathValue("id")
	if !domain.IsCanonicalUUID(id) {
		fail(w, 404, ErrNotFound.Error())
		return
	}
	result, err := o.db.ExecContext(r.Context(), `INSERT INTO bp_switch_monitoring(switch_id,next_poll_at)
 SELECT s.id,now() FROM bp_switches s WHERE s.id=$1 AND `+monitoredSwitch+`
 ON CONFLICT (switch_id) DO UPDATE SET next_poll_at=now()`, id)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		fail(w, 409, "switch is not monitored")
		return
	}
	write(w, 202, map[string]bool{"queued": true})
}

// editMonitoringSettings takes the whole set of settings; a null threshold turns its alert off.
func (o *OpsHandler) editMonitoringSettings(w http.ResponseWriter, r *http.Request) {
	actor, ok := o.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Version              int  `json:"version"`
		IntervalMinutes      int  `json:"interval_minutes"`
		CPUThreshold         *int `json:"cpu_threshold"`
		MemoryThreshold      *int `json:"memory_threshold"`
		TemperatureThreshold *int `json:"temperature_threshold"`
		ConsecutivePolls     int  `json:"consecutive_polls"`
		AlertOnRestart       bool `json:"alert_on_restart"`
	}
	within := func(value *int, low, high int) bool { return value == nil || *value >= low && *value <= high }
	if err := decode(r, &input); err != nil || input.Version < 1 || input.IntervalMinutes < 1 || input.IntervalMinutes > 60 ||
		input.ConsecutivePolls < 1 || input.ConsecutivePolls > 10 || !within(input.CPUThreshold, 1, 100) ||
		!within(input.MemoryThreshold, 1, 100) || !within(input.TemperatureThreshold, 1, 150) {
		fail(w, 422, "invalid monitoring settings")
		return
	}
	nullable := func(value *int) any {
		if value == nil {
			return nil
		}
		return *value
	}
	next := map[string]any{"interval_minutes": input.IntervalMinutes, "cpu_threshold": nullable(input.CPUThreshold),
		"memory_threshold": nullable(input.MemoryThreshold), "temperature_threshold": nullable(input.TemperatureThreshold),
		"consecutive_polls": input.ConsecutivePolls, "alert_on_restart": input.AlertOnRestart}
	var id string
	if err := o.db.QueryRowContext(r.Context(), `SELECT id::text FROM bp_monitoring_settings`).Scan(&id); err != nil {
		o.unavailable(w, err)
		return
	}
	// A shorter interval applies at once rather than after the next poll.
	stored, err := o.updateAuditedThen(r.Context(), actor, monitoringSettingsFields, id, input.Version, next, func(ctx context.Context, tx *sql.Tx, changes map[string]any) error {
		if _, ok := changes["interval_minutes"]; !ok {
			return nil
		}
		_, err := tx.ExecContext(ctx, `UPDATE bp_switch_monitoring SET next_poll_at=LEAST(next_poll_at,now()+make_interval(mins=>$1))`, input.IntervalMinutes)
		return err
	})
	switch {
	case errors.Is(err, errStaleAsset):
		fail(w, 409, err.Error())
	case err != nil:
		o.unavailable(w, err)
	default:
		write(w, 200, map[string]any{"version": stored})
	}
}
