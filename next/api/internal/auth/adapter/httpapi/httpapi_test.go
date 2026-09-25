package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	"github.com/jonathanhu237/bind-plane/next/api/internal/config"
)

type setupFake struct {
	status application.SetupStatus
	err    error
	input  application.SetupInput
}

func (f *setupFake) Status(context.Context) (application.SetupStatus, error) { return f.status, f.err }

func (f *setupFake) Complete(_ context.Context, input application.SetupInput) (domain.User, error) {
	f.input = input
	return domain.User{ID: "019535d9-3df7-79fb-b466-fa907fa17f9e", Name: input.Name, Email: input.Email}, nil
}

type authFake struct {
	user domain.User
	err  error
}

func (f *authFake) Login(context.Context, application.LoginInput) (domain.User, string, error) {
	if f.err != nil {
		return domain.User{}, "", f.err
	}
	return f.user, strings.Repeat("a", 43), nil
}
func (f *authFake) Current(context.Context, string) (domain.User, error) { return f.user, f.err }
func (f *authFake) Logout(context.Context, string) error                 { return f.err }

type recoveryFake struct {
	requestCalls  int
	completeCalls int
	requestErr    error
	completeErr   error
}

type invitationAcceptanceFake struct {
	calls int
	err   error
}

func (f *invitationAcceptanceFake) Complete(context.Context, string, string) error {
	f.calls++
	return f.err
}

func (f *recoveryFake) Request(context.Context, application.PasswordResetRequestInput) error {
	f.requestCalls++
	return f.requestErr
}

func (f *recoveryFake) Complete(context.Context, application.PasswordResetCompleteInput) error {
	f.completeCalls++
	return f.completeErr
}

func testConfig() config.Config {
	return config.Config{Origin: "http://localhost:5173", CookieName: "temvia_session"}
}

