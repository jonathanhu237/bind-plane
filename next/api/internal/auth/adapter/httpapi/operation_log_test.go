package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	"github.com/jonathanhu237/bind-plane/next/api/internal/config"
)

type auditRecorderFake struct {
	inputs []application.OperationLogInput
	err    error
}

func (f *auditRecorderFake) Record(_ context.Context, input application.OperationLogInput) error {
	f.inputs = append(f.inputs, input)
	return f.err
}
func (*auditRecorderFake) List(context.Context, application.OperationLogListOptions) (application.OperationLogPage, error) {
	return application.OperationLogPage{}, nil
}
func (*auditRecorderFake) Detail(context.Context, string) (application.OperationLog, error) {
	return application.OperationLog{}, nil
}
func (*auditRecorderFake) RecordingStatus(time.Time) application.OperationLogRecordingStatus {
	return application.OperationLogRecordingStatus{State: application.OperationLogStatusUnknown}
}
func (*auditRecorderFake) Retention(context.Context) (application.OperationLogRetention, error) {
	return application.OperationLogRetention{Days: 180, Revision: 1}, nil
}
func (*auditRecorderFake) SaveRetention(context.Context, int64, int) (application.OperationLogRetention, error) {
	return application.OperationLogRetention{Days: 180, Revision: 2}, nil
}

type operationAccessFake struct {
	createRoleErr          error
	replaceUserRolesResult domain.AccessUser
}

func (*operationAccessFake) Roles(context.Context, string) (application.RolePage, error) {
	return application.RolePage{}, nil
}
func (*operationAccessFake) RoleOptions(context.Context, string) ([]application.RoleOption, error) {
	return nil, nil
}
func (*operationAccessFake) Role(context.Context, string, string) (domain.Role, error) {
	return domain.Role{}, nil
}
func (f *operationAccessFake) CreateRole(context.Context, string, application.RoleMutationInput) (domain.Role, error) {
	if f.createRoleErr != nil {
		return domain.Role{}, f.createRoleErr
	}
	return domain.Role{ID: "019535d9-3df7-79fb-b466-fa907fa17f98", Name: "Auditor", Revision: 1}, nil
}
func (*operationAccessFake) ReplaceRole(context.Context, string, string, application.RoleMutationInput) (domain.Role, error) {
	return domain.Role{}, nil
}
func (*operationAccessFake) DeleteRole(context.Context, string, string) error { return nil }
func (*operationAccessFake) Users(context.Context, string, string, int) (application.UserPage, error) {
	return application.UserPage{}, nil
}
func (f *operationAccessFake) ReplaceUserRoles(context.Context, string, string, application.AssignmentInput) (domain.AccessUser, error) {
	return f.replaceUserRolesResult, nil
}
func (*operationAccessFake) CreateInvitation(context.Context, string, application.InvitationInput) (domain.Invitation, error) {
	return domain.Invitation{}, nil
}
func (*operationAccessFake) Invitations(context.Context, string, string, int) (application.InvitationPage, error) {
	return application.InvitationPage{}, nil
}
func (*operationAccessFake) ResendInvitation(context.Context, string, string) (domain.Invitation, error) {
	return domain.Invitation{}, nil
}
func (*operationAccessFake) RevokeInvitation(context.Context, string, string) error { return nil }

