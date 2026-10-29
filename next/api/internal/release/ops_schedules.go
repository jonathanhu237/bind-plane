package release

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

var scheduleFields = assetKind{"bp_schedules", "schedule.update", "name", []assetField{
	{column: "name", read: "name"}, {column: "kind", read: "kind"}, {column: "cron", read: "cron"},
	{column: "scope", read: "scope"}, {column: "group_id", read: "group_id::text"}, {column: "is_enabled", read: "is_enabled"},
}}

var errCronNeverMatches = errors.New("cron expression never matches")

// nextRun evaluates cron in the configured operations time zone.
func (o *OpsHandler) nextRun(expression string, after time.Time) (time.Time, error) {
	c, err := ParseCron(expression)
	if err != nil {
		return time.Time{}, err
	}
	next := c.Next(after.In(o.loc))
	if next.IsZero() {
		return next, errCronNeverMatches
	}
	return next, nil
}

func normalizeCron(expression string) string { return strings.Join(strings.Fields(expression), " ") }

func (o *OpsHandler) schedules(w http.ResponseWriter, r *http.Request) {
	if _, ok := o.authorize(w, r, false); !ok {
		return
	}
	items, err := o.rows(r.Context(), []string{"id", "name", "kind", "cron", "scope", "group_id", "group_name", "is_enabled", "next_run_at", "last_run_at", "version"},
		`SELECT sc.id::text,sc.name,sc.kind,sc.cron,sc.scope,sc.group_id::text,g.name,sc.is_enabled,sc.next_run_at,sc.last_run_at,sc.version
 FROM bp_schedules sc LEFT JOIN bp_switch_groups g ON g.id=sc.group_id ORDER BY sc.name`)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 200, map[string]any{"schedules": items, "timezone": o.loc.String()})
}

func (o *OpsHandler) createSchedule(w http.ResponseWriter, r *http.Request) {
	actor, ok := o.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		Cron      string `json:"cron"`
		Scope     string `json:"scope"`
		GroupID   string `json:"group_id"`
		IsEnabled *bool  `json:"is_enabled"`
	}
	err := decode(r, &input)
	name, expression := strings.TrimSpace(input.Name), normalizeCron(input.Cron)
	if err != nil || name == "" || !deviceTaskKinds[input.Kind] || input.Scope != "all" && !(input.Scope == "group" && domain.IsCanonicalUUID(input.GroupID)) {
		fail(w, 422, "name, kind and scope (all, or group with a group) required")
		return
	}
	next, err := o.nextRun(expression, time.Now())
	if err != nil {
		fail(w, 422, err.Error())
		return
	}
	enabled := input.IsEnabled == nil || *input.IsEnabled
	var nextRun any
	if enabled {
		nextRun = next
	}
	if input.Scope == "all" {
		input.GroupID = ""
	}
	id, err := o.createAudited(r.Context(), actor, "schedule.create", name,
		`INSERT INTO bp_schedules(name,kind,cron,scope,group_id,is_enabled,next_run_at) VALUES ($1,$2,$3,$4,NULLIF($5,'')::uuid,$6,$7) RETURNING id::text`,
		name, input.Kind, expression, input.Scope, input.GroupID, enabled, nextRun)
	if err != nil {
		fail(w, 409, "schedule could not be saved (duplicate name or missing group)")
		return
	}
	write(w, 201, map[string]any{"id": id, "name": name, "next_run_at": nextRun})
}

func (o *OpsHandler) editSchedule(w http.ResponseWriter, r *http.Request) {
	actor, ok := o.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Version   int     `json:"version"`
		Name      *string `json:"name"`
		Kind      *string `json:"kind"`
		Cron      *string `json:"cron"`
		Scope     *string `json:"scope"`
		GroupID   *string `json:"group_id"`
		IsEnabled *bool   `json:"is_enabled"`
	}
	next := map[string]any{}
	if err := decode(r, &input); err != nil || !requiredText(next, "name", input.Name) || input.Kind != nil && !deviceTaskKinds[*input.Kind] ||
		input.Scope != nil && *input.Scope != "all" && *input.Scope != "group" ||
		input.Scope != nil && *input.Scope == "group" && (input.GroupID == nil || !domain.IsCanonicalUUID(*input.GroupID)) {
		fail(w, 422, "invalid schedule")
		return
	}
	if input.Kind != nil {
		next["kind"] = *input.Kind
	}
	if input.Cron != nil {
		expression := normalizeCron(*input.Cron)
		if _, err := o.nextRun(expression, time.Now()); err != nil {
			fail(w, 422, err.Error())
			return
		}
		next["cron"] = expression
	}
	if input.Scope != nil {
		next["scope"] = *input.Scope
		next["group_id"] = nil
		if *input.Scope == "group" {
			next["group_id"] = *input.GroupID
		}
	}
	optionalBool(next, "is_enabled", input.IsEnabled)
	id := r.PathValue("id")
	o.applyEditThen(w, r, actor, scheduleFields, input.Version, next, func(ctx context.Context, tx *sql.Tx) error {
		var expression string
		var enabled bool
		if err := tx.QueryRowContext(ctx, `SELECT cron,is_enabled FROM bp_schedules WHERE id=$1`, id).Scan(&expression, &enabled); err != nil {
			return err
		}
		var nextRun any
		if enabled {
			t, err := o.nextRun(expression, time.Now())
			if err != nil {
				return err
			}
			nextRun = t
		}
		_, err := tx.ExecContext(ctx, `UPDATE bp_schedules SET next_run_at=$2 WHERE id=$1`, id, nextRun)
		return err
	})
}

