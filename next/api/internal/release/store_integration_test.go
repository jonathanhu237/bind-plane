package release

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPrepareAndConfirmPersistSeparateJobs(t *testing.T) {
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
	// Isolate setup but use the public SQLStore API for both observations.
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
	prepared, err := store.Prepare(ctx, PrepareInput{TargetIP: "10.2.1.7", Reason: "temporary_test", ActorID: actor, ActorEmail: "operator@test.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Status != "query_queued" || prepared.PreparationJobID == "" {
		t.Fatalf("%+v", prepared)
	}
	// No switch connection: simulate the worker's durable pre-query checkpoint.
	if _, err = db.ExecContext(ctx, `UPDATE bp_release_jobs SET status='waiting_confirmation',phase='waiting_confirmation',before_state='{"entry_type":"static","mac":"aa:bb"}' WHERE id=$1`, prepared.PreparationJobID); err != nil {
		t.Fatal(err)
	}
	released, err := store.Confirm(ctx, ConfirmInput{PreparationJobID: prepared.PreparationJobID, ActorID: actor})
	if err != nil {
		t.Fatal(err)
	}
	if released.Status != "queued" || released.ID == prepared.PreparationJobID {
		t.Fatalf("%+v", released)
	}
	detail, err := store.Job(ctx, released.ID, actor, false)
	if err != nil || detail.TargetIP != "10.2.1.7" || detail.CommandProfileID != profile || len(detail.Events) != 1 || detail.PreparationJobID == nil || *detail.PreparationJobID != prepared.PreparationJobID {
		t.Fatalf("release evidence: %+v %v", detail, err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE bp_release_jobs SET status='succeeded',phase='finished' WHERE id=$1`, released.ID); err != nil {
		t.Fatal(err)
	}
	again, err := store.Confirm(ctx, ConfirmInput{PreparationJobID: prepared.PreparationJobID, ActorID: actor})
	if err != nil || again.ID != released.ID || again.Status != "succeeded" {
		t.Fatalf("idempotency %+v %v", again, err)
	}
	if _, err = store.Confirm(ctx, ConfirmInput{PreparationJobID: prepared.PreparationJobID, ActorID: "another"}); err != ErrNotFound {
		t.Fatalf("foreign actor: %v", err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE bp_release_jobs SET status='failed',phase='finished' WHERE id=$1`, released.ID); err != nil {
		t.Fatal(err)
	}
	retried, err := store.Retry(ctx, released.ID, actor, "operator@test.invalid", false)
	if err != nil || retried.Kind != "pre_release_query" || retried.ID == released.ID {
		t.Fatalf("retry %+v %v", retried, err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE bp_release_jobs SET status='needs_manual_confirmation',phase='finished' WHERE id=$1`, retried.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Retry(ctx, retried.ID, actor, "operator@test.invalid", false); err != nil {
		t.Fatalf("retry of retry: %v", err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE bp_release_jobs SET status='needs_manual_confirmation' WHERE id=$1`, released.ID); err != nil {
		t.Fatal(err)
	}
	blocked, err := store.Prepare(ctx, PrepareInput{TargetIP: "10.2.1.7", ActorID: actor, ActorEmail: "operator@test.invalid", Reason: "temporary_test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE bp_release_jobs SET status='waiting_confirmation',phase='waiting_confirmation',before_state='{"entry_type":"static"}' WHERE id=$1`, blocked.PreparationJobID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Confirm(ctx, ConfirmInput{PreparationJobID: blocked.PreparationJobID, ActorID: actor}); err != ErrConflict {
		t.Fatalf("uncertain prior command allowed: %v", err)
	}
	history, err := store.Jobs(ctx, actor, false, JobFilter{Limit: 50})
	if err != nil || len(history.Items) != 5 || history.Total != 5 || history.PageSize != 50 {
		t.Fatalf("jobs %+v %v", history, err)
	}
	// The history pairs a release with the query it confirmed, in both directions.
	for _, job := range history.Items {
		if job.ID == released.ID && (job.PreparationJobID == nil || *job.PreparationJobID != prepared.PreparationJobID) {
			t.Fatalf("listed release lost its query: %+v", job)
		}
		if job.ID == prepared.PreparationJobID && (job.PreparationJobID != nil || job.ReleaseJobID == nil || *job.ReleaseJobID != released.ID) {
			t.Fatalf("listed query lost its release: %+v", job)
		}
		if job.CreatedAt == nil || job.ActorEmail != "operator@test.invalid" || job.SwitchManagementIP != "192.0.2.1" {
			t.Fatalf("listed job lacks its context: %+v", job)
		}
	}
	for _, tc := range []struct {
		name   string
		filter JobFilter
		want   int
	}{
		{"releases only", JobFilter{Kind: "release", Limit: 50}, 1},
		{"uncertain", JobFilter{Status: "needs_manual_confirmation", Limit: 50}, 2},
		{"address prefix", JobFilter{Query: "10.2.1.", Limit: 50}, 5},
		{"switch name", JobFilter{Query: "TES", Limit: 50}, 5},
		{"no match", JobFilter{Query: "10.9.", Limit: 50}, 0},
	} {
		got, err := store.Jobs(ctx, actor, false, tc.filter)
		if err != nil || len(got.Items) != tc.want || tc.want > 0 && got.Total != int64(tc.want) {
			t.Fatalf("%s: %d of %d %v", tc.name, len(got.Items), got.Total, err)
		}
	}
	second, err := store.Jobs(ctx, actor, false, JobFilter{Limit: 2, Offset: 2})
	if err != nil || len(second.Items) != 2 || second.Total != 5 || second.Items[0].ID != history.Items[2].ID {
		t.Fatalf("second page %+v %v", second, err)
	}
	query, err := store.Job(ctx, prepared.PreparationJobID, actor, false)
	if err != nil || query.ReleaseJobID == nil || *query.ReleaseJobID != released.ID {
		t.Fatalf("query detail lost its release: %+v %v", query, err)
	}
}
