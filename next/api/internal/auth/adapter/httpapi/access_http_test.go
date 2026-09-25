package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

const (
	matrixSuperID           = "019535d9-3df7-79fb-b466-fa907fa17f90"
	matrixUsersID           = "019535d9-3df7-79fb-b466-fa907fa17f91"
	matrixRolesID           = "019535d9-3df7-79fb-b466-fa907fa17f92"
	matrixTargetID          = "019535d9-3df7-79fb-b466-fa907fa17f93"
	matrixSystemRole        = "019535d9-3df7-79fb-b466-fa907fa17f94"
	matrixUsersRole         = "019535d9-3df7-79fb-b466-fa907fa17f95"
	matrixRolesRole         = "019535d9-3df7-79fb-b466-fa907fa17f96"
	matrixInviteID          = "019535d9-3df7-79fb-b466-fa907fa17f97"
	matrixInvitesReadRole   = "019535d9-3df7-79fb-b466-fa907fa17f98"
	matrixInvitesManageRole = "019535d9-3df7-79fb-b466-fa907fa17f99"
)

type matrixPrincipalAuth struct {
	bySession map[string]domain.Principal
}

func (a *matrixPrincipalAuth) Login(context.Context, application.LoginInput) (domain.User, string, error) {
	return domain.User{}, "", application.ErrInvalidCredentials
}

func (a *matrixPrincipalAuth) Current(_ context.Context, session string) (domain.User, error) {
	principal, ok := a.bySession[session]
	if !ok {
		return domain.User{}, application.ErrUnauthenticated
	}
	return principal.User, nil
}

func (*matrixPrincipalAuth) Logout(context.Context, string) error { return nil }

func (a *matrixPrincipalAuth) LoginWithPrincipal(context.Context, application.LoginInput) (domain.Principal, string, error) {
	return domain.Principal{}, "", application.ErrInvalidCredentials
}

func (a *matrixPrincipalAuth) CurrentPrincipal(_ context.Context, session string) (domain.Principal, error) {
	principal, ok := a.bySession[session]
	if !ok {
		return domain.Principal{}, application.ErrUnauthenticated
	}
	return principal, nil
}

type matrixPrincipalStore struct {
	byUser map[string]domain.Principal
}

func (s *matrixPrincipalStore) FindPrincipalByID(_ context.Context, id string) (domain.Principal, error) {
	principal, ok := s.byUser[id]
	if !ok {
		return domain.Principal{}, application.ErrAccountNotFound
	}
	return principal, nil
}

type matrixRandom struct{}

func (matrixRandom) Read(dst []byte) error {
	for index := range dst {
		dst[index] = byte(index + 1)
	}
	return nil
}

type matrixAccessStore struct {
	roles                 []domain.Role
	invitationRoleID      string
	createRoleCalls       int
	replaceRoleCalls      int
	deleteRoleCalls       int
	listRolesCalls        int
	listUsersCalls        int
	replaceUserRolesCalls int
	createInvitationCalls int
	listInvitationsCalls  int
	resendInvitationCalls int
	revokeInvitationCalls int
}

func (s *matrixAccessStore) ListRoles(context.Context) ([]domain.Role, error) {
	s.listRolesCalls++
	return cloneRoles(s.roles), nil
}

func (s *matrixAccessStore) ListRoleOptions(context.Context) ([]application.RoleOption, error) {
	options := make([]application.RoleOption, 0, len(s.roles))
	for _, role := range s.roles {
		options = append(options, application.RoleOption{ID: role.ID, Name: role.Name})
	}
	return options, nil
}

func (s *matrixAccessStore) FindRole(_ context.Context, id string) (domain.Role, error) {
	for _, role := range s.roles {
		if role.ID == id {
			return role, nil
		}
	}
	return domain.Role{}, application.ErrRoleNotFound
}

func (s *matrixAccessStore) CreateRole(_ context.Context, name, description string, permissions []domain.PermissionKey) (domain.Role, error) {
	s.createRoleCalls++
	return domain.Role{ID: "019535d9-3df7-79fb-b466-fa907fa17f98", Name: name, Description: description, Permissions: append([]domain.PermissionKey(nil), permissions...), Revision: 1}, nil
}

