package httpapi

import (
	"os"
	"path/filepath"
	"testing"

	postgresadapter "github.com/jonathanhu237/bind-plane/next/api/internal/auth/adapter/postgres"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
)

func TestUserLifecycleMigrationPreservesExistingRecordsIntegration(t *testing.T) {
	db, ctx := openHTTPIntegrationDatabase(t)
	migration := func(suffix string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "migrations", "000010_user_lifecycle."+suffix+".sql"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	// Begin at the previous real schema, not a test-specific schema approximation.
	if _, err := db.ExecContext(ctx, migration("down")); err != nil {
		t.Fatal(err)
	}
	const admin = "019535d9-3df7-79fb-b466-fa907fa17f90"
	const creator = "019535d9-3df7-79fb-b466-fa907fa17f91"
	const invitation = "019535d9-3df7-79fb-b466-fa907fa17f92"
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO auth_users(id,name,email,email_canonical,password_hash) VALUES($1,'Admin','admin@migration.test','admin@migration.test','hash'),($2,'Original Creator','creator@migration.test','creator@migration.test','hash')`, admin, creator)
	exec(`INSERT INTO auth_user_roles(user_id,role_id) SELECT $1::uuid,id FROM auth_roles WHERE system_key='super_admin'`, admin)
	exec(`INSERT INTO auth_user_invitations(id,name,email,email_canonical,selector,verifier_digest,locale,expires_at,created_by) VALUES($1,'Invite','invite@migration.test','invite@migration.test',decode(repeat('01',16),'hex'),decode(repeat('02',32),'hex'),'en',clock_timestamp()+interval '1 hour',$2)`, invitation, creator)
	exec(`INSERT INTO auth_operation_logs(actor_user_id,actor_name,actor_email,action,object_type,object_id,result) VALUES($1::uuid,'Original Creator','creator@migration.test','test.old','user',$1::text,'success')`, creator)
	exec(migration("up"))
	store := postgresadapter.NewStore(db)
	before, err := store.FindInvitation(ctx, invitation)
	if err != nil || before.CreatedByName != "Original Creator" || before.CreatedByEmail != "creator@migration.test" {
		t.Fatalf("creator backfill: %+v %v", before, err)
	}
	if err = store.DeleteUserWithRevision(ctx, admin, creator, 1); err != nil {
		t.Fatal(err)
	}
	logs, err := store.ListOperationLogs(ctx, application.OperationLogListOptions{ActorID: creator, Limit: 25})
	if err != nil || len(logs.Items) != 1 || !logs.Items[0].ActorDeleted || logs.Items[0].ActorID != creator {
		t.Fatalf("old actor retained: %+v %v", logs, err)
	}
	if _, err = db.ExecContext(ctx, migration("down")); err == nil {
		t.Fatal("lossy downgrade should refuse")
	}
	after, err := store.FindInvitation(ctx, invitation)
	if err != nil || !after.CreatorDeleted || after.CreatedBy != creator {
		t.Fatalf("downgrade changed retained invitation: %+v %v", after, err)
	}
}