func auditRequest(handler http.Handler, method, path, body string, origin bool, session string) *httptest.ResponseRecorder {
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
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func TestOperationWriteBoundaryRecordsEachEarlyResultOnce(t *testing.T) {
	user := domain.User{ID: "019535d9-3df7-79fb-b466-fa907fa17f9e", Name: "Ada", Email: "ada@example.com"}
	recorder := &auditRecorderFake{}
	auth := &authFake{user: user}
	access := &operationAccessFake{}
	handler := NewHandlerWithAccessAndOperationLog(&setupFake{status: application.SetupComplete}, auth, testConfig(), nil, access, nil, nil, recorder)

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		origin     bool
		session    string
		serviceErr error
		wantStatus int
		wantAction string
		wantResult string
	}{
		{name: "origin forbidden", method: http.MethodPost, path: "/api/roles", body: `{}`, wantStatus: http.StatusForbidden, wantAction: "roles.create", wantResult: application.OperationLogFailure},
		{name: "unauthenticated", method: http.MethodPost, path: "/api/roles", body: `{}`, origin: true, wantStatus: http.StatusUnauthorized, wantAction: "roles.create", wantResult: application.OperationLogFailure},
		{name: "malformed JSON", method: http.MethodPost, path: "/api/roles", body: `{"name":`, origin: true, session: "session", wantStatus: http.StatusBadRequest, wantAction: "roles.create", wantResult: application.OperationLogFailure},
		{name: "validation failure", method: http.MethodPost, path: "/api/roles", body: `{"name":"x","description":"","permissions":[]}`, origin: true, session: "session", serviceErr: &domain.ValidationErrors{Items: []domain.FieldError{{Field: "name", Code: "invalid"}}}, wantStatus: http.StatusUnprocessableEntity, wantAction: "roles.create", wantResult: application.OperationLogFailure},
		{name: "service failure", method: http.MethodPost, path: "/api/roles", body: `{"name":"x","description":"","permissions":[]}`, origin: true, session: "session", serviceErr: application.ErrDependencyUnavailable, wantStatus: http.StatusServiceUnavailable, wantAction: "roles.create", wantResult: application.OperationLogFailure},
		{name: "success", method: http.MethodPost, path: "/api/roles", body: `{"name":"x","description":"","permissions":[]}`, origin: true, session: "session", wantStatus: http.StatusCreated, wantAction: "roles.create", wantResult: application.OperationLogSuccess},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			recorder.inputs = nil
			access.createRoleErr = test.serviceErr
			response := auditRequest(handler, test.method, test.path, test.body, test.origin, test.session)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, body=%s; want %d", response.Code, response.Body.String(), test.wantStatus)
			}
			if len(recorder.inputs) != 1 {
				t.Fatalf("record count = %d, want one", len(recorder.inputs))
			}
			input := recorder.inputs[0]
			if input.Action != test.wantAction || input.Result != test.wantResult {
				t.Fatalf("record = %#v", input)
			}
			if (test.session == "" && input.ActorKind != "unverified") || (test.session != "" && (input.ActorKind != "authenticated" || input.ActorID == "")) {
				t.Fatalf("actor kind = %q for session %q", input.ActorKind, test.session)
			}
		})
	}
}

