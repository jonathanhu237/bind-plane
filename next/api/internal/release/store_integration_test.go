package release

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestStartQueuesOneReleasePerAddress(t *testing.T) {
	dsn := os.Getenv("BIND_PLANE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated test database required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Isolate setup but use the public SQLStore API for every observation.
	var actor, credential, profile, switchID string
	if err = tx.QueryRowContext(ctx, `INSERT INTO auth_users(name,email,email_canonical,password_hash) VALUES ('Operator','operator@test.invalid','operator@test.invalid','test') RETURNING id::text`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRowContext(ctx, `INSERT INTO bp_credentials(name,username,encrypted_password) VALUES ('test','test','ciphertext') RETURNING id::text`).Scan(&credential); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRowContext(ctx, `INSERT INTO bp_command_profiles(name) VALUES ('test') RETURNING id::text`).Scan(&profile); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRowContext(ctx, `INSERT INTO bp_switches(name,management_ip,credential_id,command_profile_id) VALUES ('test','192.0.2.1',$1,$2) RETURNING id::text`, credential, profile).Scan(&switchID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO bp_networks(switch_id,cidr,is_validated) VALUES ($1,'10.2.0.0/16',true)`, switchID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	store := NewSQLStore(db)
	start := func(ip string) (Job, error) {
		return store.Start(ctx, StartInput{TargetIP: ip, Reason: "temporary_test", ActorID: actor, ActorEmail: "operator@test.invalid"})
	}
	started, err := start("10.2.1.7")
	if err != nil {
		t.Fatal(err)
	}
	if started.Kind != "release" || started.Status != "queued" || started.JobID == "" || started.SwitchName != "test" || started.SwitchManagementIP != "192.0.2.1" {
		t.Fatalf("%+v", started)
	}
	detail, err := store.Job(ctx, started.JobID, actor, false)
	if err != nil || detail.TargetIP != "10.2.1.7" || detail.CommandProfileID != profile || len(detail.Events) != 1 || detail.PreparationJobID != nil {
		t.Fatalf("release evidence: %+v %v", detail, err)
	}
	if _, err = store.Job(ctx, started.JobID, "00000000-0000-4000-8000-000000000000", false); err != ErrNotFound {
		t.Fatalf("foreign actor: %v", err)
	}
	if stopped, err := start("10.9.0.1"); err != nil || stopped.Status != "stopped_no_switch" || stopped.JobID != "" {
		t.Fatalf("address without a switch: %+v %v", stopped, err)
	}
	// One release of an address at a time: another start or a retry waits until it finishes.
	if _, err = start("10.2.1.7"); err != ErrConflict {
		t.Fatalf("second release while one is queued: %v", err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE bp_release_jobs SET status='failed',phase='finished' WHERE id=$1`, started.JobID); err != nil {
		t.Fatal(err)
	}
	retried, err := store.Retry(ctx, started.JobID, actor, "operator@test.invalid", false)
	if err != nil || retried.Kind != "release" || retried.ID == started.JobID {
		t.Fatalf("retry %+v %v", retried, err)
	}
	if _, err = store.Retry(ctx, started.JobID, actor, "operator@test.invalid", false); err != ErrConflict {
		t.Fatalf("second retry while the first is queued: %v", err)
	}
	// An uncertain release holds the address and is not retried itself.
	if _, err = db.ExecContext(ctx, `UPDATE bp_release_jobs SET status='needs_manual_confirmation',phase='finished' WHERE id=$1`, retried.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Retry(ctx, retried.ID, actor, "operator@test.invalid", false); err != ErrConflict {
		t.Fatalf("retried an uncertain release: %v", err)
	}
	if _, err = start("10.2.1.7"); err != ErrConflict {
		t.Fatalf("uncertain prior command allowed: %v", err)
	}
	// A query from the old two-step flow stays readable but never becomes a release.
	var legacy string
	if err = db.QueryRowContext(ctx, `INSERT INTO bp_release_jobs(target_ip,kind,reason,force,status,phase,actor_id,actor_email,
 switch_id,command_profile_id,switch_version,command_profile_version,switch_name,switch_management_ip)
 SELECT target_ip,'pre_release_query',reason,false,'failed','finished',actor_id,actor_email,
 switch_id,command_profile_id,switch_version,command_profile_version,switch_name,switch_management_ip FROM bp_release_jobs WHERE id=$1 RETURNING id::text`, started.JobID).Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Retry(ctx, legacy, actor, "operator@test.invalid", false); err != ErrConflict {
		t.Fatalf("retried a legacy query: %v", err)
	}
	history, err := store.Jobs(ctx, actor, false, JobFilter{Limit: 50})
	if err != nil || len(history.Items) != 3 || history.Total != 3 || history.PageSize != 50 {
		t.Fatalf("jobs %+v %v", history, err)
	}
	for _, job := range history.Items {
		if job.CreatedAt == nil || job.ActorEmail != "operator@test.invalid" || job.SwitchManagementIP != "192.0.2.1" {
			t.Fatalf("listed job lacks its context: %+v", job)
		}
		if job.ID == retried.ID && (job.RetryOfID == nil || *job.RetryOfID != started.JobID) {
			t.Fatalf("listed retry lost its original: %+v", job)
		}
	}
	for _, tc := range []struct {
		name   string
		filter JobFilter
		want   int
	}{
		{"releases only", JobFilter{Kind: "release", Limit: 50}, 2},
		{"uncertain", JobFilter{Status: "needs_manual_confirmation", Limit: 50}, 1},
		{"address prefix", JobFilter{Query: "10.2.1.", Limit: 50}, 3},
		{"switch name", JobFilter{Query: "TES", Limit: 50}, 3},
		{"no match", JobFilter{Query: "10.9.", Limit: 50}, 0},
	} {
		got, err := store.Jobs(ctx, actor, false, tc.filter)
		if err != nil || len(got.Items) != tc.want || tc.want > 0 && got.Total != int64(tc.want) {
			t.Fatalf("%s: %d of %d %v", tc.name, len(got.Items), got.Total, err)
		}
	}
	second, err := store.Jobs(ctx, actor, false, JobFilter{Limit: 2, Offset: 2})
	if err != nil || len(second.Items) != 1 || second.Total != 3 || second.Items[0].ID != history.Items[2].ID {
		t.Fatalf("second page %+v %v", second, err)
	}
}
