package release

import (
	"context"
	"database/sql"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// Retry queues a fresh release after a failed or timed-out one, or one whose outcome is unknown. The
// worker queries the binding again before sending anything, so a release that took effect is not
// repeated: releasing again is how an unknown outcome gets settled.
// The address is resolved again and the job pins the switch's current revision. A release forced, or
// sent to a switch picked by hand, before both were removed is released again as a normal one.
func (s *SQLStore) Retry(ctx context.Context, original string, actor Actor, admin bool) (Job, error) {
	if !domain.IsCanonicalUUID(original) {
		return Job{}, ErrNotFound
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	var kind, status, owner, ip string
	err = tx.QueryRowContext(ctx, `SELECT kind,status,actor_id::text,host(target_ip) FROM bp_release_jobs WHERE id=$1 FOR UPDATE`, original).Scan(&kind, &status, &owner, &ip)
	if err == sql.ErrNoRows {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, err
	}
	if !admin && owner != actor.ID {
		return Job{}, ErrNotFound
	}
	// Queries from the old two-step flow are never turned into releases.
	if kind != "release" || status != "failed" && status != "timeout" && status != "needs_manual_confirmation" {
		return Job{}, ErrConflict
	}
	networks, err := activeNetworks(ctx, tx)
	if err != nil {
		return Job{}, err
	}
	selected, err := ResolveNetwork(ip, networks)
	if err != nil {
		return Job{}, err
	}
	if err = addressFree(ctx, tx, ip); err != nil {
		return Job{}, err
	}
	var id, reason, switchName, managementIP string
	err = tx.QueryRowContext(ctx, `INSERT INTO bp_release_jobs(target_ip,kind,reason,ticket_id,status,phase,actor_id,actor_email,actor_name,retry_of_id,resolved_network_id,
 switch_id,command_profile_id,switch_version,command_profile_version,switch_name,switch_management_ip,source)
 SELECT j.target_ip,'release',j.reason,j.ticket_id,'queued','queued',$1,$2,$6,j.id,$4::uuid,`+pinnedColumns+`,$7
 FROM bp_release_jobs j, `+usableSwitch+` WHERE j.id=$3 AND s.id=$5 AND `+switchUsable+`
 RETURNING id::text,reason,switch_name,host(switch_management_ip)`, actor.ID, actor.Email, original, selected.ID, selected.SwitchID, actor.Name, actor.source()).Scan(&id, &reason, &switchName, &managementIP)
	if err == sql.ErrNoRows {
		return Job{}, ErrAssetChanged
	}
	if err != nil {
		return Job{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO bp_release_events(job_id,phase,status,message,payload) VALUES ($1,'queued','queued','Retry queued as a new release',jsonb_build_object('retry_of_id',$2::text))`, id, original); err != nil {
		return Job{}, err
	}
	if err = tx.Commit(); err != nil {
		return Job{}, err
	}
	return Job{ID: id, JobID: id, TargetIP: ip, SwitchID: selected.SwitchID, SwitchName: switchName, SwitchManagementIP: managementIP, Kind: "release", Source: actor.source(), Status: "queued", Phase: "queued", Reason: reason}, nil
}
