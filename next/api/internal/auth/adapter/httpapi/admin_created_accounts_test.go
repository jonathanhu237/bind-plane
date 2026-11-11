package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type creationMatrixStore struct {
	*matrixAccessStore
	created []application.NewUser
}

func (s *creationMatrixStore) CreateUser(_ context.Context, _ string, user application.NewUser) (domain.AccessUser, error) {
	s.created = append(s.created, user)
	roles := make([]domain.Role, 0, len(user.RoleIDs))
	for _, id := range user.RoleIDs {
		role, err := s.FindRole(context.Background(), id)
		if err != nil {
			return domain.AccessUser{}, err
		}
		roles = append(roles, role)
	}
	return domain.AccessUser{User: domain.User{ID: "019535d9-3df7-79fb-b466-fa907fa17fa0", Name: user.Name, Email: user.Email.Display, CreatedAt: time.Unix(1_700_000_000, 0), PasswordChangeRequired: user.RequirePasswordChange}, Roles: roles, AuthVersion: 1}, nil
}

type creationHasher struct{}

func (creationHasher) Hash(_ context.Context, password string) (string, error) {
	return "hashed:" + password, nil
}
func (creationHasher) Verify(context.Context, string, string) (bool, error) { return false, nil }

// passwordChangeAuth holds the session "pending" on an initial password: ordinary lookups refuse
// it, the AllowingPasswordChange ones resolve it.
type passwordChangeAuth struct {
	*matrixPrincipalAuth
	pending domain.Principal
}

func (a *passwordChangeAuth) CurrentPrincipal(ctx context.Context, session string) (domain.Principal, error) {
	if session == "pending" {
		return domain.Principal{}, application.ErrPasswordChangeRequired
	}
	return a.matrixPrincipalAuth.CurrentPrincipal(ctx, session)
}

func (a *passwordChangeAuth) Current(ctx context.Context, session string) (domain.User, error) {
	if session == "pending" {
		return domain.User{}, application.ErrPasswordChangeRequired
	}
	return a.matrixPrincipalAuth.Current(ctx, session)
}

func (a *passwordChangeAuth) CurrentPrincipalAllowingPasswordChange(ctx context.Context, session string) (domain.Principal, error) {
	if session == "pending" {
		return a.pending, nil
	}
	return a.matrixPrincipalAuth.CurrentPrincipal(ctx, session)
}

func (a *passwordChangeAuth) CurrentPrincipalAllowingPasswordChangeNoTouch(ctx context.Context, session string) (domain.Principal, error) {
	return a.CurrentPrincipalAllowingPasswordChange(ctx, session)
}

func newAccountCreationFixture() (http.Handler, *creationMatrixStore) {
	usersRole := matrixRole(matrixUsersRole, "Users reader", domain.PermissionUsersRead)
	rolesRole := matrixRole(matrixRolesRole, "Roles reader", domain.PermissionRolesRead)
	systemRole := domain.Role{ID: matrixSystemRole, Name: "Super Admin", SystemKey: "super_admin", Revision: 1}
	store := &creationMatrixStore{matrixAccessStore: &matrixAccessStore{roles: []domain.Role{systemRole, usersRole, rolesRole}}}
	super := domain.Principal{User: domain.User{ID: matrixSuperID, Name: "Super", Email: "super@example.com"}, Roles: []domain.Role{systemRole}}
	users := domain.Principal{User: domain.User{ID: matrixUsersID, Name: "Users", Email: "users@example.com"}, Roles: []domain.Role{usersRole}}
	pending := domain.Principal{User: domain.User{ID: matrixTargetID, Name: "New", Email: "new@example.com", PasswordChangeRequired: true}, Roles: []domain.Role{usersRole}}
	principals := &matrixPrincipalStore{byUser: map[string]domain.Principal{matrixSuperID: super, matrixUsersID: users, matrixTargetID: pending}}
	auth := &passwordChangeAuth{matrixPrincipalAuth: &matrixPrincipalAuth{bySession: map[string]domain.Principal{"super": super, "users": users}}, pending: pending}
	access := application.NewAccessManagementWithInvitations(store, principals, domain.DefaultPermissionCatalog(), []byte(strings.Repeat("k", 32)), matrixRandom{}, time.Hour)
	access.SetPasswordHasher(creationHasher{})
	return NewHandlerWithAccess(&setupFake{status: application.SetupComplete}, auth, testConfig(), nil, access, nil), store
}