func (s *matrixAccessStore) ReplaceRole(_ context.Context, id string, revision int64, name, description string, permissions []domain.PermissionKey) (domain.Role, error) {
	s.replaceRoleCalls++
	return domain.Role{ID: id, Name: name, Description: description, Permissions: append([]domain.PermissionKey(nil), permissions...), Revision: revision + 1}, nil
}

func (s *matrixAccessStore) DeleteRole(context.Context, string) error {
	s.deleteRoleCalls++
	return nil
}

func (s *matrixAccessStore) ListUsers(context.Context, string, int) (application.UserPage, error) {
	s.listUsersCalls++
	role := s.roles[1]
	return application.UserPage{Items: []domain.AccessUser{{
		User:        domain.User{ID: matrixTargetID, Name: "Target", Email: "target@example.com"},
		Roles:       []domain.Role{role},
		AuthVersion: 1,
	}}}, nil
}

func (s *matrixAccessStore) ReplaceUserRoles(_ context.Context, userID string, authVersion int64, roleIDs []string) (domain.AccessUser, error) {
	s.replaceUserRolesCalls++
	roles := make([]domain.Role, 0, len(roleIDs))
	for _, id := range roleIDs {
		role, err := s.FindRole(context.Background(), id)
		if err != nil {
			return domain.AccessUser{}, err
		}
		roles = append(roles, role)
	}
	return domain.AccessUser{User: domain.User{ID: userID, Name: "Target", Email: "target@example.com"}, Roles: roles, AuthVersion: authVersion + 1}, nil
}

