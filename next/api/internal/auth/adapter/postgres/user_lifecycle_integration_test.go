package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

func TestUserLifecycleIntegration(t *testing.T) {
	dsn := integrationDSN(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if cleanupErr := resetAuthState(cleanupCtx, db); cleanupErr != nil {
			t.Errorf("reset user lifecycle state: %v", cleanupErr)
		}
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close user lifecycle database: %v", closeErr)
		}
	})
	if err = resetAuthState(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := newTestStore(db)
	const a = "019535d9-3df7-79fb-b466-fa907fa17f91"
	const b = "019535d9-3df7-79fb-b466-fa907fa17f92"
	const writer = "019535d9-3df7-79fb-b466-fa907fa17f93"
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{a, b, writer} {
		exec(`INSERT INTO auth_users(id,name,email,email_canonical,password_hash) VALUES($1,'Lifecycle',$2,$2,'hash')`, id, id+"@example.com")
	}
	exec(`INSERT INTO auth_user_roles(user_id,role_id) SELECT u.id,r.id FROM auth_users u CROSS JOIN auth_roles r WHERE u.id IN ($1::uuid,$2::uuid) AND r.system_key='super_admin'`, a, b)
	var roleID string
	if err = db.QueryRowContext(ctx, `INSERT INTO auth_roles(name,name_canonical,description) VALUES('Lifecycle writer','lifecycle writer','') RETURNING id::text`).Scan(&roleID); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO auth_role_permissions(role_id,permission_key) VALUES($1,'users.write')`, roleID)
	exec(`INSERT INTO auth_user_roles(user_id,role_id) VALUES($1,$2)`, writer, roleID)
	if _, err = s.DeactivateUserWithRevision(ctx, a, a, 1); !errors.Is(err, application.ErrSelfUserOperation) {
		t.Fatalf("self: %v", err)
	}
	if _, err = s.DeactivateUserWithRevision(ctx, writer, a, 0); !errors.Is(err, application.ErrStaleRevision) {
		t.Fatalf("stale: %v", err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range []string{a, b} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			_, err := s.DeactivateUserWithRevision(ctx, writer, id, 1)
			results <- err
		}(id)
	}
	close(start)
	wg.Wait()
	close(results)
	success, blocked := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, application.ErrLastSuperAdmin) {
			blocked++
		} else {
			t.Fatalf("concurrent: %v", err)
		}
	}
	if success != 1 || blocked != 1 {
		t.Fatalf("concurrent success=%d blocked=%d", success, blocked)
	}
	var disabled, active string
	if err = db.QueryRowContext(ctx, `SELECT id::text FROM auth_users WHERE disabled_at IS NOT NULL`).Scan(&disabled); err != nil {
		t.Fatal(err)
	}
	if disabled == a {
		active = b
	} else {
		active = a
	}
	// Record an action before deletion, then another from an in-flight request afterward.
	audit := application.OperationLogInput{ActorID: disabled, ActorName: "Lifecycle", ActorEmail: disabled + "@example.com", Action: "test.lifecycle", ObjectType: "user", ObjectID: disabled, Result: application.OperationLogSuccess}
	if err = s.CreateOperationLog(ctx, audit); err != nil {
		t.Fatal(err)
	}
	inv, err := s.CreateInvitation(ctx, disabled, "Invited", "invite-lifecycle@example.com", domain.LocaleEnglish, []string{roleID}, bytes.Repeat([]byte{3}, 16), bytes.Repeat([]byte{4}, 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// Removing a disabled holder must be allowed: the active holder remains.
	if err = s.DeleteUserWithRevision(ctx, writer, disabled, 2); err != nil {
		t.Fatalf("delete disabled super: %v", err)
	}
	if err = s.DeleteUserWithRevision(ctx, writer, active, 1); !errors.Is(err, application.ErrLastSuperAdmin) {
		t.Fatalf("delete last: %v", err)
	}
	if err = s.CreateOperationLog(ctx, audit); err != nil {
		t.Fatalf("in-flight audit after deletion: %v", err)
	}
	logs, err := s.ListOperationLogs(ctx, application.OperationLogListOptions{ActorID: disabled, Limit: 25})
	if err != nil || len(logs.Items) != 2 {
		t.Fatalf("retained logs: %+v %v", logs, err)
	}
	for _, log := range logs.Items {
		if log.ActorID != disabled || !log.ActorDeleted || !log.ObjectDeleted || log.ActorEmail != audit.ActorEmail {
			t.Fatalf("deleted identity: %+v", log)
		}
	}
	retained, err := s.FindInvitation(ctx, inv.ID)
	if err != nil || retained.CreatedBy != disabled {
		t.Fatalf("retained invitation: %+v %v", retained, err)
	}
	// A new account never reuses the deleted identifier.
	disabled = "019535d9-3df7-79fb-b466-fa907fa17f94"
	// Role removal must not count a disabled holder as an available administrator.
	exec(`INSERT INTO auth_users(id,name,email,email_canonical,password_hash,disabled_at) VALUES($1,'Disabled',$2,$2,'hash',clock_timestamp())`, disabled, disabled+"@example.com")
	exec(`INSERT INTO auth_user_roles(user_id,role_id) SELECT $1::uuid,id FROM auth_roles WHERE system_key='super_admin'`, disabled)
	if _, err = s.ReplaceUserRolesWithinScope(ctx, active, active, 1, []string{roleID}); !errors.Is(err, application.ErrLastSuperAdmin) {
		t.Fatalf("role removal last: %v", err)
	}
	if _, err = s.ReactivateUserWithRevision(ctx, writer, disabled, 1); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err = s.DeactivateUserWithRevision(ctx, writer, disabled, 1); !errors.Is(err, application.ErrStaleRevision) {
		t.Fatalf("old revision: %v", err)
	}
	credential := integrationCredential('l')
	if err = s.CreateVersioned(ctx, credential, disabled, 2); err != nil {
		t.Fatal(err)
	}
	selector, digest := bytes.Repeat([]byte{7}, 16), bytes.Repeat([]byte{8}, 32)
	if err = s.RequestPasswordReset(ctx, disabled+"@example.com", selector, digest, time.Hour, domain.LocaleEnglish); err != nil {
		t.Fatal(err)
	}
	const lease = "019535d9-3df7-79fb-b466-fa907fa17f88"
	var leased *application.MailJob
	for attempt := 0; attempt < 5; attempt++ {
		job, claimErr := s.ClaimMail(ctx, lease, time.Minute)
		if claimErr != nil {
			t.Fatal(claimErr)
		}
		if job == nil {
			break
		}
		if job.Kind == application.MailPasswordReset && job.UserID == disabled {
			leased = job
			break
		}
		if _, claimErr = s.MarkMailSent(ctx, job.ID, lease); claimErr != nil {
			t.Fatal(claimErr)
		}
	}
	if leased == nil {
		t.Fatal("expected leased reset mail before deactivation")
	}
	if _, err = s.DeactivateUserWithRevision(ctx, writer, disabled, 2); err != nil {
		t.Fatal(err)
	}
	// Durable material is a historical mail snapshot, not an authorization
	// grant. Deactivation invalidates the account credential but must not
	// cancel a message already handed to the worker; the late SMTP result may
	// still finish this task exactly once.
	if marked, markErr := s.MarkMailSent(ctx, leased.ID, lease); markErr != nil || !marked {
		t.Fatalf("retained in-flight job was not completed: %v %v", marked, markErr)
	}
	for _, resolve := range []func(context.Context, string) (string, int64, error){s.ResolveVersioned, s.ResolveAndTouchVersioned} {
		if _, _, err = resolve(ctx, credential); !errors.Is(err, application.ErrAccountDisabled) {
			t.Fatalf("trusted disabled reason: %v", err)
		}
		if _, _, err = resolve(ctx, integrationCredential('z')); err != nil {
			t.Fatalf("unknown token must not disclose status: %v", err)
		}
	}
	if _, err = s.FindByCanonicalEmail(ctx, disabled+"@example.com"); !errors.Is(err, application.ErrAccountNotFound) {
		t.Fatalf("disabled login: %v", err)
	}
	if err = s.RequestPasswordReset(ctx, disabled+"@example.com", selector, digest, time.Hour, domain.LocaleEnglish); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CompletePasswordReset(ctx, selector, digest, "newhash", domain.LocaleEnglish, time.Hour); !errors.Is(err, application.ErrInvalidPasswordResetToken) {
		t.Fatalf("disabled recovery: %v", err)
	}
	if _, err = s.ReactivateUserWithRevision(ctx, writer, disabled, 3); err != nil {
		t.Fatal(err)
	}
	if id, _, err := s.ResolveVersioned(ctx, credential); err != nil || id != "" {
		t.Fatalf("old session revived: %s %v", id, err)
	}
	if _, err = s.CompletePasswordReset(ctx, selector, digest, "newhash", domain.LocaleEnglish, time.Hour); !errors.Is(err, application.ErrInvalidPasswordResetToken) {
		t.Fatalf("old reset revived: %v", err)
	}
	if err = s.RequestPasswordReset(ctx, disabled+"@example.com", selector, digest, time.Hour, domain.LocaleEnglish); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CompletePasswordReset(ctx, selector, digest, "newhash", domain.LocaleEnglish, time.Hour); err != nil {
		t.Fatalf("new reset: %v", err)
	}
	// A disabled writer cannot authorize a mutation after its request began.
	exec(`UPDATE auth_users SET disabled_at=clock_timestamp() WHERE id=$1::uuid`, writer)
	if _, err = s.DeactivateUserWithRevision(ctx, writer, disabled, 2); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("disabled actor: %v", err)
	}
}
