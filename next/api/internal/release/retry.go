package release

import (
	"context"
	"database/sql"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// Retry creates a fresh pre-query, never replays a possibly executed switch command.
// It runs against the switch's current revision; an auto-resolved target is resolved again.
func (s *SQLStore) Retry(ctx context.Context, original, actorID, actorEmail string, admin bool) (Job, error) {
	if !domain.IsCanonicalUUID(original) {
		return Job{}, ErrNotFound
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	var kind, status, owner, ip, switchID string
	var resolvedNetwork sql.NullString
	var forced bool
	err = tx.QueryRowContext(ctx, `SELECT kind,status,actor_id::text,force,host(target_ip),switch_id::text,resolved_network_id::text FROM bp_release_jobs WHERE id=$1 FOR UPDATE`, original).Scan(&kind, &status, &owner, &forced, &ip, &switchID, &resolvedNetwork)
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
	if resolvedNetwork.Valid {
		networks, err := activeNetworks(ctx, tx)
		if err != nil {
			return Job{}, err
		}
		selected, err := ResolveNetwork(ip, networks)
		if err != nil {
			return Job{}, err
		}
		switchID, resolvedNetwork.String = selected.SwitchID, selected.ID
	}
	var id, reason, switchName, managementIP string
	err = tx.QueryRowContext(ctx, `INSERT INTO bp_release_jobs(target_ip,kind,reason,ticket_id,force,status,phase,actor_id,actor_email,retry_of_id,resolved_network_id,
 switch_id,command_profile_id,switch_version,command_profile_version,switch_name,switch_management_ip)
 SELECT j.target_ip,'pre_release_query',j.reason,j.ticket_id,j.force,'queued','queued',$1,$2,j.id,$4::uuid,`+pinnedColumns+`
 FROM bp_release_jobs j, `+usableSwitch+` WHERE j.id=$3 AND s.id=$5 AND `+switchUsable+`
 RETURNING id::text,reason,switch_name,host(switch_management_ip)`, actorID, actorEmail, original, nullable(resolvedNetwork), switchID).Scan(&id, &reason, &switchName, &managementIP)
	if err == sql.ErrNoRows {
		return Job{}, ErrAssetChanged
	}
	if err != nil {
		return Job{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO bp_release_events(job_id,phase,status,message,payload) VALUES ($1,'queued','queued','Retry begins with a fresh pre-query',jsonb_build_object('retry_of_id',$2::text))`, id, original); err != nil {
		return Job{}, err
	}
	if err = tx.Commit(); err != nil {
		return Job{}, err
	}
	return Job{ID: id, JobID: id, TargetIP: ip, SwitchID: switchID, SwitchName: switchName, SwitchManagementIP: managementIP, Kind: "pre_release_query", Status: "queued", Phase: "queued", Reason: reason, Force: forced}, nil
}

func nullable(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}