func (s *matrixAccessStore) CreateInvitation(_ context.Context, createdBy, name, email string, locale domain.Locale, roleIDs []string, _ []byte, _ []byte, ttl time.Duration) (domain.Invitation, error) {
	s.createInvitationCalls++
	roles := make([]domain.Role, 0, len(roleIDs))
	for _, id := range roleIDs {
		role, err := s.FindRole(context.Background(), id)
		if err != nil {
			return domain.Invitation{}, err
		}
		roles = append(roles, role)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	return domain.Invitation{ID: matrixInviteID, Name: name, Email: email, Locale: locale, Roles: roles, ExpiresAt: now.Add(ttl), CreatedAt: now, Revision: 1, CreatedBy: createdBy}, nil
}

func (s *matrixAccessStore) ListInvitations(context.Context, string, int) (application.InvitationPage, error) {
	s.listInvitationsCalls++
	role := s.roles[1]
	now := time.Unix(1_700_000_000, 0).UTC()
	return application.InvitationPage{Items: []domain.Invitation{{ID: matrixInviteID, Name: "Invitee", Email: "invitee@example.com", Locale: domain.LocaleEnglish, Roles: []domain.Role{role}, ExpiresAt: now.Add(time.Hour), CreatedAt: now, Revision: 1}}}, nil
}

func (s *matrixAccessStore) FindInvitation(_ context.Context, id string) (domain.Invitation, error) {
	if id != matrixInviteID {
		return domain.Invitation{}, application.ErrInvitationNotFound
	}
	roleID := s.invitationRoleID
	if roleID == "" {
		roleID = matrixRolesRole
	}
	var role domain.Role
	for _, candidate := range s.roles {
		if candidate.ID == roleID {
			role = candidate
			break
		}
	}
	if role.ID == "" {
		return domain.Invitation{}, application.ErrRoleNotFound
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	return domain.Invitation{ID: id, Name: "Invitee", Email: "invitee@example.com", Locale: domain.LocaleEnglish, Roles: []domain.Role{role}, ExpiresAt: now.Add(time.Hour), CreatedAt: now, Revision: 1}, nil
}

func (s *matrixAccessStore) ResendInvitation(_ context.Context, id string, locale domain.Locale, _ []byte, _ []byte, ttl time.Duration) (domain.Invitation, error) {
	s.resendInvitationCalls++
	role := s.roles[1]
	now := time.Unix(1_700_000_000, 0).UTC()
	return domain.Invitation{ID: id, Name: "Invitee", Email: "invitee@example.com", Locale: locale, Roles: []domain.Role{role}, ExpiresAt: now.Add(ttl), CreatedAt: now, Revision: 2}, nil
}

func (s *matrixAccessStore) RevokeInvitation(context.Context, string) error {
	s.revokeInvitationCalls++
	return nil
}

func (*matrixAccessStore) PreflightInvitation(context.Context, []byte, []byte) error { return nil }
func (*matrixAccessStore) CompleteInvitation(context.Context, []byte, []byte, string) error {
	return nil
}

func cloneRoles(roles []domain.Role) []domain.Role {
	result := make([]domain.Role, len(roles))
	copy(result, roles)
	for index := range result {
		result[index].Permissions = append([]domain.PermissionKey(nil), roles[index].Permissions...)
	}
	return result
}

func matrixRole(id, name string, permission domain.PermissionKey) domain.Role {
	return matrixRoleWithPermissions(id, name, permission)
}

func matrixRoleWithPermissions(id, name string, permissions ...domain.PermissionKey) domain.Role {
	return domain.Role{ID: id, Name: name, Permissions: append([]domain.PermissionKey(nil), permissions...), Revision: 1}
}

func newAccessHTTPMatrixFixture() (http.Handler, *matrixAccessStore) {
	usersRole := matrixRole(matrixUsersRole, "Users reader", domain.PermissionUsersRead)
	rolesRole := matrixRole(matrixRolesRole, "Roles reader", domain.PermissionRolesRead)
	invitationsReadRole := matrixRole(matrixInvitesReadRole, "Invitations reader", domain.PermissionInvitationsRead)
	invitationsManageRole := matrixRoleWithPermissions(matrixInvitesManageRole, "Invitations manager", domain.PermissionInvitationsRead, domain.PermissionInvitationsWrite, domain.PermissionRolesRead)
	systemRole := domain.Role{ID: matrixSystemRole, Name: "Super Admin", SystemKey: "super_admin", Revision: 1}
	store := &matrixAccessStore{roles: []domain.Role{systemRole, usersRole, rolesRole, invitationsReadRole, invitationsManageRole}}
	super := domain.Principal{User: domain.User{ID: matrixSuperID, Name: "Super", Email: "super@example.com"}, Roles: []domain.Role{systemRole}}
	users := domain.Principal{User: domain.User{ID: matrixUsersID, Name: "Users", Email: "users@example.com"}, Roles: []domain.Role{usersRole}}
	roles := domain.Principal{User: domain.User{ID: matrixRolesID, Name: "Roles", Email: "roles@example.com"}, Roles: []domain.Role{rolesRole}}
	invitationsReader := domain.Principal{User: domain.User{ID: matrixInvitesReadRole, Name: "Invitations reader", Email: "invitations-reader@example.com"}, Roles: []domain.Role{invitationsReadRole}}
	invitationsManager := domain.Principal{User: domain.User{ID: matrixInvitesManageRole, Name: "Invitations manager", Email: "invitations-manager@example.com"}, Roles: []domain.Role{invitationsManageRole}}
	principals := &matrixPrincipalStore{byUser: map[string]domain.Principal{
		matrixSuperID:           super,
		matrixUsersID:           users,
		matrixRolesID:           roles,
		matrixInvitesReadRole:   invitationsReader,
		matrixInvitesManageRole: invitationsManager,
	}}
	auth := &matrixPrincipalAuth{bySession: map[string]domain.Principal{"super": super, "users": users, "roles": roles, "invitations-reader": invitationsReader, "invitations-manager": invitationsManager}}
	access := application.NewAccessManagementWithInvitations(store, principals, domain.DefaultPermissionCatalog(), []byte(strings.Repeat("k", 32)), matrixRandom{}, time.Hour)
	return NewHandlerWithAccess(&setupFake{status: application.SetupComplete}, auth, testConfig(), nil, access, nil), store
}

func matrixRequest(handler http.Handler, session, method, path, body string, origin bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin {
		req.Header.Set("Origin", testConfig().Origin)
	}
	if session != "" {
		req.AddCookie(&http.Cookie{Name: testConfig().CookieName, Value: session})
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestAccessHTTPPermissionMatrix(t *testing.T) {
	handler, store := newAccessHTTPMatrixFixture()
	cases := []struct {
		name       string
		session    string
		method     string
		path       string
		body       string
		origin     bool
		wantStatus int
		touches    bool
		writes     bool
	}{
		{name: "users reader can list users", session: "users", method: http.MethodGet, path: "/api/users", wantStatus: http.StatusOK, touches: true},
		{name: "roles reader cannot list users", session: "roles", method: http.MethodGet, path: "/api/users", wantStatus: http.StatusForbidden},
		{name: "super can list users", session: "super", method: http.MethodGet, path: "/api/users", wantStatus: http.StatusOK, touches: true},
		{name: "roles reader can list roles", session: "roles", method: http.MethodGet, path: "/api/roles", wantStatus: http.StatusOK, touches: true},
		{name: "users reader cannot list roles", session: "users", method: http.MethodGet, path: "/api/roles", wantStatus: http.StatusForbidden},
		{name: "super can list roles", session: "super", method: http.MethodGet, path: "/api/roles", wantStatus: http.StatusOK, touches: true},
		{name: "roles reader can list role options", session: "roles", method: http.MethodGet, path: "/api/access/role-options", wantStatus: http.StatusOK},
		{name: "users reader cannot list role options", session: "users", method: http.MethodGet, path: "/api/access/role-options", wantStatus: http.StatusForbidden},
		{name: "super can list invitations", session: "super", method: http.MethodGet, path: "/api/user-invitations", wantStatus: http.StatusOK, touches: true},
		{name: "invitations reader can list invitations", session: "invitations-reader", method: http.MethodGet, path: "/api/user-invitations", wantStatus: http.StatusOK, touches: true},
		{name: "invitations reader cannot list users without dependency", session: "invitations-reader", method: http.MethodGet, path: "/api/users", wantStatus: http.StatusForbidden},
		{name: "invitations reader cannot list roles without dependency", session: "invitations-reader", method: http.MethodGet, path: "/api/roles", wantStatus: http.StatusForbidden},
		{name: "users reader cannot list invitations", session: "users", method: http.MethodGet, path: "/api/user-invitations", wantStatus: http.StatusForbidden},
		{name: "roles reader cannot list invitations", session: "roles", method: http.MethodGet, path: "/api/user-invitations", wantStatus: http.StatusForbidden},
		{name: "super can create role", session: "super", method: http.MethodPost, path: "/api/roles", body: `{"name":"Auditor","description":"Read access","permissions":["users.read"]}`, origin: true, wantStatus: http.StatusCreated, touches: true, writes: true},
		{name: "users reader cannot create role", session: "users", method: http.MethodPost, path: "/api/roles", body: `{"name":"Auditor","description":"Read access","permissions":["users.read"]}`, origin: true, wantStatus: http.StatusForbidden},
		{name: "super can replace role", session: "super", method: http.MethodPut, path: "/api/roles/019535d9-3df7-79fb-b466-fa907fa17f95", body: `{"name":"Users reader","description":"Updated","permissions":["users.read"],"revision":1}`, origin: true, wantStatus: http.StatusOK, touches: true, writes: true},
		{name: "roles reader cannot replace role", session: "roles", method: http.MethodPut, path: "/api/roles/019535d9-3df7-79fb-b466-fa907fa17f95", body: `{"name":"Users reader","description":"Updated","permissions":["users.read"],"revision":1}`, origin: true, wantStatus: http.StatusForbidden},
		{name: "super can delete role", session: "super", method: http.MethodDelete, path: "/api/roles/019535d9-3df7-79fb-b466-fa907fa17f95", origin: true, wantStatus: http.StatusNoContent, touches: true, writes: true},
		{name: "roles reader cannot delete role", session: "roles", method: http.MethodDelete, path: "/api/roles/019535d9-3df7-79fb-b466-fa907fa17f95", origin: true, wantStatus: http.StatusForbidden},
		{name: "super can replace user roles", session: "super", method: http.MethodPut, path: "/api/users/019535d9-3df7-79fb-b466-fa907fa17f93/roles", body: `{"roleIds":["019535d9-3df7-79fb-b466-fa907fa17f95"],"authVersion":1}`, origin: true, wantStatus: http.StatusOK, touches: true, writes: true},
		{name: "users reader cannot replace user roles", session: "users", method: http.MethodPut, path: "/api/users/019535d9-3df7-79fb-b466-fa907fa17f93/roles", body: `{"roleIds":["019535d9-3df7-79fb-b466-fa907fa17f95"],"authVersion":1}`, origin: true, wantStatus: http.StatusForbidden},
		{name: "super can create invitation", session: "super", method: http.MethodPost, path: "/api/user-invitations", body: `{"name":"Invitee","email":"invitee@example.com","roleIds":["019535d9-3df7-79fb-b466-fa907fa17f96"]}`, origin: true, wantStatus: http.StatusCreated, touches: true, writes: true},
		{name: "invitations manager can create invitation", session: "invitations-manager", method: http.MethodPost, path: "/api/user-invitations", body: `{"name":"Invitee","email":"invitee@example.com","roleIds":["019535d9-3df7-79fb-b466-fa907fa17f96"]}`, origin: true, wantStatus: http.StatusCreated, touches: true, writes: true},
		{name: "invitations reader cannot create invitation", session: "invitations-reader", method: http.MethodPost, path: "/api/user-invitations", body: `{"name":"Invitee","email":"invitee@example.com","roleIds":["019535d9-3df7-79fb-b466-fa907fa17f96"]}`, origin: true, wantStatus: http.StatusForbidden},
		{name: "roles reader cannot create invitation", session: "roles", method: http.MethodPost, path: "/api/user-invitations", body: `{"name":"Invitee","email":"invitee@example.com","roleIds":["019535d9-3df7-79fb-b466-fa907fa17f96"]}`, origin: true, wantStatus: http.StatusForbidden},
		{name: "super can resend invitation", session: "super", method: http.MethodPost, path: "/api/user-invitations/019535d9-3df7-79fb-b466-fa907fa17f97/resend", origin: true, wantStatus: http.StatusAccepted, touches: true, writes: true},
		{name: "invitations manager can resend invitation", session: "invitations-manager", method: http.MethodPost, path: "/api/user-invitations/019535d9-3df7-79fb-b466-fa907fa17f97/resend", origin: true, wantStatus: http.StatusAccepted, touches: true, writes: true},
		{name: "users reader cannot resend invitation", session: "users", method: http.MethodPost, path: "/api/user-invitations/019535d9-3df7-79fb-b466-fa907fa17f97/resend", origin: true, wantStatus: http.StatusForbidden},
		{name: "invitations reader cannot resend invitation", session: "invitations-reader", method: http.MethodPost, path: "/api/user-invitations/019535d9-3df7-79fb-b466-fa907fa17f97/resend", origin: true, wantStatus: http.StatusForbidden},
		{name: "super can revoke invitation", session: "super", method: http.MethodDelete, path: "/api/user-invitations/019535d9-3df7-79fb-b466-fa907fa17f97", origin: true, wantStatus: http.StatusNoContent, touches: true, writes: true},
		{name: "invitations manager can revoke invitation", session: "invitations-manager", method: http.MethodDelete, path: "/api/user-invitations/019535d9-3df7-79fb-b466-fa907fa17f97", origin: true, wantStatus: http.StatusNoContent, touches: true, writes: true},
		{name: "roles reader cannot revoke invitation", session: "roles", method: http.MethodDelete, path: "/api/user-invitations/019535d9-3df7-79fb-b466-fa907fa17f97", origin: true, wantStatus: http.StatusForbidden},
		{name: "invitations reader cannot revoke invitation", session: "invitations-reader", method: http.MethodDelete, path: "/api/user-invitations/019535d9-3df7-79fb-b466-fa907fa17f97", origin: true, wantStatus: http.StatusForbidden},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			before := [10]int{store.createRoleCalls, store.replaceRoleCalls, store.deleteRoleCalls, store.listRolesCalls, store.listUsersCalls, store.replaceUserRolesCalls, store.createInvitationCalls, store.listInvitationsCalls, store.resendInvitationCalls, store.revokeInvitationCalls}
			response := matrixRequest(handler, test.session, test.method, test.path, test.body, test.origin)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, body=%s; want %d", response.Code, response.Body.String(), test.wantStatus)
			}
			if test.wantStatus == http.StatusForbidden && !strings.Contains(response.Body.String(), `"type":"/problems/forbidden"`) {
				t.Fatalf("forbidden response = %s", response.Body.String())
			}
			after := [10]int{store.createRoleCalls, store.replaceRoleCalls, store.deleteRoleCalls, store.listRolesCalls, store.listUsersCalls, store.replaceUserRolesCalls, store.createInvitationCalls, store.listInvitationsCalls, store.resendInvitationCalls, store.revokeInvitationCalls}
			changed := false
			for index := range before {
				if before[index] != after[index] {
					changed = true
					break
				}
			}
			if changed != test.touches {
				t.Fatalf("store call changed = %t, want %t; before=%v after=%v", changed, test.touches, before, after)
			}
			writesBefore := [7]int{before[0], before[1], before[2], before[5], before[6], before[8], before[9]}
			writesAfter := [7]int{after[0], after[1], after[2], after[5], after[6], after[8], after[9]}
			writesChanged := false
			for index := range writesBefore {
				if writesBefore[index] != writesAfter[index] {
					writesChanged = true
					break
				}
			}
			if writesChanged != test.writes {
				t.Fatalf("store write changed = %t, want %t; before=%v after=%v", writesChanged, test.writes, writesBefore, writesAfter)
			}
		})
	}

	if response := matrixRequest(handler, "", http.MethodGet, "/api/users", "", false); response.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAccessHTTPMutationChecksOriginBeforeAuthorizationBody(t *testing.T) {
	handler, store := newAccessHTTPMatrixFixture()
	response := matrixRequest(handler, "super", http.MethodPost, "/api/roles", `{"name":"Auditor","description":"Read access","permissions":["users.read"]}`, false)
	if response.Code != http.StatusForbidden || store.createRoleCalls != 0 {
		t.Fatalf("missing Origin response = %d, create calls = %d", response.Code, store.createRoleCalls)
	}
}

func TestInvitationManagerCannotAssignOrManageSuperAdminInvitation(t *testing.T) {
	handler, store := newAccessHTTPMatrixFixture()
	create := matrixRequest(handler, "invitations-manager", http.MethodPost, "/api/user-invitations", `{"name":"Invitee","email":"invitee@example.com","roleIds":["019535d9-3df7-79fb-b466-fa907fa17f94"]}`, true)
	if create.Code != http.StatusForbidden || !strings.Contains(create.Body.String(), `"code":"invitation_role_forbidden"`) || store.createInvitationCalls != 0 {
		t.Fatalf("create privileged invitation status = %d, calls = %d; body=%s", create.Code, store.createInvitationCalls, create.Body.String())
	}
	store.invitationRoleID = matrixSystemRole
	beforeResend, beforeRevoke := store.resendInvitationCalls, store.revokeInvitationCalls
	resend := matrixRequest(handler, "invitations-manager", http.MethodPost, "/api/user-invitations/019535d9-3df7-79fb-b466-fa907fa17f97/resend", "", true)
	if resend.Code != http.StatusForbidden || !strings.Contains(resend.Body.String(), `"code":"invitation_not_manageable"`) || store.resendInvitationCalls != beforeResend {
		t.Fatalf("resend privileged invitation status = %d, calls = %d; body=%s", resend.Code, store.resendInvitationCalls, resend.Body.String())
	}
	revoke := matrixRequest(handler, "invitations-manager", http.MethodDelete, "/api/user-invitations/019535d9-3df7-79fb-b466-fa907fa17f97", "", true)
	if revoke.Code != http.StatusForbidden || !strings.Contains(revoke.Body.String(), `"code":"invitation_not_manageable"`) || store.revokeInvitationCalls != beforeRevoke {
		t.Fatalf("revoke privileged invitation status = %d, calls = %d; body=%s", revoke.Code, store.revokeInvitationCalls, revoke.Body.String())
	}
}

func TestAccessHTTPMatrixFixtureRejectsUnknownPrincipal(t *testing.T) {
	store := &matrixAccessStore{roles: []domain.Role{{ID: matrixSystemRole, Name: "Super Admin", SystemKey: "super_admin", Revision: 1}}}
	principals := &matrixPrincipalStore{byUser: map[string]domain.Principal{}}
	auth := &matrixPrincipalAuth{bySession: map[string]domain.Principal{"unknown": {User: domain.User{ID: matrixUsersID}}}}
	access := application.NewAccessManagement(store, principals, domain.DefaultPermissionCatalog())
	handler := NewHandlerWithAccess(&setupFake{status: application.SetupComplete}, auth, testConfig(), nil, access, nil)
	response := matrixRequest(handler, "unknown", http.MethodGet, "/api/users", "", false)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unknown principal status = %d, body=%s", response.Code, response.Body.String())
	}
}
