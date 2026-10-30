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

// ErrAssetChanged means the switch a pre-query observed is no longer the switch a release would use.
var ErrAssetChanged = errors.New("switch configuration changed since the pre-query; query again")

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

// pinnedSwitchCurrent re-checks, inside the caller's transaction, that a job's pinned revision still applies.
func pinnedSwitchCurrent(ctx context.Context, q querier, ip, switchID, profileID string, switchVersion, profileVersion int, resolvedNetwork sql.NullString) error {
	var current bool
	err := q.QueryRowContext(ctx, `SELECT s.version=$3 AND p.version=$4 AND `+switchUsable+`
 FROM `+usableSwitch+` WHERE s.id=$1 AND p.id=$2`, switchID, profileID, switchVersion, profileVersion).Scan(&current)
	if err == sql.ErrNoRows || err == nil && !current {
		return ErrAssetChanged
	}
	if err != nil || !resolvedNetwork.Valid {
		// An admin's explicit switch selection is not re-resolved.
		return err
	}
	networks, err := activeNetworks(ctx, q)
	if err != nil {
		return err
	}
	selected, err := ResolveNetwork(ip, networks)
	if err == ErrNoSwitch || err == ErrAmbiguous || err == nil && selected.SwitchID != switchID {
		return ErrAssetChanged
	}
	return err
}

