package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type fakeRandom struct{ value byte }

func (r fakeRandom) Read(dst []byte) error {
	for i := range dst {
		dst[i] = r.value
	}
	return nil
}

type errorRandom struct{}

func (errorRandom) Read([]byte) error { return errors.New("random unavailable") }

type fakeHasher struct {
	hashCalls   int
	verifyCalls int
	valid       bool
}

func TestCompletedSetupDoesNotGenerateAnotherToken(t *testing.T) {
	setup := NewSetup(&fakeSetupStore{complete: true}, &fakeHasher{}, errorRandom{}, time.Minute)
	if token, required, err := setup.IssueStartupToken(context.Background()); err != nil || required || token != "" {
		t.Fatalf("IssueStartupToken(completed) = %q, %t, %v", token, required, err)
	}
}

func TestUnpaddedBase64URLRequiresCanonicalEncoding(t *testing.T) {
	canonical := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, setupTokenBytes))
	nonCanonical := canonical[:len(canonical)-1] + "d"
	if !isUnpaddedBase64URL(canonical, setupTokenBytes) {
		t.Fatal("canonical credential rejected")
	}
	if isUnpaddedBase64URL(nonCanonical, setupTokenBytes) {
		t.Fatal("non-canonical credential accepted")
	}
}

func (h *fakeHasher) Hash(context.Context, string) (string, error) { h.hashCalls++; return "hash", nil }
func (h *fakeHasher) Verify(context.Context, string, string) (bool, error) {
	h.verifyCalls++
	return h.valid, nil
}

type fakeSetupStore struct {
	complete bool
	digest   []byte
	user     domain.User
}

func (s *fakeSetupStore) Status(context.Context) (bool, error) { return s.complete, nil }
func (s *fakeSetupStore) ReplaceCurrentToken(_ context.Context, digest []byte, _ time.Duration) (bool, error) {
	if s.complete {
		return true, nil
	}
	s.digest = append([]byte(nil), digest...)
	return false, nil
}
func (s *fakeSetupStore) PreflightToken(_ context.Context, digest []byte) error {
	if s.complete {
		return ErrSetupComplete
	}
	if string(s.digest) != string(digest) {
		return ErrInvalidSetupToken
	}
	return nil
}
func (s *fakeSetupStore) Complete(_ context.Context, digest []byte, name domain.Name, email domain.Email, _ string) (domain.User, error) {
	if s.complete {
		return domain.User{}, ErrSetupComplete
	}
	if string(s.digest) != string(digest) {
		return domain.User{}, ErrInvalidSetupToken
	}
	s.complete = true
	s.user = domain.User{ID: "user-1", Name: string(name), Email: email.Display}
	return s.user, nil
}

