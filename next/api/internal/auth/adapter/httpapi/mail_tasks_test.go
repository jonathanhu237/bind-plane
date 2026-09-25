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

type mailTaskHTTPAuth struct{ principal domain.Principal }

func (a *mailTaskHTTPAuth) Login(context.Context, application.LoginInput) (domain.User, string, error) {
	return a.principal.User, "", nil
}
func (a *mailTaskHTTPAuth) Current(context.Context, string) (domain.User, error) {
	return a.principal.User, nil
}
func (a *mailTaskHTTPAuth) Logout(context.Context, string) error { return nil }
func (a *mailTaskHTTPAuth) CurrentPrincipal(context.Context, string) (domain.Principal, error) {
	return a.principal, nil
}

// mailTaskHTTPService deliberately returns a safe projection and records the
// operation input. The handler test therefore exercises route authorization,
// strict query/body parsing, and the no-content/token response boundary
// without coupling to PostgreSQL.
type mailTaskHTTPService struct {
	task      application.MailTask
	retryIDs  []string
	deleteIDs []string
}

func (s *mailTaskHTTPService) List(context.Context, string, application.MailTaskListOptions) (application.MailTaskPage, error) {
	return application.MailTaskPage{Items: []application.MailTask{s.task}}, nil
}
func (s *mailTaskHTTPService) Detail(context.Context, string, string) (application.MailTask, error) {
	return s.task, nil
}
func (s *mailTaskHTTPService) Retry(_ context.Context, _ string, id string) (application.MailTask, error) {
	s.retryIDs = append(s.retryIDs, id)
	return s.task, nil
}
func (s *mailTaskHTTPService) Delete(_ context.Context, _ string, id string) error {
	s.deleteIDs = append(s.deleteIDs, id)
	return nil
}
func (s *mailTaskHTTPService) BulkRetry(_ context.Context, _ string, ids []string) (application.MailTaskBatchResult, error) {
	s.retryIDs = append(s.retryIDs, ids...)
	return application.MailTaskBatchResult{Succeeded: len(ids)}, nil
}
func (s *mailTaskHTTPService) BulkDelete(_ context.Context, _ string, ids []string) (application.MailTaskBatchResult, error) {
	s.deleteIDs = append(s.deleteIDs, ids...)
	return application.MailTaskBatchResult{Succeeded: len(ids)}, nil
}

func TestMailTasksHTTPBoundary(t *testing.T) {
	const id = "00000000-0000-4000-8000-000000000301"
	service := &mailTaskHTTPService{task: application.MailTask{ID: id, Kind: application.MailTest, RecipientEmail: "operator@example.com", Locale: domain.LocaleEnglish, Status: application.MailTaskStatusFailed, CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), AvailableAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), AttemptCount: 1, Round: 1, RoundAttemptCount: 1, LastErrorCode: "temporary"}}
	auth := &mailTaskHTTPAuth{principal: domain.Principal{User: domain.User{ID: "00000000-0000-4000-8000-000000000001", Name: "Admin", Email: "admin@example.com"}, SuperAdmin: true}}
	handler := NewHandlerWithMailTasks(&setupFake{status: application.SetupComplete}, auth, testConfig(), nil, service)
	request := httptest.NewRequest(http.MethodGet, "/api/mail-tasks?status=failed&limit=10", nil)
	request.AddCookie(&http.Cookie{Name: testConfig().CookieName, Value: "session"})
	request.Header.Set("Origin", testConfig().Origin)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"recipientEmail":"operator@example.com"`) {
		t.Fatalf("list response=%d body=%s", response.Code, response.Body.String())
	}
	for _, forbidden := range []string{"material", "ciphertext", "token", "password", "smtp"} {
		if strings.Contains(strings.ToLower(response.Body.String()), forbidden) {
			t.Fatalf("list response leaked %q: %s", forbidden, response.Body.String())
		}
	}

	request = httptest.NewRequest(http.MethodPost, "/api/mail-tasks/bulk-retry", strings.NewReader(`{"ids":["`+id+`"]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", testConfig().Origin)
	request.AddCookie(&http.Cookie{Name: testConfig().CookieName, Value: "session"})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || len(service.retryIDs) != 1 || service.retryIDs[0] != id {
		t.Fatalf("bulk retry response=%d ids=%v body=%s", response.Code, service.retryIDs, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/mail-tasks/bulk-retry", strings.NewReader(`{"ids":["`+id+`"],"unexpected":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", testConfig().Origin)
	request.AddCookie(&http.Cookie{Name: testConfig().CookieName, Value: "session"})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("strict bulk body status=%d body=%s", response.Code, response.Body.String())
	}
}
