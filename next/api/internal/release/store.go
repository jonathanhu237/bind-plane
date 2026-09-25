package release

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	"time"
)

type SQLStore struct{ DB *sql.DB }

func NewSQLStore(db *sql.DB) *SQLStore { return &SQLStore{DB: db} }
func (s *SQLStore) Prepare(ctx context.Context, input PrepareInput) (Preparation, error) {
	if !ValidReason(input.Reason) {
		return Preparation{}, ErrConflict
	}
	if _, err := ResolveNetwork(input.TargetIP, nil); err == ErrInvalidIP {
		return Preparation{}, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT n.id::text,s.id::text,n.cidr::text,n.is_enabled,n.is_validated,s.is_enabled
 FROM bp_networks n JOIN bp_switches s ON s.id=n.switch_id
 JOIN bp_credentials c ON c.id=s.credential_id JOIN bp_command_profiles p ON p.id=s.command_profile_id
 WHERE c.is_active AND p.is_active`)
	if err != nil {
		return Preparation{}, err
	}
	var networks []Network
	for rows.Next() {
		var n Network
		if err = rows.Scan(&n.ID, &n.SwitchID, &n.CIDR, &n.Enabled, &n.Validated, &n.SwitchEnabled); err != nil {
			break
		}
		networks = append(networks, n)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
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
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO bp_release_jobs(target_ip,kind,reason,force,status,phase,actor_id,actor_email,switch_id,command_profile_id,result)
 SELECT $1,'pre_release_query',$2,$3,'queued','queued',$4,$5,s.id,s.command_profile_id,$6::jsonb
 FROM bp_switches s WHERE s.id=$7 RETURNING id::text`, input.TargetIP, input.Reason, input.Force, input.ActorID, input.ActorEmail, selectionJSON(selected), selected.SwitchID).Scan(&id)
	if err != nil {
		return Preparation{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO bp_release_events(job_id,phase,status,message) VALUES ($1,'queued','queued','Pre-release query queued')`, id); err != nil {
		return Preparation{}, err
	}
	if err = tx.Commit(); err != nil {
		return Preparation{}, err
	}
	return Preparation{PreparationJobID: id, Status: "query_queued", TargetIP: input.TargetIP, ResolvedSwitch: map[string]string{"switch_id": selected.SwitchID, "network_id": selected.ID, "cidr": selected.CIDR}, Force: input.Force, Reason: input.Reason}, nil
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
	err = tx.QueryRowContext(ctx, `SELECT actor_id::text,target_ip::text,kind,reason,status,force,switch_id::text,command_profile_id::text,before_state FROM bp_release_jobs WHERE id=$1 FOR UPDATE`, input.PreparationJobID).Scan(&owner, &ip, &kind, &reason, &status, &preparedForce, &switchID, &profileID, &before)
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
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO bp_release_jobs(target_ip,kind,reason,force,status,phase,actor_id,actor_email,switch_id,command_profile_id,preparation_job_id,before_state)
 SELECT target_ip,'release',reason,$1,'queued','queued',actor_id,actor_email,switch_id,command_profile_id,id,before_state FROM bp_release_jobs WHERE id=$2 RETURNING id::text`, input.Force, input.PreparationJobID).Scan(&id)
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
	var rawBefore, rawRelease, rawAfter, errMessage, retryOf, preparationID sql.NullString
	var created time.Time
	var started, finished sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT id::text,target_ip::text,switch_id::text,kind,status,phase,reason,force,before_state,after_state,result,raw_before_output,raw_release_output,raw_after_output,error_message,actor_email,retry_of_id::text,preparation_job_id::text,created_at,started_at,finished_at FROM bp_release_jobs WHERE id=$1 AND ($2 OR actor_id=$3)`, id, admin, actor).Scan(&job.ID, &job.TargetIP, &job.SwitchID, &job.Kind, &job.Status, &job.Phase, &job.Reason, &job.Force, &before, &after, &result, &rawBefore, &rawRelease, &rawAfter, &errMessage, &job.ActorEmail, &retryOf, &preparationID, &created, &started, &finished)
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
func (s *SQLStore) Jobs(ctx context.Context, actor string, admin bool) ([]Job, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text,target_ip::text,switch_id::text,kind,status,phase,reason,force FROM bp_release_jobs WHERE ($1 OR actor_id=$2) ORDER BY created_at DESC LIMIT 100`, admin, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Job{}
	for rows.Next() {
		var job Job
		if err = rows.Scan(&job.ID, &job.TargetIP, &job.SwitchID, &job.Kind, &job.Status, &job.Phase, &job.Reason, &job.Force); err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	return result, rows.Err()
}

var _ Store = (*SQLStore)(nil)