func TestSetupTokenReplacementAndCompletion(t *testing.T) {
	store := &fakeSetupStore{}
	hasher := &fakeHasher{}
	setup := NewSetup(store, hasher, fakeRandom{value: 7}, time.Minute)
	token, required, err := setup.IssueStartupToken(context.Background())
	if err != nil || !required || len(token) != 43 {
		t.Fatalf("IssueStartupToken() = %q, %t, %v", token, required, err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(token)
	digest := sha256.Sum256(raw)
	if string(store.digest) != string(digest[:]) {
		t.Fatal("store did not receive token digest")
	}
	user, err := setup.Complete(context.Background(), SetupInput{Token: token, Name: " Ada ", Email: "ADA@EXAMPLE.COM", Password: "Admin1!x"})
	if err != nil || user.Name != "Ada" || user.Email != "ADA@EXAMPLE.COM" || !store.complete || hasher.hashCalls != 1 {
		t.Fatalf("Complete() = %#v, %v; store=%#v hasher=%d", user, err, store, hasher.hashCalls)
	}
	if _, err := setup.Complete(context.Background(), SetupInput{Token: token, Name: "Ada", Email: "ada@example.com", Password: "Admin1!x"}); !errors.Is(err, ErrSetupComplete) {
		t.Fatalf("replay error = %v, want setup complete", err)
	}
}

type fakeAccounts struct{ account domain.Account }

func (a fakeAccounts) FindByCanonicalEmail(_ context.Context, email string) (domain.Account, error) {
	if email != a.account.User.Email {
		return domain.Account{}, ErrAccountNotFound
	}
	return a.account, nil
}
func (a fakeAccounts) FindPublicByID(context.Context, string) (domain.User, error) {
	return a.account.User, nil
}

type principalFakeAccounts struct {
	fakeAccounts
	principal domain.Principal
	err       error
}

func (a principalFakeAccounts) FindPrincipalByID(context.Context, string) (domain.Principal, error) {
	return a.principal, a.err
}

type fakeLimiter struct {
	allow bool
	reset int
}

func (l *fakeLimiter) Allow(context.Context, string) (bool, error) { return l.allow, nil }
func (l *fakeLimiter) ResetEmail(context.Context, string) error    { l.reset++; return nil }

type fakeSessions struct {
	created, deleted string
	touchCalls       int
	noTouchCalls     int
}

func (s *fakeSessions) Create(_ context.Context, id, _ string) error {
	s.created = id
	return nil
}
func (s *fakeSessions) ResolveAndTouch(context.Context, string) (string, error) {
	s.touchCalls++
	return "user-1", nil
}
func (s *fakeSessions) Resolve(context.Context, string) (string, error) {
	s.noTouchCalls++
	return "user-1", nil
}
func (s *fakeSessions) Delete(_ context.Context, id string) error {
	s.deleted = id
	return nil
}

type versionedFakeSessions struct {
	fakeSessions
	version int64
}

func (s *versionedFakeSessions) CreateVersioned(_ context.Context, id, _ string, _ int64) error {
	s.created = id
	return nil
}

func (s *versionedFakeSessions) ResolveAndTouchVersioned(context.Context, string) (string, int64, error) {
	s.touchCalls++
	return "user-1", s.version, nil
}

func (s *versionedFakeSessions) ResolveVersioned(context.Context, string) (string, int64, error) {
	s.noTouchCalls++
	return "user-1", s.version, nil
}

type versionedFakeAccounts struct{ fakeAccounts }

func (a versionedFakeAccounts) FindPublicAccountByID(context.Context, string) (domain.Account, error) {
	return a.account, nil
}

func TestAuthenticationFlowAndUnknownEmail(t *testing.T) {
	account := domain.Account{User: domain.User{ID: "user-1", Name: "Ada", Email: "ada@example.com"}, PasswordHash: "hash"}
	hasher := &fakeHasher{valid: true}
	limiter := &fakeLimiter{allow: true}
	sessions := &fakeSessions{}
	auth := NewAuthentication(fakeAccounts{account}, hasher, sessions, limiter, fakeRandom{value: 8})
	user, session, err := auth.Login(context.Background(), LoginInput{Email: "ADA@EXAMPLE.COM", Password: "a sufficiently long password"})
	if err != nil || user.ID != "user-1" || len(session) != 43 || sessions.created != session || limiter.reset != 1 {
		t.Fatalf("Login() = %#v, %q, %v", user, session, err)
	}
	if _, err := auth.Current(context.Background(), session); err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if err := auth.Logout(context.Background(), session); err != nil || sessions.deleted != session {
		t.Fatalf("Logout() = %v, deleted=%q", err, sessions.deleted)
	}
	hasher.verifyCalls = 0
	if _, _, err := auth.Login(context.Background(), LoginInput{Email: "unknown@example.com", Password: "a sufficiently long password"}); !errors.Is(err, ErrInvalidCredentials) || hasher.verifyCalls != 0 {
		t.Fatalf("unknown login = %v, verify calls=%d", err, hasher.verifyCalls)
	}
}

func TestCurrentRequiresAuthoritativeAccountVersion(t *testing.T) {
	account := domain.Account{User: domain.User{ID: "user-1", Name: "Ada", Email: "ada@example.com"}, AuthVersion: 1}
	sessions := &versionedFakeSessions{version: 1}
	auth := NewAuthentication(fakeAccounts{account}, &fakeHasher{}, sessions, &fakeLimiter{allow: true}, fakeRandom{value: 8})
	session := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{8}, sessionIDBytes))

	if _, err := auth.Current(context.Background(), session); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("Current() error = %v, want dependency unavailable when account version store is missing", err)
	}
}

func TestCurrentRejectsStaleVersionedSession(t *testing.T) {
	account := domain.Account{User: domain.User{ID: "user-1", Name: "Ada", Email: "ada@example.com"}, AuthVersion: 2}
	sessions := &versionedFakeSessions{version: 1}
	auth := NewAuthentication(versionedFakeAccounts{fakeAccounts{account}}, &fakeHasher{}, sessions, &fakeLimiter{allow: true}, fakeRandom{value: 8})
	session := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{8}, sessionIDBytes))

	if _, err := auth.Current(context.Background(), session); !errors.Is(err, ErrUnauthenticated) || sessions.deleted != session {
		t.Fatalf("Current(stale) = %v, deleted=%q", err, sessions.deleted)
	}
}

func TestCurrentAllowsMatchingVersionedSession(t *testing.T) {
	account := domain.Account{User: domain.User{ID: "user-1", Name: "Ada", Email: "ada@example.com"}, AuthVersion: 2}
	sessions := &versionedFakeSessions{version: 2}
	auth := NewAuthentication(versionedFakeAccounts{fakeAccounts{account}}, &fakeHasher{}, sessions, &fakeLimiter{allow: true}, fakeRandom{value: 8})
	session := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{8}, sessionIDBytes))

	user, err := auth.Current(context.Background(), session)
	if err != nil || user.ID != account.User.ID {
		t.Fatalf("Current(matching) = %#v, %v", user, err)
	}
}