func TestCreateUserOverHTTP(t *testing.T) {
	handler, store := newAccountCreationFixture()
	body := `{"name":"Grace","email":"grace@example.com","password":"Initial1!pass","roleIds":["` + matrixUsersRole + `"]}`

	response := matrixRequest(handler, "super", http.MethodPost, "/api/users", body, true)
	if response.Code != http.StatusCreated || strings.Contains(response.Body.String(), "Initial1!pass") {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}
	var created struct {
		User struct {
			Email                  string `json:"email"`
			PasswordChangeRequired bool   `json:"passwordChangeRequired"`
		} `json:"user"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || created.User.Email != "grace@example.com" || !created.User.PasswordChangeRequired {
		t.Fatalf("created = %+v, %v", created, err)
	}
	if len(store.created) != 1 || store.created[0].PasswordHash != "hashed:Initial1!pass" || !store.created[0].RequirePasswordChange {
		t.Fatalf("stored = %+v", store.created)
	}

	// A shared account may skip the change; a weak password, a reader and a missing origin are refused.
	shared := `{"name":"Desk","email":"desk@example.com","password":"Initial1!pass","roleIds":["` + matrixUsersRole + `"],"requirePasswordChange":false}`
	if response := matrixRequest(handler, "super", http.MethodPost, "/api/users", shared, true); response.Code != http.StatusCreated || strings.Contains(response.Body.String(), "passwordChangeRequired") || store.created[1].RequirePasswordChange {
		t.Fatalf("shared account = %d %s", response.Code, response.Body.String())
	}
	weak := strings.Replace(body, "Initial1!pass", "short", 1)
	if response := matrixRequest(handler, "super", http.MethodPost, "/api/users", weak, true); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"pointer":"/password"`) {
		t.Fatalf("weak password = %d %s", response.Code, response.Body.String())
	}
	if response := matrixRequest(handler, "users", http.MethodPost, "/api/users", body, true); response.Code != http.StatusForbidden {
		t.Fatalf("reader = %d %s", response.Code, response.Body.String())
	}
	if response := matrixRequest(handler, "super", http.MethodPost, "/api/users", body, false); response.Code != http.StatusForbidden {
		t.Fatalf("missing origin = %d %s", response.Code, response.Body.String())
	}
	if len(store.created) != 2 {
		t.Fatalf("refused requests created accounts: %+v", store.created)
	}
	if response := matrixRequest(handler, "super", http.MethodPut, "/api/users", body, true); response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, POST" {
		t.Fatalf("PUT /api/users = %d, Allow %q", response.Code, response.Header().Get("Allow"))
	}
}

func TestAccountOnItsInitialPasswordOnlyReachesTheChange(t *testing.T) {
	handler, _ := newAccountCreationFixture()
	me := matrixRequest(handler, "pending", http.MethodGet, "/api/auth/me", "", false)
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"passwordChangeRequired":true`) {
		t.Fatalf("me = %d %s", me.Code, me.Body.String())
	}
	if status := matrixRequest(handler, "pending", http.MethodGet, "/api/auth/session-status", "", false); status.Code != http.StatusOK {
		t.Fatalf("session status = %d %s", status.Code, status.Body.String())
	}
	users := matrixRequest(handler, "pending", http.MethodGet, "/api/users", "", false)
	if users.Code != http.StatusForbidden || !strings.Contains(users.Body.String(), `"code":"password_change_required"`) {
		t.Fatalf("users = %d %s", users.Code, users.Body.String())
	}
	if logout := matrixRequest(handler, "pending", http.MethodPost, "/api/auth/logout", "", true); logout.Code != http.StatusNoContent {
		t.Fatalf("logout = %d %s", logout.Code, logout.Body.String())
	}
	if me := matrixRequest(handler, "super", http.MethodGet, "/api/auth/me", "", false); me.Code != http.StatusOK || strings.Contains(me.Body.String(), "passwordChangeRequired") {
		t.Fatalf("settled me = %d %s", me.Code, me.Body.String())
	}
}
