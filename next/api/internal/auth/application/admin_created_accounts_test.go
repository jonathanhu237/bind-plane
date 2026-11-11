package application

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// An account still on the initial password an administrator gave it signs in, but every ordinary
// lookup of its session is refused until it chooses its own; only the AllowingPasswordChange
// variants resolve it.
func TestAuthenticationHoldsAnAccountThatMustChangeItsPassword(t *testing.T) {
	user := domain.User{ID: "user-1", Name: "Ada", Email: "ada@example.com", PasswordChangeRequired: true}
	accounts := principalFakeAccounts{
		fakeAccounts: fakeAccounts{account: domain.Account{User: user, PasswordHash: "hash"}},
		principal:    domain.Principal{User: user, Roles: []domain.Role{{SystemKey: "super_admin", Name: "Super Admin"}}},
	}
	auth := NewAuthentication(accounts, &fakeHasher{valid: true}, &fakeSessions{}, &fakeLimiter{allow: true}, fakeRandom{value: 8})
	ctx := context.Background()
	principal, session, err := auth.LoginWithPrincipal(ctx, LoginInput{Email: user.Email, Password: "Initial1!pass"})
	if err != nil || !principal.User.PasswordChangeRequired || session == "" {
		t.Fatalf("LoginWithPrincipal() = %#v, %q, %v", principal, session, err)
	}
	if _, err := auth.Current(ctx, session); !errors.Is(err, ErrPasswordChangeRequired) {
		t.Fatalf("Current() error = %v", err)
	}
	if _, err := auth.CurrentNoTouch(ctx, session); !errors.Is(err, ErrPasswordChangeRequired) {
		t.Fatalf("CurrentNoTouch() error = %v", err)
	}
	if _, err := auth.CurrentPrincipal(ctx, session); !errors.Is(err, ErrPasswordChangeRequired) {
		t.Fatalf("CurrentPrincipal() error = %v", err)
	}
	if _, err := auth.CurrentPrincipalNoTouch(ctx, session); !errors.Is(err, ErrPasswordChangeRequired) {
		t.Fatalf("CurrentPrincipalNoTouch() error = %v", err)
	}
	for _, lookup := range []func(context.Context, string) (domain.Principal, error){auth.CurrentPrincipalAllowingPasswordChange, auth.CurrentPrincipalAllowingPasswordChangeNoTouch} {
		if held, err := lookup(ctx, session); err != nil || held.User.ID != user.ID || !held.User.PasswordChangeRequired {
			t.Fatalf("lookup allowing a password change = %#v, %v", held, err)
		}
	}

	settled := user
	settled.PasswordChangeRequired = false
	accounts.fakeAccounts.account.User = settled
	accounts.principal.User = settled
	auth = NewAuthentication(accounts, &fakeHasher{valid: true}, &fakeSessions{}, &fakeLimiter{allow: true}, fakeRandom{value: 8})
	if _, err := auth.CurrentPrincipal(ctx, base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{8}, sessionIDBytes))); err != nil {
		t.Fatalf("CurrentPrincipal(settled) error = %v", err)
	}
}

type creationStore struct {
	AccessStore
	created []NewUser
}

func (s *creationStore) CreateUser(_ context.Context, _ string, user NewUser) (domain.AccessUser, error) {
	s.created = append(s.created, user)
	return domain.AccessUser{User: domain.User{ID: "019535d9-3df7-79fb-b466-fa907fa17fa0", Name: user.Name, Email: user.Email.Display, PasswordChangeRequired: user.RequirePasswordChange},
		Roles: []domain.Role{{ID: user.RoleIDs[0], Name: "Readers", Permissions: []domain.PermissionKey{domain.PermissionUsersRead}, Revision: 1}}, AuthVersion: 1}, nil
}

type creationPrincipals map[string]domain.Principal

func (p creationPrincipals) FindPrincipalByID(_ context.Context, id string) (domain.Principal, error) {
	principal, ok := p[id]
	if !ok {
		return domain.Principal{}, ErrAccountNotFound
	}
	return principal, nil
}

func TestCreateUserChecksTheActorAndThePassword(t *testing.T) {
	const roleID = "019535d9-3df7-79fb-b466-fa907fa17fa1"
	super := domain.Principal{User: domain.User{ID: "admin"}, Roles: []domain.Role{{SystemKey: "super_admin", Name: "Super Admin"}}}
	reader := domain.Principal{User: domain.User{ID: "reader"}, Roles: []domain.Role{{ID: roleID, Name: "Readers", Permissions: []domain.PermissionKey{domain.PermissionUsersRead}, Revision: 1}}}
	store := &creationStore{}
	manager := NewAccessManagement(store, creationPrincipals{"admin": super, "reader": reader}, domain.DefaultPermissionCatalog())
	ctx := context.Background()
	input := CreateUserInput{Name: "Grace", Email: "Grace@Example.com", Password: "Initial1!pass", RoleIDs: []string{roleID}, RequirePasswordChange: true}
	if _, err := manager.CreateUser(ctx, "admin", input); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("CreateUser without a hasher = %v", err)
	}
	hasher := &fakeHasher{}
	manager.SetPasswordHasher(hasher)
	if _, err := manager.CreateUser(ctx, "reader", input); !errors.Is(err, ErrForbidden) || len(store.created) != 0 {
		t.Fatalf("reader CreateUser = %v, created %d", err, len(store.created))
	}
	weak := input
	weak.Password = "short"
	if _, err := manager.CreateUser(ctx, "admin", weak); err == nil || hasher.hashCalls != 0 {
		t.Fatalf("weak password = %v, hash calls %d", err, hasher.hashCalls)
	}
	noRoles := input
	noRoles.RoleIDs = nil
	if _, err := manager.CreateUser(ctx, "admin", noRoles); !errors.Is(err, ErrInvalidRoleSet) {
		t.Fatalf("no roles = %v", err)
	}
	created, err := manager.CreateUser(ctx, "admin", input)
	if err != nil || !created.User.PasswordChangeRequired || len(store.created) != 1 {
		t.Fatalf("CreateUser() = %#v, %v", created, err)
	}
	if got := store.created[0]; got.PasswordHash != "hash" || got.Email.Canonical != "grace@example.com" || !got.RequirePasswordChange || got.Name != "Grace" {
		t.Fatalf("stored = %#v", got)
	}
}
