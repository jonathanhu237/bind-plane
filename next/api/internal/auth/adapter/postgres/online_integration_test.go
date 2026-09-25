package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	"testing"
	"time"
)

func TestRevokeSessionsIntegration(t *testing.T) {
	dsn := integrationDSN(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := resetAuthState(cleanupCtx, db); err != nil {
			t.Errorf("reset online integration state: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Errorf("close online integration database: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := newTestStore(db)
	if err := resetAuthState(ctx, db); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("online setup"))
	if _, err := s.ReplaceCurrentToken(ctx, digest[:], time.Hour); err != nil {
		t.Fatal(err)
	}
	name, _ := domain.NewName("Admin")
	email, _ := domain.NewEmail("admin@example.com")
	admin, err := s.Complete(ctx, digest[:], name, email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	const operator = "019535d9-3df7-79fb-b466-fa907fa17f95"
	const target = "019535d9-3df7-79fb-b466-fa907fa17f96"
	for _, id := range []string{operator, target} {
		if _, err := db.ExecContext(ctx, `INSERT INTO auth_users(id,name,email,email_canonical,password_hash) VALUES($1,'User',$2,$2,'hash')`, id, id+"@example.com"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RevokeUserSessions(ctx, operator, target); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("unauthorized=%v", err)
	}
	role, err := s.CreateRole(ctx, "Online manager", "", []domain.PermissionKey{domain.PermissionOnlineUsersRead, domain.PermissionOnlineUsersWrite})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_user_roles(user_id,role_id) VALUES($1,$2)`, operator, role.ID); err != nil {
		t.Fatal(err)
	}
	adminBefore, err := s.FindPublicAccountByID(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeUserSessions(ctx, operator, admin.ID); err != nil {
		t.Fatalf("manager can revoke super=%v", err)
	}
	adminAfter, err := s.FindPublicAccountByID(ctx, admin.ID)
	if err != nil || adminAfter.AuthVersion != adminBefore.AuthVersion+1 {
		t.Fatalf("super revocation=%+v err=%v", adminAfter, err)
	}
	before, err := s.FindPublicAccountByID(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeUserSessions(ctx, operator, target); err != nil {
		t.Fatal(err)
	}
	after, err := s.FindPublicAccountByID(ctx, target)
	if err != nil || after.AuthVersion != before.AuthVersion+1 {
		t.Fatalf("revocation=%+v err=%v", after, err)
	}
	if err := s.RevokeUserSessions(ctx, admin.ID, admin.ID); err != nil {
		t.Fatalf("self revocation=%v", err)
	}
}