func TestOperationRecordFailureDoesNotChangeBusinessResponse(t *testing.T) {
	auth := &authFake{err: application.ErrInvalidCredentials}
	recorder := &auditRecorderFake{err: errors.New("recorder unavailable")}
	handler := NewHandlerWithAccessAndOperationLog(&setupFake{status: application.SetupComplete}, auth, testConfig(), nil, nil, nil, nil, recorder)
	response := auditRequest(handler, http.MethodPost, "/api/auth/login", `{"email":"ada@example.com","password":"bad"}`, true, "")
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"type":"/problems/invalid-credentials"`) {
		t.Fatalf("business response changed after recorder error: %d %s", response.Code, response.Body.String())
	}
	if len(recorder.inputs) != 1 {
		t.Fatalf("record count = %d, want one", len(recorder.inputs))
	}
}

func TestOperationSnapshotsKeepLogoutIdentityAndSetupTarget(t *testing.T) {
	user := domain.User{ID: "019535d9-3df7-79fb-b466-fa907fa17f9e", Name: "Ada", Email: "ada@example.com"}
	recorder := &auditRecorderFake{}
	auth := &authFake{user: user}
	handler := NewHandlerWithAccessAndOperationLog(&setupFake{status: application.SetupRequired}, auth, testConfig(), nil, nil, nil, nil, recorder)
	response := auditRequest(handler, http.MethodPost, "/api/auth/logout", "", true, "session")
	if response.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d", response.Code)
	}
	if len(recorder.inputs) != 1 || recorder.inputs[0].ActorName != user.Name || recorder.inputs[0].ActorEmail != user.Email {
		t.Fatalf("logout actor snapshot = %#v", recorder.inputs)
	}
	recorder.inputs = nil
	response = auditRequest(handler, http.MethodPost, "/api/setup", `{"token":"x","name":"Grace","email":"grace@example.com","password":"Aa1!xxxx"}`, true, "")
	if response.Code != http.StatusCreated || len(recorder.inputs) != 1 {
		t.Fatalf("setup response/record = %d %#v", response.Code, recorder.inputs)
	}
	if target, ok := recorder.inputs[0].Details["target"].(map[string]any); !ok || target["email"] != "grace@example.com" {
		t.Fatalf("setup target snapshot = %#v", recorder.inputs[0].Details)
	}
}

type invalidatingPrincipalAuthFake struct {
	principal   domain.Principal
	currentCall int
}

func (f *invalidatingPrincipalAuthFake) Login(context.Context, application.LoginInput) (domain.User, string, error) {
	return f.principal.User, "session", nil
}

func (f *invalidatingPrincipalAuthFake) LoginWithPrincipal(context.Context, application.LoginInput) (domain.Principal, string, error) {
	return f.principal, "session", nil
}

func (f *invalidatingPrincipalAuthFake) Current(context.Context, string) (domain.User, error) {
	return f.principal.User, nil
}

func (f *invalidatingPrincipalAuthFake) CurrentPrincipal(context.Context, string) (domain.Principal, error) {
	f.currentCall++
	if f.currentCall > 1 {
		return domain.Principal{}, application.ErrUnauthenticated
	}
	return f.principal, nil
}

func (f *invalidatingPrincipalAuthFake) Logout(context.Context, string) error { return nil }

func TestManagementOperationKeepsVerifiedActorSnapshotAfterSessionInvalidation(t *testing.T) {
	user := domain.User{ID: "019535d9-3df7-79fb-b466-fa907fa17f9e", Name: "Ada", Email: "ada@example.com"}
	auth := &invalidatingPrincipalAuthFake{principal: domain.Principal{User: user, SuperAdmin: true}}
	recorder := &auditRecorderFake{}
	role := domain.Role{ID: "019535d9-3df7-79fb-b466-fa907fa17f95", Name: "Auditor", Description: "Read account data", Permissions: []domain.PermissionKey{domain.PermissionUsersRead}, Revision: 4}
	access := &operationAccessFake{replaceUserRolesResult: domain.AccessUser{User: user, Roles: []domain.Role{role}, AuthVersion: 2}}
	handler := NewHandlerWithAccessAndOperationLog(&setupFake{status: application.SetupComplete}, auth, testConfig(), nil, access, nil, nil, recorder)

	response := auditRequest(handler, http.MethodPut, "/api/users/019535d9-3df7-79fb-b466-fa907fa17f93/roles", `{"roleIds":["019535d9-3df7-79fb-b466-fa907fa17f95"],"authVersion":1}`, true, "session")
	if response.Code != http.StatusOK {
		t.Fatalf("assignment status = %d, body=%s", response.Code, response.Body.String())
	}
	if auth.currentCall != 1 {
		t.Fatalf("CurrentPrincipal calls = %d, want one verified pre-mutation snapshot", auth.currentCall)
	}
	if len(recorder.inputs) != 1 || recorder.inputs[0].ActorName != user.Name || recorder.inputs[0].ActorEmail != user.Email {
		t.Fatalf("actor snapshot = %#v", recorder.inputs)
	}
	after, ok := recorder.inputs[0].Details["after"].(map[string]any)
	roles, rolesOK := after["roles"].([]map[string]any)
	if !ok || !rolesOK || len(roles) != 1 {
		t.Fatalf("assignment after snapshot = %#v", recorder.inputs[0].Details)
	}
}

func TestAssignmentAndInvitationSnapshotsRetainRoleDefinitions(t *testing.T) {
	role := domain.Role{ID: "019535d9-3df7-79fb-b466-fa907fa17f95", Name: "Auditor", Description: "Read account data", Permissions: []domain.PermissionKey{domain.PermissionUsersRead}, Revision: 4}
	userSnapshot := accessUserSnapshot(domain.AccessUser{User: domain.User{ID: "019535d9-3df7-79fb-b466-fa907fa17f93"}, Roles: []domain.Role{role}, AuthVersion: 8})
	invitSnapshot := invitationSnapshot(domain.Invitation{ID: "019535d9-3df7-79fb-b466-fa907fa17f97", Roles: []domain.Role{role}})

	for name, snapshot := range map[string]map[string]any{"assignment": userSnapshot, "invitation": invitSnapshot} {
		t.Run(name, func(t *testing.T) {
			roles, ok := snapshot["roles"].([]map[string]any)
			if !ok || len(roles) != 1 {
				t.Fatalf("roles snapshot = %#v", snapshot["roles"])
			}
			if roles[0]["name"] != role.Name || roles[0]["description"] != role.Description {
				t.Fatalf("role descriptor = %#v", roles[0])
			}
			permissions, ok := roles[0]["permissions"].([]string)
			if !ok || len(permissions) != 1 || permissions[0] != string(domain.PermissionUsersRead) {
				t.Fatalf("role permissions = %#v", roles[0]["permissions"])
			}
		})
	}
}

func TestRequestSourceIPTrustsOnlyImmediateProxyAndStripsTrustedHops(t *testing.T) {
	handler := &Handler{cfg: config.Config{TrustedProxyCIDRs: []string{"10.0.0.0/8"}}}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.2:1234"
	request.Header.Set("X-Forwarded-For", "198.51.100.9, 10.0.0.3, 203.0.113.8")
	if got := handler.requestSourceIP(request); got != "203.0.113.8" {
		t.Fatalf("right-to-left source = %q, want 203.0.113.8", got)
	}
	request.RemoteAddr = "198.51.100.2:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := handler.requestSourceIP(request); got != "198.51.100.2" {
		t.Fatalf("untrusted peer source = %q, want peer", got)
	}
}

func TestRequestSourceIPRejectsAmbiguousForwardingWithoutLosingTrustedClient(t *testing.T) {
	handler := &Handler{cfg: config.Config{TrustedProxyCIDRs: []string{"10.0.0.0/8", "2001:db8:100::/48"}}}

	tests := []struct {
		name       string
		remoteAddr string
		forwarded  []string
		realIP     []string
		want       string
	}{
		{
			name:       "malicious prefix is outside trust boundary",
			remoteAddr: "10.0.0.2:1234",
			forwarded:  []string{"not-an-ip, 198.51.100.9, 10.0.0.3"},
			want:       "198.51.100.9",
		},
		{
			name:       "rightmost untrusted address is not ignored",
			remoteAddr: "10.0.0.2:1234",
			forwarded:  []string{"10.0.0.3, 203.0.113.8"},
			want:       "203.0.113.8",
		},
		{
			name:       "invalid node before trust boundary falls back",
			remoteAddr: "10.0.0.2:1234",
			forwarded:  []string{"198.51.100.9, not-an-ip, 10.0.0.3"},
			want:       "10.0.0.2",
		},
		{
			name:       "header values are traversed from application side",
			remoteAddr: "10.0.0.2:1234",
			forwarded:  []string{"not-an-ip", "198.51.100.9, 10.0.0.3"},
			want:       "198.51.100.9",
		},
		{
			name:       "invalid closest header value falls back",
			remoteAddr: "10.0.0.2:1234",
			forwarded:  []string{"198.51.100.9", "not-an-ip"},
			want:       "10.0.0.2",
		},
		{
			name:       "only trusted forwarded hops fall back",
			remoteAddr: "10.0.0.2:1234",
			forwarded:  []string{"10.0.0.3, 10.0.0.4"},
			want:       "10.0.0.2",
		},
		{
			name:       "x-real-ip is single value fallback",
			remoteAddr: "10.0.0.2:1234",
			realIP:     []string{"::ffff:198.51.100.11"},
			want:       "198.51.100.11",
		},
		{
			name:       "multiple x-real-ip values fall back",
			remoteAddr: "10.0.0.2:1234",
			realIP:     []string{"198.51.100.11", "198.51.100.12"},
			want:       "10.0.0.2",
		},
		{
			name:       "trusted x-real-ip cannot replace peer",
			remoteAddr: "10.0.0.2:1234",
			realIP:     []string{"10.0.0.3"},
			want:       "10.0.0.2",
		},
		{
			name:       "ipv6 is canonicalized",
			remoteAddr: "2001:db8:100::2:1234",
			forwarded:  []string{"2001:db8:200::9, 2001:db8:100::3"},
			want:       "2001:db8:200::9",
		},
		{
			name:       "mapped ipv4 trusted peer",
			remoteAddr: "[::ffff:10.0.0.2]:1234",
			forwarded:  []string{"::ffff:198.51.100.13"},
			want:       "198.51.100.13",
		},
		{
			name:       "untrusted peer ignores all forwarding headers",
			remoteAddr: "198.51.100.2:1234",
			forwarded:  []string{"198.51.100.13"},
			realIP:     []string{"198.51.100.14"},
			want:       "198.51.100.2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.RemoteAddr = test.remoteAddr
			for _, value := range test.forwarded {
				request.Header.Add("X-Forwarded-For", value)
			}
			for _, value := range test.realIP {
				request.Header.Add("X-Real-IP", value)
			}
			if got := handler.requestSourceIP(request); got != test.want {
				t.Fatalf("source = %q, want %q", got, test.want)
			}
		})
	}
}
