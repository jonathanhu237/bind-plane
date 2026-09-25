package release

import (
	"context"
	"database/sql"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// Retry creates a fresh pre-query, never replays a possibly executed switch command.
func (s *SQLStore) Retry(ctx context.Context, original, actorID, actorEmail string, admin bool) (Job, error) {
	if !domain.IsCanonicalUUID(original) {
		return Job{}, ErrNotFound
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	var kind, status, owner string
	var forced bool
	err = tx.QueryRowContext(ctx, `SELECT kind,status,actor_id::text,force FROM bp_release_jobs WHERE id=$1 FOR UPDATE`, original).Scan(&kind, &status, &owner, &forced)
	if err == sql.ErrNoRows {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, err
	}
	if !admin && owner != actorID {
		return Job{}, ErrNotFound
	}
	if forced && !admin {
		return Job{}, ErrConflict
	}
	if status != "failed" && status != "timeout" && status != "needs_manual_confirmation" {
		return Job{}, ErrConflict
	}
	var id, ip, reason, switchID string
	err = tx.QueryRowContext(ctx, `INSERT INTO bp_release_jobs(target_ip,kind,reason,ticket_id,force,status,phase,actor_id,actor_email,switch_id,command_profile_id,retry_of_id)
 SELECT target_ip,'pre_release_query',reason,ticket_id,force,'queued','queued',$1,$2,switch_id,command_profile_id,id
 FROM bp_release_jobs WHERE id=$3 RETURNING id::text,target_ip::text,reason,switch_id::text`, actorID, actorEmail, original).Scan(&id, &ip, &reason, &switchID)
	if err != nil {
		return Job{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO bp_release_events(job_id,phase,status,message,payload) VALUES ($1,'queued','queued','Retry begins with a fresh pre-query',jsonb_build_object('retry_of_id',$2::text))`, id, original); err != nil {
		return Job{}, err
	}
	if err = tx.Commit(); err != nil {
		return Job{}, err
	}
	return Job{ID: id, JobID: id, TargetIP: ip, SwitchID: switchID, Kind: "pre_release_query", Status: "queued", Phase: "queued", Reason: reason, Force: forced}, nil
}
