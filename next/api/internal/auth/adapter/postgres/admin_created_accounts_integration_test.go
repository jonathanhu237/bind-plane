package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// An administrator creates an account directly: it keeps the roles granted, may have to replace
// its initial password first, and is refused where an invitation would be refused.
func TestStoreIntegrationAdminCreatedAccounts(t *testing.T) {
	dsn := integrationDSN(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	store := newTestStore(db)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := resetAuthState(cleanupCtx, db); err != nil {
			t.Errorf("reset auth state: %v", err)
		}
		_ = db.Close()
	})
	if err := resetAuthState(ctx, db); err != nil {
		t.Fatal(err)
	}
	setupDigest := sha256.Sum256([]byte("admin-created accounts setup token"))
	if _, err := store.ReplaceCurrentToken(ctx, setupDigest[:], time.Hour); err != nil {
		t.Fatal(err)
	}
	adminName, _ := domain.NewName("Account Admin")
	adminEmail, _ := domain.NewEmail("account-admin@example.com")
	admin, err := store.Complete(ctx, setupDigest[:], adminName, adminEmail, "$argon2id$admin")
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	var superRoleID string
	if err := db.QueryRowContext(ctx, `SELECT id::text FROM auth_roles WHERE system_key = 'super_admin'`).Scan(&superRoleID); err != nil {
		t.Fatal(err)
	}
	readers, err := store.CreateRole(ctx, "Readers", "", []domain.PermissionKey{domain.PermissionUsersRead})
	if err != nil {
		t.Fatal(err)
	}

	email, _ := domain.NewEmail("Created.Person@Example.com")
	created, err := store.CreateUser(ctx, admin.ID, application.NewUser{Name: "Created Person", Email: email, PasswordHash: "$argon2id$initial", RoleIDs: []string{readers.ID}, RequirePasswordChange: true})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if !created.User.PasswordChangeRequired || created.User.Email != "Created.Person@Example.com" || len(created.Roles) != 1 || created.Roles[0].ID != readers.ID || created.AuthVersion != 1 {
		t.Fatalf("created = %#v", created)
	}
	account, err := store.FindByCanonicalEmail(ctx, "created.person@example.com")
	if err != nil || account.PasswordHash != "$argon2id$initial" || !account.User.PasswordChangeRequired || account.User.Locale != "" {
		t.Fatalf("account = %#v, %v", account, err)
	}
	if principal, err := store.FindPrincipalByID(ctx, created.User.ID); err != nil || !principal.User.PasswordChangeRequired {
		t.Fatalf("principal = %#v, %v", principal, err)
	}
	if listed, err := store.FindAccessUser(ctx, created.User.ID); err != nil || !listed.User.PasswordChangeRequired {
		t.Fatalf("listed = %#v, %v", listed, err)
	}

	// The email is taken by the account, and by a pending invitation for another address.
	if _, err := store.CreateUser(ctx, admin.ID, application.NewUser{Name: "Again", Email: email, PasswordHash: "x", RoleIDs: []string{readers.ID}}); !errors.Is(err, application.ErrEmailAlreadyRegistered) {
		t.Fatalf("duplicate email = %v", err)
	}
	if _, err := store.CreateInvitation(ctx, admin.ID, "Invited", "invited@example.com", domain.LocaleEnglish, []string{readers.ID}, bytes.Repeat([]byte{0x41}, 16), bytes.Repeat([]byte{0x42}, 32), time.Hour); err != nil {
		t.Fatalf("CreateInvitation() error = %v", err)
	}
	invited, _ := domain.NewEmail("INVITED@example.com")
	if _, err := store.CreateUser(ctx, admin.ID, application.NewUser{Name: "Invited", Email: invited, PasswordHash: "x", RoleIDs: []string{readers.ID}}); !errors.Is(err, application.ErrInvitationPending) {
		t.Fatalf("pending invitation = %v", err)
	}

	// An actor who is no Super Admin grants only roles within their own permissions.
	managers, err := store.CreateRole(ctx, "Account managers", "", []domain.PermissionKey{domain.PermissionUsersWrite, domain.PermissionRolesRead})
	if err != nil {
		t.Fatal(err)
	}
	roleReaders, err := store.CreateRole(ctx, "Role readers", "", []domain.PermissionKey{domain.PermissionRolesRead})
	if err != nil {
		t.Fatal(err)
	}
	managerEmail, _ := domain.NewEmail("manager@example.com")
	manager, err := store.CreateUser(ctx, admin.ID, application.NewUser{Name: "Manager", Email: managerEmail, PasswordHash: "x", RoleIDs: []string{managers.ID}})
	if err != nil || manager.User.PasswordChangeRequired {
		t.Fatalf("manager = %#v, %v", manager, err)
	}
	for _, roleID := range []string{superRoleID, readers.ID} {
		other, _ := domain.NewEmail("scoped@example.com")
		if _, err := store.CreateUser(ctx, manager.User.ID, application.NewUser{Name: "Scoped", Email: other, PasswordHash: "x", RoleIDs: []string{roleID}}); !errors.Is(err, application.ErrPermissionScope) {
			t.Fatalf("manager granting %s = %v", roleID, err)
		}
	}
	scopedEmail, _ := domain.NewEmail("scoped@example.com")
	if _, err := store.CreateUser(ctx, manager.User.ID, application.NewUser{Name: "Scoped", Email: scopedEmail, PasswordHash: "x", RoleIDs: []string{roleReaders.ID}}); err != nil {
		t.Fatalf("manager granting a role within scope = %v", err)
	}
	readerEmail, _ := domain.NewEmail("reader@example.com")
	reader, err := store.CreateUser(ctx, admin.ID, application.NewUser{Name: "Reader", Email: readerEmail, PasswordHash: "x", RoleIDs: []string{readers.ID}})
	if err != nil {
		t.Fatal(err)
	}
	nobodyEmail, _ := domain.NewEmail("nobody@example.com")
	if _, err := store.CreateUser(ctx, reader.User.ID, application.NewUser{Name: "Nobody", Email: nobodyEmail, PasswordHash: "x", RoleIDs: []string{readers.ID}}); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("reader creating an account = %v", err)
	}

	// Choosing a password of one's own ends the requirement.
	if _, _, err := store.ChangePassword(ctx, created.User.ID, account.AuthVersion, "$argon2id$chosen", domain.LocaleEnglish, time.Hour); err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}
	if account, err = store.FindByCanonicalEmail(ctx, "created.person@example.com"); err != nil || account.User.PasswordChangeRequired || account.PasswordHash != "$argon2id$chosen" {
		t.Fatalf("after change = %#v, %v", account, err)
	}
}
