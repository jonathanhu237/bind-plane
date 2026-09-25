package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type settingsHTTPAuth struct {
	principal domain.Principal
}

func (a *settingsHTTPAuth) Login(context.Context, application.LoginInput) (domain.User, string, error) {
	return a.principal.User, "", nil
}

func (a *settingsHTTPAuth) Current(context.Context, string) (domain.User, error) {
	return a.principal.User, nil
}

func (*settingsHTTPAuth) Logout(context.Context, string) error { return nil }

func (a *settingsHTTPAuth) LoginWithPrincipal(context.Context, application.LoginInput) (domain.Principal, string, error) {
	return a.principal, "", nil
}

func (a *settingsHTTPAuth) CurrentPrincipal(context.Context, string) (domain.Principal, error) {
	return a.principal, nil
}

type settingsHTTPFake struct {
	recipient string
	calls     int
}

type settingsHTTPNoTaskFake struct{}

func (*settingsHTTPNoTaskFake) GetEmailSettings(context.Context) (application.EmailSettingsView, error) {
	return application.EmailSettingsView{}, nil
}
func (*settingsHTTPNoTaskFake) SaveEmailSettings(context.Context, application.EmailSettingsInput) (application.EmailSettingsView, error) {
	return application.EmailSettingsView{}, nil
}
func (*settingsHTTPNoTaskFake) OperationalWarnings(context.Context) ([]application.OperationalWarning, error) {
	return nil, nil
}

func (*settingsHTTPFake) GetEmailSettings(context.Context) (application.EmailSettingsView, error) {
	return application.EmailSettingsView{}, nil
}

func (*settingsHTTPFake) SaveEmailSettings(context.Context, application.EmailSettingsInput) (application.EmailSettingsView, error) {
	return application.EmailSettingsView{}, nil
}

func (f *settingsHTTPFake) TestEmailSettingsTask(_ context.Context, _ string, _ application.EmailSettingsInput, recipient string) (application.MailTask, error) {
	f.calls++
	f.recipient = recipient
	return application.MailTask{ID: "00000000-0000-4000-8000-000000000402", Kind: application.MailTest, RecipientEmail: recipient, Status: application.MailTaskStatusQueued, Round: 1}, nil
}

func (*settingsHTTPFake) OperationalWarnings(context.Context) ([]application.OperationalWarning, error) {
	return nil, nil
}

func TestEmailSettingsTestRequiresDurableTaskService(t *testing.T) {
	settings := &settingsHTTPNoTaskFake{}
	auth := &settingsHTTPAuth{principal: domain.Principal{User: domain.User{ID: "admin", Email: "admin@example.com"}, SuperAdmin: true}}
	handler := NewHandlerWithAccess(&setupFake{status: application.SetupComplete}, auth, testConfig(), nil, nil, nil, settings)
	req := httptest.NewRequest(http.MethodPost, "/api/settings/email/test", strings.NewReader(`{"recipient":"real@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testConfig().Origin)
	req.AddCookie(&http.Cookie{Name: testConfig().CookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing task service status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestEmailSettingsTestForwardsRecipient(t *testing.T) {
	settings := &settingsHTTPFake{}
	auth := &settingsHTTPAuth{principal: domain.Principal{User: domain.User{ID: "admin", Email: "admin@example.com"}, SuperAdmin: true}}
	handler := NewHandlerWithAccess(&setupFake{status: application.SetupComplete}, auth, testConfig(), nil, nil, nil, settings)
	req := httptest.NewRequest(http.MethodPost, "/api/settings/email/test", strings.NewReader(`{"recipient":"real@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testConfig().Origin)
	req.AddCookie(&http.Cookie{Name: testConfig().CookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if response.Code != http.StatusAccepted || settings.calls != 1 || settings.recipient != "real@example.com" {
		t.Fatalf("response=%d calls=%d recipient=%q body=%s", response.Code, settings.calls, settings.recipient, response.Body.String())
	}
}

func TestEmailSettingsRejectsNullRetryPolicy(t *testing.T) {
	settings := &settingsHTTPFake{}
	auth := &settingsHTTPAuth{principal: domain.Principal{User: domain.User{ID: "admin", Email: "admin@example.com"}, SuperAdmin: true}}
	handler := NewHandlerWithAccess(&setupFake{status: application.SetupComplete}, auth, testConfig(), nil, nil, nil, settings)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/email", strings.NewReader(`{"autoRetryCount":null}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testConfig().Origin)
	req.AddCookie(&http.Cookie{Name: testConfig().CookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if response.Code != http.StatusUnprocessableEntity || settings.calls != 0 || !strings.Contains(response.Body.String(), "autoRetryCount") {
		t.Fatalf("response=%d calls=%d body=%s", response.Code, settings.calls, response.Body.String())
	}
}
