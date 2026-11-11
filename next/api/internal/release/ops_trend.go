package release

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// trendDays is how far the home page's configuration trend reaches back; backup runs are kept
// for 30 days, so failures are known that far.
const trendDays = 30

// A configuration change is a stored version with an earlier one of the same switch: a switch's
// first backup is its baseline, not a change.
const isChange = `EXISTS (SELECT 1 FROM bp_config_backups p WHERE p.switch_id=b.switch_id AND (p.captured_at,p.id) < (b.captured_at,b.id))`

// pgTimeZone names loc for PostgreSQL. A zone name Go loaded from the time zone database passes
// through; a fixed zone made in Go, whose short name PostgreSQL may read as another zone ("CST" is
// US Central there), becomes the Etc/GMT zone of its offset (with the sign POSIX gives it).
func pgTimeZone(loc *time.Location) string {
	name := loc.String()
	_, offset := time.Now().In(loc).Zone()
	if loaded, err := time.LoadLocation(name); err == nil && name != "Local" {
		if _, same := time.Now().In(loaded).Zone(); same == offset {
			return name
		}
	}
	if offset%3600 == 0 {
		if offset == 0 {
			return "UTC"
		}
		return fmt.Sprintf("Etc/GMT%+d", -offset/3600)
	}
	return "UTC"
}

// changeTrend counts, for each of the last trendDays days in the operations time zone, the
// switches whose configuration changed and the switches whose backup failed.
func (o *OpsHandler) changeTrend(ctx context.Context) ([]map[string]any, error) {
	return o.rows(ctx, []string{"date", "changed", "backup_failed"},
		`WITH days AS (
  SELECT d::date AS day FROM generate_series((now() AT TIME ZONE $1)::date-($2::int-1),(now() AT TIME ZONE $1)::date,interval '1 day') AS d),
changes AS (
  SELECT (b.captured_at AT TIME ZONE $1)::date AS day,count(DISTINCT b.switch_id) AS n FROM bp_config_backups b
  WHERE b.captured_at >= now()-make_interval(days=>$2::int+1) AND `+isChange+` GROUP BY 1),
failures AS (
  SELECT (i.finished_at AT TIME ZONE $1)::date AS day,count(DISTINCT i.switch_id) AS n FROM bp_device_task_items i
  WHERE i.kind='config_backup' AND i.status='failed' AND i.finished_at >= now()-make_interval(days=>$2::int+1) GROUP BY 1)
SELECT to_char(d.day,'YYYY-MM-DD'),COALESCE(c.n,0),COALESCE(f.n,0) FROM days d
 LEFT JOIN changes c ON c.day=d.day LEFT JOIN failures f ON f.day=d.day ORDER BY d.day`, pgTimeZone(o.loc), trendDays)
}

// changes lists one day of the trend: the switches whose configuration changed, each with the
// version before it to compare against, and the switches whose backup failed with the last reason.
func (o *OpsHandler) changes(w http.ResponseWriter, r *http.Request) {
	if _, ok := o.authorize(w, r, false); !ok {
		return
	}
	day, err := time.Parse("2006-01-02", r.URL.Query().Get("date"))
	if err != nil {
		fail(w, 422, "date must be YYYY-MM-DD")
		return
	}
	ctx, zone, date := r.Context(), pgTimeZone(o.loc), day.Format("2006-01-02")
	// The day runs from one local midnight to the next, as a range the indexes can use.
	changed, err := o.rows(ctx, []string{"backup_id", "previous_id", "switch_id", "switch_name", "management_ip", "captured_at", "line_count"},
		`SELECT b.id,prev.id,s.id::text,s.name,host(s.management_ip),b.captured_at,b.line_count
 FROM bp_config_backups b JOIN bp_switches s ON s.id=b.switch_id
 CROSS JOIN LATERAL (SELECT p.id FROM bp_config_backups p WHERE p.switch_id=b.switch_id AND (p.captured_at,p.id) < (b.captured_at,b.id)
  ORDER BY p.captured_at DESC,p.id DESC LIMIT 1) prev
 WHERE b.captured_at >= ($2::date::timestamp AT TIME ZONE $1) AND b.captured_at < (($2::date+1)::timestamp AT TIME ZONE $1)
 ORDER BY b.captured_at DESC,b.id DESC LIMIT 500`, zone, date)
	var failed []map[string]any
	if err == nil {
		failed, err = o.rows(ctx, []string{"switch_id", "switch_name", "management_ip", "finished_at", "message"},
			`SELECT * FROM (SELECT DISTINCT ON (i.switch_id) s.id::text,s.name,host(s.management_ip),i.finished_at,i.message
 FROM bp_device_task_items i JOIN bp_switches s ON s.id=i.switch_id
 WHERE i.kind='config_backup' AND i.status='failed'
  AND i.finished_at >= ($2::date::timestamp AT TIME ZONE $1) AND i.finished_at < (($2::date+1)::timestamp AT TIME ZONE $1)
 ORDER BY i.switch_id,i.finished_at DESC) f ORDER BY 2 LIMIT 500`, zone, date)
	}
	if err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 200, map[string]any{"date": date, "changed": changed, "backup_failed": failed})
}