func TestCurrentNoTouchDoesNotRenewVersionedSession(t *testing.T) {
	account := domain.Account{User: domain.User{ID: "user-1", Name: "Ada", Email: "ada@example.com"}, AuthVersion: 2}
	sessions := &versionedFakeSessions{version: 2}
	auth := NewAuthentication(versionedFakeAccounts{fakeAccounts{account}}, &fakeHasher{}, sessions, &fakeLimiter{allow: true}, fakeRandom{value: 8})
	session := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{8}, sessionIDBytes))

	user, err := auth.CurrentNoTouch(context.Background(), session)
	if err != nil || user.ID != account.User.ID {
		t.Fatalf("CurrentNoTouch() = %#v, %v", user, err)
	}
	if sessions.noTouchCalls != 1 || sessions.touchCalls != 0 {
		t.Fatalf("session resolver calls = no-touch %d, touch %d", sessions.noTouchCalls, sessions.touchCalls)
	}
}

func TestLoginRejectsMissingAuthVersionForVersionedSessions(t *testing.T) {
	account := domain.Account{User: domain.User{ID: "user-1", Name: "Ada", Email: "ada@example.com"}, PasswordHash: "hash"}
	sessions := &versionedFakeSessions{version: 1}
	auth := NewAuthentication(fakeAccounts{account}, &fakeHasher{valid: true}, sessions, &fakeLimiter{allow: true}, fakeRandom{value: 8})

	if _, _, err := auth.Login(context.Background(), LoginInput{Email: "ada@example.com", Password: "a sufficiently long password"}); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("Login() error = %v, want dependency unavailable for missing auth version", err)
	}
}

func TestLoginWithPrincipalRejectsInvalidProjectionAndCleansSession(t *testing.T) {
	account := domain.Account{User: domain.User{ID: "user-1", Name: "Ada", Email: "ada@example.com"}, PasswordHash: "hash"}
	sessions := &fakeSessions{}
	accounts := principalFakeAccounts{fakeAccounts: fakeAccounts{account: account}, principal: domain.Principal{User: account.User}}
	auth := NewAuthentication(accounts, &fakeHasher{valid: true}, sessions, &fakeLimiter{allow: true}, fakeRandom{value: 8})

	if _, _, err := auth.LoginWithPrincipal(context.Background(), LoginInput{Email: account.User.Email, Password: "a sufficiently long password"}); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("LoginWithPrincipal() error = %v, want dependency unavailable for a zero-role projection", err)
	}
	if sessions.created == "" || sessions.deleted != sessions.created {
		t.Fatalf("LoginWithPrincipal() session cleanup = created %q, deleted %q", sessions.created, sessions.deleted)
	}
}

func TestLoginWithPrincipalRejectsMismatchedProjectionAndCleansSession(t *testing.T) {
	account := domain.Account{User: domain.User{ID: "user-1", Name: "Ada", Email: "ada@example.com"}, PasswordHash: "hash"}
	sessions := &fakeSessions{}
	accounts := principalFakeAccounts{
		fakeAccounts: fakeAccounts{account: account},
		principal: domain.Principal{
			User:  domain.User{ID: "user-2", Name: "Grace", Email: "grace@example.com"},
			Roles: []domain.Role{{SystemKey: "super_admin", Name: "Super Admin"}},
		},
	}
	auth := NewAuthentication(accounts, &fakeHasher{valid: true}, sessions, &fakeLimiter{allow: true}, fakeRandom{value: 8})

	if _, _, err := auth.LoginWithPrincipal(context.Background(), LoginInput{Email: account.User.Email, Password: "a sufficiently long password"}); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("LoginWithPrincipal() error = %v, want dependency unavailable for mismatched identity", err)
	}
	if sessions.created == "" || sessions.deleted != sessions.created {
		t.Fatalf("LoginWithPrincipal() session cleanup = created %q, deleted %q", sessions.created, sessions.deleted)
	}
}

func TestCurrentPrincipalRejectsMismatchedProjection(t *testing.T) {
	account := domain.Account{User: domain.User{ID: "user-1", Name: "Ada", Email: "ada@example.com"}}
	accounts := principalFakeAccounts{
		fakeAccounts: fakeAccounts{account: account},
		principal: domain.Principal{
			User:  domain.User{ID: "user-2", Name: "Grace", Email: "grace@example.com"},
			Roles: []domain.Role{{SystemKey: "super_admin", Name: "Super Admin"}},
		},
	}
	auth := NewAuthentication(accounts, &fakeHasher{}, &fakeSessions{}, &fakeLimiter{allow: true}, fakeRandom{value: 8})
	session := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{8}, sessionIDBytes))

	if _, err := auth.CurrentPrincipal(context.Background(), session); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("CurrentPrincipal() error = %v, want dependency unavailable for mismatched identity", err)
	}
}