func request(handler http.Handler, method, path, body string, origin bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin {
		req.Header.Set("Origin", "http://localhost:5173")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestMailTaskDeleteRouteAllowsMethodPreflight(t *testing.T) {
	expected, ok := expectedMethods("/api/mail-tasks/019535d9-3df7-79fb-b466-fa907fa17f9e")
	if !ok || !methodAllowed(expected, http.MethodDelete) {
		t.Fatalf("mail-task item methods = %q, ok=%v; DELETE must be allowed", expected, ok)
	}
	expected, ok = expectedMethods("/api/settings/email/test/019535d9-3df7-79fb-b466-fa907fa17f9e")
	if !ok || !methodAllowed(expected, http.MethodGet) || methodAllowed(expected, http.MethodPost) {
		t.Fatalf("test-task status methods = %q, ok=%v; only GET must be allowed", expected, ok)
	}
}

func TestStrictJSONAndOrigin(t *testing.T) {
	setup := &setupFake{status: application.SetupRequired}
	handler := NewHandler(setup, &authFake{}, testConfig())
	if got := request(handler, http.MethodPost, "/api/setup", `{"token":"x","name":"Ada","email":"a@example.com","password":"long password value"}`, false).Code; got != http.StatusForbidden {
		t.Fatalf("missing Origin status = %d", got)
	}
	for _, body := range []string{
		`{"token":"x","name":"Ada","email":"a@example.com","password":"long password value","extra":true}`,
		`{"token":"x","token":"y","name":"Ada","email":"a@example.com","password":"long password value"}`,
		`[{"token":"x"}]`,
		`{"token":"x"}{}`,
	} {
		if got := request(handler, http.MethodPost, "/api/setup", body, true).Code; got != http.StatusBadRequest {
			t.Errorf("body %s status = %d, want 400", body, got)
		}
	}
	if got := request(handler, http.MethodPost, "/api/setup", `{"token":123,"name":"Ada","email":"a@example.com","password":"long password value"}`, true).Code; got != http.StatusForbidden {
		t.Fatalf("wrong setup token type status = %d, want 403", got)
	}
	multipleOrigin := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{"token":"x","name":"Ada","email":"a@example.com","password":"long password value"}`))
	multipleOrigin.Header.Set("Content-Type", "application/json")
	multipleOrigin.Header.Add("Origin", "http://localhost:5173")
	multipleOrigin.Header.Add("Origin", "http://evil.example")
	multipleOriginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(multipleOriginRecorder, multipleOrigin)
	if multipleOriginRecorder.Code != http.StatusForbidden {
		t.Fatalf("multiple Origin headers status = %d, want 403", multipleOriginRecorder.Code)
	}
}

func TestBodyAndMediaTypeBoundaries(t *testing.T) {
	handler := NewHandler(&setupFake{status: application.SetupRequired}, &authFake{}, testConfig())
	unsupported := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"a@example.com","password":"long password value"}`))
	unsupported.Header.Set("Origin", "http://localhost:5173")
	unsupported.Header.Set("Content-Type", "application/json; profile=unexpected")
	unsupportedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unsupportedRecorder, unsupported)
	if unsupportedRecorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("unknown media parameter status = %d, want 415", unsupportedRecorder.Code)
	}
	multipleContentType := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"a@example.com","password":"long password value"}`))
	multipleContentType.Header.Set("Origin", "http://localhost:5173")
	multipleContentType.Header.Add("Content-Type", "application/json")
	multipleContentType.Header.Add("Content-Type", "application/json")
	multipleContentTypeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(multipleContentTypeRecorder, multipleContentType)
	if multipleContentTypeRecorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("multiple Content-Type headers status = %d, want 415", multipleContentTypeRecorder.Code)
	}

	invalidUTF8 := `{"email":"a@example.com","password":"12345678901234` + string([]byte{0xff}) + `"}`
	if got := request(handler, http.MethodPost, "/api/auth/login", invalidUTF8, true).Code; got != http.StatusBadRequest {
		t.Fatalf("invalid UTF-8 status = %d, want 400", got)
	}

	oversized := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(strings.Repeat("x", maxJSONBody+1)))
	oversized.Header.Set("Origin", "http://localhost:5173")
	oversizedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(oversizedRecorder, oversized)
	if oversizedRecorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("declared oversized status = %d, want 413", oversizedRecorder.Code)
	}
}

func TestHTTPContracts(t *testing.T) {
	user := domain.User{ID: "019535d9-3df7-79fb-b466-fa907fa17f9e", Name: "Ada", Email: "ada@example.com"}
	setup := &setupFake{status: application.SetupRequired}
	auth := &authFake{user: user}
	handler := NewHandler(setup, auth, testConfig())
	status := request(handler, http.MethodGet, "/api/setup/status", "", false)
	if status.Code != http.StatusOK || status.Header().Get("Cache-Control") != "no-store" || status.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("status response = %d, headers %#v", status.Code, status.Header())
	}
	login := request(handler, http.MethodPost, "/api/auth/login", `{"email":"ada@example.com","password":"long password value"}`, true)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, body %s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "temvia_session" || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Secure {
		t.Fatalf("login cookies = %#v", cookies)
	}
	setupResult := request(handler, http.MethodPost, "/api/setup", `{"token":"x","name":"Ada","email":"ada@example.com","password":"long password value"}`, true)
	if setupResult.Code != http.StatusCreated {
		t.Fatalf("setup status = %d", setupResult.Code)
	}
	method := request(handler, http.MethodGet, "/api/auth/login", "", false)
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("method response = %d, headers %#v", method.Code, method.Header())
	}
}

func TestProblemMappings(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want int
	}{
		{"invalid credentials", application.ErrInvalidCredentials, http.StatusUnauthorized},
		{"service unavailable", application.ErrDependencyUnavailable, http.StatusServiceUnavailable},
		{"rate limited", application.ErrRateLimited, http.StatusTooManyRequests},
		{"setup complete", application.ErrSetupComplete, http.StatusConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeApplicationError(response, test.err)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
			if response.Header().Get("Content-Type") != "application/problem+json" {
				t.Fatal("missing Problem Details content type")
			}
		})
	}
	if errors.Is(errInvalidJSON, errBodyTooLarge) {
		t.Fatal("sentinel errors overlap")
	}
}

func TestPasswordRecoveryHTTPContracts(t *testing.T) {
	recovery := &recoveryFake{}
	handler := NewHandler(&setupFake{status: application.SetupRequired}, &authFake{}, testConfig(), recovery)
	requestResponse := request(handler, http.MethodPost, "/api/auth/password-reset/request", `{"email":"ada@example.com"}`, true)
	if requestResponse.Code != http.StatusAccepted || requestResponse.Header().Get("Cache-Control") != "no-store" || requestResponse.Header().Get("Content-Type") != "application/json; charset=utf-8" || requestResponse.Body.String() != "{\"status\":\"accepted\"}\n" {
		t.Fatalf("request response = %d, headers %#v, body %q", requestResponse.Code, requestResponse.Header(), requestResponse.Body.String())
	}
	if recovery.requestCalls != 1 {
		t.Fatalf("request calls = %d", recovery.requestCalls)
	}
	complete := request(handler, http.MethodPost, "/api/auth/password-reset/complete", `{"token":123,"password":"Aa1!xxxx"}`, true)
	if complete.Code != http.StatusForbidden || recovery.completeCalls != 0 {
		t.Fatalf("malformed token response = %d, calls=%d", complete.Code, recovery.completeCalls)
	}
	complete = request(handler, http.MethodPost, "/api/auth/password-reset/complete", `{"token":"v1.AAAAAAAAAAAAAAAAAAAAAA.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","password":"Aa1!xxxx"}`, true)
	if complete.Code != http.StatusNoContent {
		t.Fatalf("complete response = %d, body=%q", complete.Code, complete.Body.String())
	}
	cookies := complete.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "temvia_session" || cookies[0].MaxAge >= 0 || cookies[0].Value != "" {
		t.Fatalf("completion cookie = %#v", cookies)
	}
	if recovery.completeCalls != 1 {
		t.Fatalf("completion calls = %d", recovery.completeCalls)
	}
	missingOrigin := request(handler, http.MethodPost, "/api/auth/password-reset/request", `{"email":"ada@example.com"}`, false)
	if missingOrigin.Code != http.StatusForbidden || recovery.requestCalls != 1 {
		t.Fatalf("missing Origin response = %d, calls=%d", missingOrigin.Code, recovery.requestCalls)
	}
	method := request(handler, http.MethodGet, "/api/auth/password-reset/request", "", false)
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("recovery method response = %d, headers=%#v", method.Code, method.Header())
	}
}

func TestPasswordRecoveryHTTPErrorMapping(t *testing.T) {
	recovery := &recoveryFake{requestErr: application.ErrRateLimited, completeErr: application.ErrInvalidPasswordResetToken}
	handler := NewHandler(&setupFake{status: application.SetupRequired}, &authFake{}, testConfig(), recovery)
	limited := request(handler, http.MethodPost, "/api/auth/password-reset/request", `{"email":"ada@example.com"}`, true)
	if limited.Code != http.StatusTooManyRequests || !strings.Contains(limited.Body.String(), `"type":"/problems/rate-limited"`) {
		t.Fatalf("rate-limit response = %d, body=%s", limited.Code, limited.Body.String())
	}
	invalid := request(handler, http.MethodPost, "/api/auth/password-reset/complete", `{"token":"v1.AAAAAAAAAAAAAAAAAAAAAA.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","password":"Aa1!xxxx"}`, true)
	if invalid.Code != http.StatusForbidden || !strings.Contains(invalid.Body.String(), `"type":"/problems/invalid-password-reset-token"`) {
		t.Fatalf("invalid-token response = %d, body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestPasswordResetDependencyFailureDoesNotClearSessionCookie(t *testing.T) {
	recovery := &recoveryFake{completeErr: application.ErrDependencyUnavailable}
	handler := NewHandler(&setupFake{status: application.SetupComplete}, &authFake{}, testConfig(), recovery)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/password-reset/complete", strings.NewReader(`{"token":"v1.AAAAAAAAAAAAAAAAAAAAAA.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","password":"Aa1!xxxx"}`))
	req.Header.Set("Origin", testConfig().Origin)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: testConfig().CookieName, Value: strings.Repeat("s", 43)})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("dependency failure status = %d, body=%s", response.Code, response.Body.String())
	}
	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("dependency failure changed session cookie: %#v", cookies)
	}
}

func TestInvitationAcceptanceHTTPContracts(t *testing.T) {
	acceptance := &invitationAcceptanceFake{}
	handler := NewHandlerWithAccess(&setupFake{status: application.SetupRequired}, &authFake{}, testConfig(), nil, nil, acceptance)
	response := request(handler, http.MethodPost, "/api/auth/invitations/accept", `{"token":"token","password":"Aa1!xxxx"}`, true)
	if response.Code != http.StatusNoContent || acceptance.calls != 1 || len(response.Result().Cookies()) != 1 {
		t.Fatalf("acceptance response = %d, calls=%d, cookies=%#v", response.Code, acceptance.calls, response.Result().Cookies())
	}
	unknownLocale := request(handler, http.MethodPost, "/api/auth/invitations/accept", `{"token":"token","password":"Aa1!xxxx","locale":"fr"}`, true)
	if unknownLocale.Code != http.StatusBadRequest || acceptance.calls != 1 {
		t.Fatalf("unknown locale response = %d, calls=%d", unknownLocale.Code, acceptance.calls)
	}
	invalidTokenType := request(handler, http.MethodPost, "/api/auth/invitations/accept", `{"token":123,"password":"Aa1!xxxx"}`, true)
	if invalidTokenType.Code != http.StatusForbidden || acceptance.calls != 1 || !strings.Contains(invalidTokenType.Body.String(), `"type":"/problems/invalid-invitation"`) {
		t.Fatalf("invalid token type response = %d, calls=%d, body=%s", invalidTokenType.Code, acceptance.calls, invalidTokenType.Body.String())
	}
	missingOrigin := request(handler, http.MethodPost, "/api/auth/invitations/accept", `{"token":"token","password":"Aa1!xxxx"}`, false)
	if missingOrigin.Code != http.StatusForbidden || acceptance.calls != 1 {
		t.Fatalf("missing Origin response = %d, calls=%d", missingOrigin.Code, acceptance.calls)
	}
}
