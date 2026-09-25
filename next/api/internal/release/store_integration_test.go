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
	jobs, err := store.Jobs(ctx, actor, false)
	if err != nil || len(jobs) != 4 {
		t.Fatalf("jobs %d %v", len(jobs), err)
	}
}