func (s *SQLStore) Prepare(ctx context.Context, input PrepareInput) (Preparation, error) {
	if !ValidReason(input.Reason) {
		return Preparation{}, ErrConflict
	}
	if _, err := ResolveNetwork(input.TargetIP, nil); err == ErrInvalidIP {
		return Preparation{}, err
	}
	networks, err := activeNetworks(ctx, s.DB)
	if err != nil {
		return Preparation{}, err
	}
	selected, err := ResolveNetwork(input.TargetIP, networks)
	if err == ErrAmbiguous {
		return Preparation{Status: "stopped_ambiguous_switch", TargetIP: input.TargetIP, Reason: input.Reason, Force: input.Force}, nil
	}
	if err == ErrNoSwitch && input.SelectedSwitchID != "" && input.Force && input.Admin {
		err = s.DB.QueryRowContext(ctx, `SELECT s.id::text FROM bp_switches s JOIN bp_credentials c ON c.id=s.credential_id JOIN bp_command_profiles p ON p.id=s.command_profile_id WHERE s.id=$1 AND s.is_enabled AND c.is_active AND p.is_active`, input.SelectedSwitchID).Scan(&selected.SwitchID)
		if err == sql.ErrNoRows {
			return Preparation{}, ErrNoSwitch
		}
		if err != nil {
			return Preparation{}, err
		}
		selected.ID = ""
	} else if err == ErrNoSwitch {
		return Preparation{Status: "stopped_no_switch", TargetIP: input.TargetIP, Reason: input.Reason, Force: input.Force}, nil
	} else if err != nil {
		return Preparation{}, err
	}
	if input.SelectedSwitchID != "" && selected.ID != "" {
		return Preparation{}, ErrConflict
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Preparation{}, err
	}
	defer tx.Rollback()
	var id, switchName, managementIP string
	err = tx.QueryRowContext(ctx, `INSERT INTO bp_release_jobs(target_ip,kind,reason,force,status,phase,actor_id,actor_email,result,resolved_network_id,
 switch_id,command_profile_id,switch_version,command_profile_version,switch_name,switch_management_ip)
 SELECT $1,'pre_release_query',$2,$3,'queued','queued',$4,$5,$6::jsonb,NULLIF($7,'')::uuid,`+pinnedColumns+`
 FROM `+usableSwitch+` WHERE `+switchUsable+` AND s.id=$8
 RETURNING id::text,switch_name,host(switch_management_ip)`, input.TargetIP, input.Reason, input.Force, input.ActorID, input.ActorEmail, selectionJSON(selected), selected.ID, selected.SwitchID).Scan(&id, &switchName, &managementIP)
	if err == sql.ErrNoRows {
		// The switch was disabled between resolution and insertion.
		return Preparation{}, ErrAssetChanged
	}
	if err != nil {
		return Preparation{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO bp_release_events(job_id,phase,status,message) VALUES ($1,'queued','queued','Pre-release query queued')`, id); err != nil {
		return Preparation{}, err
	}
	if err = tx.Commit(); err != nil {
		return Preparation{}, err
	}
	return Preparation{PreparationJobID: id, Status: "query_queued", TargetIP: input.TargetIP, ResolvedSwitch: map[string]string{"switch_id": selected.SwitchID, "network_id": selected.ID, "cidr": selected.CIDR, "name": switchName, "management_ip": managementIP}, Force: input.Force, Reason: input.Reason}, nil
}
func selectionJSON(n Network) string {
	b, _ := json.Marshal(map[string]string{"network_id": n.ID, "cidr": n.CIDR})
	return string(b)
}
func (s *SQLStore) Confirm(ctx context.Context, input ConfirmInput) (Job, error) {
	if !domain.IsCanonicalUUID(input.PreparationJobID) {
		return Job{}, ErrNotFound
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	var owner, ip, kind, reason, status, switchID, profileID, entry string
	var preparedForce bool
	var before []byte
	var switchVersion, profileVersion int
	var resolvedNetwork sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT actor_id::text,host(target_ip),kind,reason,status,force,switch_id::text,command_profile_id::text,before_state,switch_version,command_profile_version,resolved_network_id::text FROM bp_release_jobs WHERE id=$1 FOR UPDATE`, input.PreparationJobID).Scan(&owner, &ip, &kind, &reason, &status, &preparedForce, &switchID, &profileID, &before, &switchVersion, &profileVersion, &resolvedNetwork)
	if err == sql.ErrNoRows {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, err
	}
	if owner != input.ActorID {
		return Job{}, ErrNotFound
	}
	var state struct {
		EntryType string `json:"entry_type"`
	}
	if err = json.Unmarshal(before, &state); err != nil {
		return Job{}, err
	}
	entry = state.EntryType
	if kind != "pre_release_query" || !(status == "waiting_confirmation" || status == "cancelled" && entry == "missing") || !CanConfirmRelease(entry, input.Force, input.Admin) {
		return Job{}, ErrConflict
	}
	if !preparedForce && input.Force && entry != "missing" {
		return Job{}, ErrConflict
	}
	var existing, existingStatus, existingPhase string
	var existingForce bool
	err = tx.QueryRowContext(ctx, `SELECT id::text,status,phase,force FROM bp_release_jobs WHERE preparation_job_id=$1`, input.PreparationJobID).Scan(&existing, &existingStatus, &existingPhase, &existingForce)
	if err == nil {
		if existingForce != input.Force {
			return Job{}, ErrConflict
		}
		if err = tx.Commit(); err != nil {
			return Job{}, err
		}
		return Job{ID: existing, JobID: existing, TargetIP: ip, SwitchID: switchID, Kind: "release", Status: existingStatus, Phase: existingPhase, Reason: reason, Force: existingForce}, nil
	}
	if err != sql.ErrNoRows {
		return Job{}, err
	}
	var blocked bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bp_release_jobs WHERE target_ip=$1 AND kind='release' AND status IN ('running','needs_manual_confirmation'))`, ip).Scan(&blocked); err != nil {
		return Job{}, err
	}
	if blocked {
		return Job{}, ErrConflict
	}
	if err = pinnedSwitchCurrent(ctx, tx, ip, switchID, profileID, switchVersion, profileVersion, resolvedNetwork); err != nil {
		return Job{}, err
	}
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO bp_release_jobs(target_ip,kind,reason,force,status,phase,actor_id,actor_email,preparation_job_id,before_state,resolved_network_id,
 switch_id,command_profile_id,switch_version,command_profile_version,switch_name,switch_management_ip)
 SELECT target_ip,'release',reason,$1,'queued','queued',actor_id,actor_email,id,before_state,resolved_network_id,
 switch_id,command_profile_id,switch_version,command_profile_version,switch_name,switch_management_ip FROM bp_release_jobs WHERE id=$2 RETURNING id::text`, input.Force, input.PreparationJobID).Scan(&id)
	if err != nil {
		return Job{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO bp_release_events(job_id,phase,status,message) VALUES ($1,'queued','queued','Release confirmed and queued')`, id); err != nil {
		return Job{}, err
	}
	if err = tx.Commit(); err != nil {
		return Job{}, err
	}
	return Job{ID: id, JobID: id, TargetIP: ip, SwitchID: switchID, Kind: "release", Status: "queued", Phase: "queued", Reason: reason, Force: input.Force}, nil
}
func (s *SQLStore) Job(ctx context.Context, id, actor string, admin bool) (Job, error) {
	if !domain.IsCanonicalUUID(id) {
		return Job{}, ErrNotFound
	}
	var job Job
	var before, after, result []byte
	var rawBefore, rawRelease, rawAfter, errMessage, retryOf, preparationID, releaseID sql.NullString
	var created time.Time
	var started, finished sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT j.id::text,host(j.target_ip),j.switch_id::text,j.switch_name,host(j.switch_management_ip),j.command_profile_id::text,j.kind,j.status,j.phase,j.reason,j.force,j.before_state,j.after_state,j.result,j.raw_before_output,j.raw_release_output,j.raw_after_output,j.error_message,j.actor_email,j.retry_of_id::text,j.preparation_job_id::text,r.id::text,j.created_at,j.started_at,j.finished_at FROM bp_release_jobs j LEFT JOIN bp_release_jobs r ON r.preparation_job_id=j.id WHERE j.id=$1 AND ($2 OR j.actor_id=$3)`, id, admin, actor).Scan(&job.ID, &job.TargetIP, &job.SwitchID, &job.SwitchName, &job.SwitchManagementIP, &job.CommandProfileID, &job.Kind, &job.Status, &job.Phase, &job.Reason, &job.Force, &before, &after, &result, &rawBefore, &rawRelease, &rawAfter, &errMessage, &job.ActorEmail, &retryOf, &preparationID, &releaseID, &created, &started, &finished)
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
 j.actor_email,j.retry_of_id::text,j.preparation_job_id::text,r.id::text,j.created_at,j.finished_at,count(*) OVER()
 FROM bp_release_jobs j LEFT JOIN bp_release_jobs r ON r.preparation_job_id=j.id
 WHERE ($1 OR j.actor_id=$2) AND ($3='' OR host(j.target_ip) LIKE $3||'%' OR j.switch_name ILIKE '%'||$3||'%')
 AND ($4='' OR j.kind=$4) AND ($5='' OR j.status=$5)
 ORDER BY j.created_at DESC,j.id DESC LIMIT $6 OFFSET $7`, admin, actor, filter.Query, filter.Kind, filter.Status, filter.Limit, filter.Offset)
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
			&job.ActorEmail, &retryOf, &preparationID, &releaseID, &created, &finished, &page.Total); err != nil {
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