func (o *OpsHandler) runSchedule(w http.ResponseWriter, r *http.Request) {
	actor, ok := o.authorize(w, r, true)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !domain.IsCanonicalUUID(id) {
		fail(w, 404, ErrNotFound.Error())
		return
	}
	tx, err := o.db.BeginTx(r.Context(), nil)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	defer tx.Rollback()
	var name, kind, scope, groupID string
	err = tx.QueryRowContext(r.Context(), `SELECT name,kind,scope,COALESCE(group_id::text,'') FROM bp_schedules WHERE id=$1`, id).Scan(&name, &kind, &scope, &groupID)
	if err == sql.ErrNoRows {
		fail(w, 404, ErrNotFound.Error())
		return
	}
	var taskID string
	var queued, skipped int64
	if err == nil {
		taskID, _, queued, skipped, err = enqueueDeviceTask(r.Context(), tx, kind, id, actor.User.ID, actor.User.Email, scope, groupID, nil)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO bp_asset_events(actor_id,actor_email,action,object_id,object_name) VALUES ($1,$2,'schedule.run',$3,$4)`, actor.User.ID, actor.User.Email, id, name)
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

// RunScheduler enqueues due schedules. A missed run (the API was down) fires once and
// then resumes from the next future match, never as a burst of catch-up runs.
func (o *OpsHandler) RunScheduler(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	var lastPrune time.Time
	for {
		if err := o.runDueSchedules(ctx, time.Now()); err != nil && ctx.Err() == nil {
			log.Printf("device scheduler: %v", err)
		}
		if time.Since(lastPrune) >= time.Hour {
			if err := o.pruneHistory(ctx); err != nil && ctx.Err() == nil {
				log.Printf("device history cleanup: %v", err)
			}
			lastPrune = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (o *OpsHandler) runDueSchedules(ctx context.Context, now time.Time) error {
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id::text,kind,cron,scope,COALESCE(group_id::text,'') FROM bp_schedules
 WHERE is_enabled AND next_run_at<=$1 ORDER BY next_run_at FOR UPDATE SKIP LOCKED LIMIT 20`, now)
	if err != nil {
		return err
	}
	type due struct{ id, kind, cron, scope, group string }
	var dues []due
	for rows.Next() {
		var d due
		if err = rows.Scan(&d.id, &d.kind, &d.cron, &d.scope, &d.group); err != nil {
			rows.Close()
			return err
		}
		dues = append(dues, d)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, d := range dues {
		var nextRun any
		if next, err := o.nextRun(d.cron, now); err == nil {
			nextRun = next
		}
		if _, _, _, _, err = enqueueDeviceTask(ctx, tx, d.kind, d.id, nil, nil, d.scope, d.group, nil); err != nil && err != ErrNotFound {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bp_schedules SET last_run_at=$2,next_run_at=$3 WHERE id=$1`, d.id, now, nextRun); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// pruneHistory keeps task runs for 30 days and resolved alerts for 180; configuration
// versions and the audit trail are never pruned automatically.
func (o *OpsHandler) pruneHistory(ctx context.Context) error {
	if _, err := o.db.ExecContext(ctx, `DELETE FROM bp_device_tasks t WHERE t.created_at < now()-interval '30 days'
 AND NOT EXISTS (SELECT 1 FROM bp_device_task_items i WHERE i.task_id=t.id AND i.status IN ('queued','running'))`); err != nil {
		return err
	}
	_, err := o.db.ExecContext(ctx, `DELETE FROM bp_alerts WHERE status='resolved' AND resolved_at < now()-interval '180 days'`)
	return err
}
