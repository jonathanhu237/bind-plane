package httpapi

import (
	"context"
	"errors"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	"net/http"
	"net/http/httptest"
	"testing"
)

type onlineAuthFake struct {
	authFake
	kicks        int
	kickErr      error
	noTouchCalls int
}

func (f *onlineAuthFake) OnlineUsers(context.Context, string) ([]application.OnlineUser, error) {
	return []application.OnlineUser{}, nil
}
func (f *onlineAuthFake) KickUser(context.Context, string, string) error { f.kicks++; return f.kickErr }

func (f *onlineAuthFake) CurrentPrincipalNoTouch(context.Context, string) (domain.Principal, error) {
	f.noTouchCalls++
	if f.err != nil {
		return domain.Principal{}, f.err
	}
	return domain.Principal{User: f.user}, nil
}

func TestOnlineHTTPOriginAuthenticationAndAudit(t *testing.T) {
	auth := &onlineAuthFake{authFake: authFake{user: domain.User{ID: "019535d9-3df7-79fb-b466-fa907fa17f95"}}}
	logs := &auditRecorderFake{}
	h := newHandlerWithOperationLog(&setupFake{}, auth, testConfig(), nil, nil, nil, nil, logs)
	request := func(handler http.Handler, method, path, body string, origin bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.AddCookie(&http.Cookie{Name: "temvia_session", Value: "test"})
		if origin {
			req.Header.Set("Origin", "http://localhost:5173")
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder
	}
	path := "/api/online-users/019535d9-3df7-79fb-b466-fa907fa17f96/kick"
	if got := request(h, http.MethodPost, path, "", false).Code; got != 403 || auth.kicks != 0 {
		t.Fatalf("origin=%d kicks=%d", got, auth.kicks)
	}
	auth.err = application.ErrUnauthenticated
	if got := request(h, http.MethodPost, path, "", true).Code; got != 401 || auth.kicks != 0 {
		t.Fatalf("authentication=%d kicks=%d", got, auth.kicks)
	}
	auth.err = nil
	if got := request(h, http.MethodPost, path, "", true).Code; got != 204 || auth.kicks != 1 {
		t.Fatalf("kick=%d kicks=%d", got, auth.kicks)
	}
	if last := logs.inputs[len(logs.inputs)-1]; last.Action != "users.sessions.revoke" || last.Result != "success" {
		t.Fatalf("audit=%+v", last)
	}
	auth.kickErr = application.ErrForbidden
	if got := request(h, http.MethodPost, path, "", true).Code; got != 403 {
		t.Fatalf("permission=%d", got)
	}
	if last := logs.inputs[len(logs.inputs)-1]; last.Result != "failure" {
		t.Fatalf("failure audit=%+v", last)
	}
	auth.err = errors.New("offline")
	if got := request(h, http.MethodGet, "/api/online-users", "", false).Code; got == 200 {
		t.Fatal("failed authentication exposed list")
	}
	auth.err = nil
	if got := request(h, http.MethodGet, "/api/online-users", "", false).Code; got != http.StatusOK || auth.noTouchCalls == 0 {
		t.Fatalf("online list status=%d no-touch calls=%d", got, auth.noTouchCalls)
	}
	if got := request(h, http.MethodGet, "/api/auth/session-status", "", false).Code; got != http.StatusOK || auth.noTouchCalls < 2 {
		t.Fatalf("session status=%d no-touch calls=%d", got, auth.noTouchCalls)
	}
	if got := request(h, http.MethodPost, "/api/auth/session-status", "", false).Code; got != http.StatusMethodNotAllowed {
		t.Fatalf("session status method=%d", got)
	}
}