func TestNormalizePrincipalRejectsUnknownCustomRoleData(t *testing.T) {
	catalog := domain.DefaultPermissionCatalog()
	for _, role := range []domain.Role{
		{ID: "role-2", Name: "Unknown", Permissions: []domain.PermissionKey{"projects.write"}},
		{ID: "role-3", SystemKey: "auditor", Name: "Auditor"},
	} {
		if _, err := normalizePrincipal(catalog, domain.Principal{Roles: []domain.Role{role}}); !errors.Is(err, ErrDependencyUnavailable) {
			t.Fatalf("normalizePrincipal(%#v) error = %v, want dependency unavailable", role, err)
		}
	}
	if principal, err := normalizePrincipal(catalog, domain.Principal{Roles: []domain.Role{{SystemKey: "super_admin", Name: "Super Admin"}}}); err != nil || !principal.SuperAdmin || !principal.Has(domain.PermissionRolesRead) {
		t.Fatalf("normalizePrincipal(super) = %#v, %v", principal, err)
	}
}

// Bind Plane's Operator role signs in and releases bindings but holds no administration permission.
func TestNormalizePrincipalAcceptsRoleWithoutPermissions(t *testing.T) {
	catalog := domain.DefaultPermissionCatalog()
	operator, err := normalizePrincipal(catalog, domain.Principal{Roles: []domain.Role{{ID: "role-operator", Name: "Operator"}}})
	if err != nil || operator.SuperAdmin || len(operator.Permissions) != 0 || operator.Roles[0].Permissions == nil {
		t.Fatalf("normalizePrincipal(operator) = %#v, %v", operator, err)
	}
	mixed, err := normalizePrincipal(catalog, domain.Principal{Roles: []domain.Role{
		{ID: "role-operator", Name: "Operator"},
		{ID: "role-users", Name: "Users", Permissions: []domain.PermissionKey{domain.PermissionUsersRead}},
	}})
	if err != nil || !mixed.Has(domain.PermissionUsersRead) || len(mixed.Permissions) != 1 {
		t.Fatalf("normalizePrincipal(operator and users) = %#v, %v", mixed, err)
	}
}

func TestNormalizePrincipalUnionsRolePermissionsWhenProjectionOmitsSummary(t *testing.T) {
	principal, err := normalizePrincipal(domain.DefaultPermissionCatalog(), domain.Principal{Roles: []domain.Role{
		{ID: "role-users", Name: "Users", Permissions: []domain.PermissionKey{domain.PermissionUsersRead}},
		{ID: "role-roles", Name: "Roles", Permissions: []domain.PermissionKey{domain.PermissionRolesRead}},
	}})
	if err != nil {
		t.Fatalf("normalizePrincipal() error = %v", err)
	}
	if !principal.Has(domain.PermissionUsersRead) || !principal.Has(domain.PermissionRolesRead) {
		t.Fatalf("normalized principal permissions = %#v", principal.Permissions)
	}
	if got := principal.Permissions; len(got) != 2 || got[0] != domain.PermissionRolesRead || got[1] != domain.PermissionUsersRead {
		t.Fatalf("normalized principal permissions = %#v, want sorted union", got)
	}
}

func TestNormalizePrincipalRejectsPermissionInjectedOutsideRoleAssignments(t *testing.T) {
	_, err := normalizePrincipal(domain.DefaultPermissionCatalog(), domain.Principal{
		Roles:       []domain.Role{{ID: "role-users", Name: "Users", Permissions: []domain.PermissionKey{domain.PermissionUsersRead}}},
		Permissions: []domain.PermissionKey{domain.PermissionUsersRead, domain.PermissionRolesRead},
	})
	if !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("normalizePrincipal() error = %v, want dependency unavailable for injected permission", err)
	}
}

func TestPermissionSummaryCannotGrantAuthorizationOutsideRoleAssignments(t *testing.T) {
	accounts := principalFakeAccounts{principal: domain.Principal{
		User:        domain.User{ID: "user-1", Name: "Ada", Email: "ada@example.com"},
		Roles:       []domain.Role{{ID: "role-users", Name: "Users", Permissions: []domain.PermissionKey{domain.PermissionUsersRead}}},
		Permissions: []domain.PermissionKey{domain.PermissionUsersRead, domain.PermissionRolesRead},
	}}
	manager := NewAccessManagement(nil, accounts, domain.DefaultPermissionCatalog())
	if err := manager.require(context.Background(), "user-1", domain.PermissionRolesRead); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("require() error = %v, want dependency unavailable for injected permission", err)
	}
}
