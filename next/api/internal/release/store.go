package release

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type SQLStore struct{ DB *sql.DB }

type querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// ErrAssetChanged means the resolved switch stopped being usable while its job was being queued.
var ErrAssetChanged = errors.New("switch configuration changed; try again")

// Jobs copy the switch revision (pinnedColumns) so the worker can refuse an edited asset.
const (
	pinnedColumns = `s.id,s.command_profile_id,s.version,p.version,s.name,s.management_ip`
	usableSwitch  = `bp_switches s JOIN bp_credentials c ON c.id=s.credential_id JOIN bp_command_profiles p ON p.id=s.command_profile_id`
	switchUsable  = `s.is_enabled AND c.is_active AND p.is_active`
)

func NewSQLStore(db *sql.DB) *SQLStore { return &SQLStore{DB: db} }

func activeNetworks(ctx context.Context, q querier) ([]Network, error) {
	rows, err := q.QueryContext(ctx, `SELECT n.id::text,s.id::text,n.cidr::text,n.is_enabled,n.is_validated,s.is_enabled
 FROM bp_networks n JOIN bp_switches s ON s.id=n.switch_id
 JOIN bp_credentials c ON c.id=s.credential_id JOIN bp_command_profiles p ON p.id=s.command_profile_id
 WHERE c.is_active AND p.is_active`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var networks []Network
	for rows.Next() {
		var n Network
		if err = rows.Scan(&n.ID, &n.SwitchID, &n.CIDR, &n.Enabled, &n.Validated, &n.SwitchEnabled); err != nil {
			return nil, err
		}
		networks = append(networks, n)
	}
	return networks, rows.Err()
}

// Start queues a release job. The worker queries the binding first and changes nothing unless it is
// static. When no switch, or several equally specific ones,
// own the address, it returns that stop as a status and queues nothing.
func (s *SQLStore) Start(ctx context.Context, input StartInput) (Job, error) {
	if !ValidReason(input.Reason) {
		return Job{}, ErrConflict
	}
	if _, err := ResolveNetwork(input.TargetIP, nil); err == ErrInvalidIP {
		return Job{}, err
	}
	stopped := func(status string) (Job, error) {
		return Job{Status: status, TargetIP: input.TargetIP, Reason: input.Reason}, nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	networks, err := activeNetworks(ctx, tx)
	if err != nil {
		return Job{}, err
	}
	selected, err := ResolveNetwork(input.TargetIP, networks)
	if err == ErrAmbiguous {
		return stopped("stopped_ambiguous_switch")
	}
	if err == ErrNoSwitch {
		return stopped("stopped_no_switch")
	}
	if err != nil {
		return Job{}, err
	}
	if err = addressFree(ctx, tx, input.TargetIP); err != nil {
		return Job{}, err
	}
	var id, switchName, managementIP string
	err = tx.QueryRowContext(ctx, `INSERT INTO bp_release_jobs(target_ip,kind,reason,status,phase,actor_id,actor_email,actor_name,result,resolved_network_id,
 switch_id,command_profile_id,switch_version,command_profile_version,switch_name,switch_management_ip)
 SELECT $1,'release',$2,'queued','queued',$3,$4,$8,$5::jsonb,NULLIF($6,'')::uuid,`+pinnedColumns+`
 FROM `+usableSwitch+` WHERE `+switchUsable+` AND s.id=$7
 RETURNING id::text,switch_name,host(switch_management_ip)`, input.TargetIP, input.Reason, input.Actor.ID, input.Actor.Email, selectionJSON(selected), selected.ID, selected.SwitchID, input.Actor.Name).Scan(&id, &switchName, &managementIP)
	if err == sql.ErrNoRows {
		// The switch was disabled between resolution and insertion.
		return Job{}, ErrAssetChanged
	}
	if err != nil {
		return Job{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO bp_release_events(job_id,phase,status,message) VALUES ($1,'queued','queued','Release queued')`, id); err != nil {
		return Job{}, err
	}
	if err = tx.Commit(); err != nil {
		return Job{}, err
	}
	return Job{ID: id, JobID: id, TargetIP: input.TargetIP, SwitchID: selected.SwitchID, SwitchName: switchName, SwitchManagementIP: managementIP, Kind: "release", Status: "queued", Phase: "queued", Reason: input.Reason}, nil
}
func selectionJSON(n Network) string {
	b, _ := json.Marshal(map[string]string{"network_id": n.ID, "cidr": n.CIDR})
	return string(b)
}

// addressFree takes the address lock the worker also holds while it claims a release, then refuses
// another release while one is queued or running. An earlier release with an unknown outcome does not
// hold the address: the next one queries the switch before it sends anything.
func addressFree(ctx context.Context, tx *sql.Tx, ip string) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(host($1::inet),0))`, ip); err != nil {
		return err
	}
	var busy bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bp_release_jobs WHERE target_ip=$1::inet AND kind='release' AND status IN ('queued','running'))`, ip).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return ErrConflict
	}
	return nil
}
func (s *SQLStore) Job(ctx context.Context, id, actor string, admin bool) (Job, error) {
	if !domain.IsCanonicalUUID(id) {
		return Job{}, ErrNotFound
	}
	var job Job
	var before, after, result []byte
	var rawBefore, rawRelease, rawAfter, errMessage, retryOf, preparationID, releaseID, nextRelease sql.NullString
	var created time.Time
	var started, finished sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT j.id::text,host(j.target_ip),j.switch_id::text,j.switch_name,host(j.switch_management_ip),j.command_profile_id::text,j.kind,j.status,j.phase,j.reason,j.force,j.before_state,j.after_state,j.result,j.raw_before_output,j.raw_release_output,j.raw_after_output,j.error_message,COALESCE(j.actor_name,''),j.actor_email,j.retry_of_id::text,j.preparation_job_id::text,r.id::text,j.created_at,j.started_at,j.finished_at,
 (SELECT n.id::text FROM bp_release_jobs n WHERE n.target_ip=j.target_ip AND n.kind='release' AND n.created_at>j.created_at AND ($2 OR n.actor_id=$3) ORDER BY n.created_at LIMIT 1)
 FROM bp_release_jobs j LEFT JOIN bp_release_jobs r ON r.preparation_job_id=j.id WHERE j.id=$1 AND ($2 OR j.actor_id=$3)`, id, admin, actor).Scan(&job.ID, &job.TargetIP, &job.SwitchID, &job.SwitchName, &job.SwitchManagementIP, &job.CommandProfileID, &job.Kind, &job.Status, &job.Phase, &job.Reason, &job.Force, &before, &after, &result, &rawBefore, &rawRelease, &rawAfter, &errMessage, &job.ActorName, &job.ActorEmail, &retryOf, &preparationID, &releaseID, &created, &started, &finished, &nextRelease)
	if err == sql.ErrNoRows {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, err
	}
	job.CreatedAt = &created
	if started.Valid {
		job.StartedAt = &started.Time
	}
	if finished.Valid {
		job.FinishedAt = &finished.Time
	}
	if retryOf.Valid {
		job.RetryOfID = &retryOf.String
	}
	if preparationID.Valid {
		job.PreparationJobID = &preparationID.String
	}
	if releaseID.Valid {
		job.ReleaseJobID = &releaseID.String
	}
	if nextRelease.Valid {
		job.NextReleaseID = &nextRelease.String
	}
	job.BeforeState = json.RawMessage(before)
	job.AfterState = json.RawMessage(after)
	job.Result = json.RawMessage(result)
	if rawBefore.Valid {
		job.RawBeforeOutput = &rawBefore.String
	}
	if rawRelease.Valid {
		job.RawReleaseOutput = &rawRelease.String
	}
	if rawAfter.Valid {
		job.RawAfterOutput = &rawAfter.String
	}
	if errMessage.Valid {
		job.ErrorMessage = &errMessage.String
	}
	events, err := s.DB.QueryContext(ctx, `SELECT phase,status,message,created_at FROM bp_release_events WHERE job_id=$1 ORDER BY id LIMIT 1000`, id)
	if err != nil {
		return Job{}, err
	}
	defer events.Close()
	job.Events = []JobEvent{}
	for events.Next() {
		var event JobEvent
		var message sql.NullString
		if err = events.Scan(&event.Phase, &event.Status, &message, &event.CreatedAt); err != nil {
			return Job{}, err
		}
		if message.Valid {
			event.Message = &message.String
		}
		job.Events = append(job.Events, event)
	}
	if err = events.Err(); err != nil {
		return Job{}, err
	}
	return job, nil
}

// Jobs pages through the history newest first; an operator sees only their own jobs.
// The search text matches an address prefix or part of a switch name.
func (s *SQLStore) Jobs(ctx context.Context, actor string, admin bool, filter JobFilter) (JobPage, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT j.id::text,host(j.target_ip),j.switch_id::text,j.switch_name,host(j.switch_management_ip),j.kind,j.status,j.phase,j.reason,j.force,
 COALESCE(j.actor_name,''),j.actor_email,j.retry_of_id::text,j.preparation_job_id::text,r.id::text,j.created_at,j.finished_at,count(*) OVER()
 FROM bp_release_jobs j LEFT JOIN bp_release_jobs r ON r.preparation_job_id=j.id
 WHERE ($1 OR j.actor_id=$2) AND ($3='' OR host(j.target_ip) LIKE $3||'%' OR j.switch_name ILIKE '%'||$3||'%')
 AND ($4='' OR j.kind=$4) AND ($5='' OR j.status=$5)
 AND (NOT $8 OR NOT EXISTS (SELECT 1 FROM bp_release_jobs n WHERE n.target_ip=j.target_ip AND n.kind='release' AND n.created_at>j.created_at))
 ORDER BY j.created_at DESC,j.id DESC LIMIT $6 OFFSET $7`, admin, actor, filter.Query, filter.Kind, filter.Status, filter.Limit, filter.Offset, filter.Latest)
	if err != nil {
		return JobPage{}, err
	}
	defer rows.Close()
	page := JobPage{Items: []Job{}, PageSize: filter.Limit}
	for rows.Next() {
		var job Job
		var retryOf, preparationID, releaseID sql.NullString
		var created time.Time
		var finished sql.NullTime
		if err = rows.Scan(&job.ID, &job.TargetIP, &job.SwitchID, &job.SwitchName, &job.SwitchManagementIP, &job.Kind, &job.Status, &job.Phase, &job.Reason, &job.Force,
			&job.ActorName, &job.ActorEmail, &retryOf, &preparationID, &releaseID, &created, &finished, &page.Total); err != nil {
			return JobPage{}, err
		}
		job.CreatedAt = &created
		if finished.Valid {
			job.FinishedAt = &finished.Time
		}
		if retryOf.Valid {
			job.RetryOfID = &retryOf.String
		}
		if preparationID.Valid {
			job.PreparationJobID = &preparationID.String
		}
		if releaseID.Valid {
			job.ReleaseJobID = &releaseID.String
		}
		page.Items = append(page.Items, job)
	}
	return page, rows.Err()
}

var _ Store = (*SQLStore)(nil)
