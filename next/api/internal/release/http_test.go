package release

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type fakeIdentity struct {
	principal domain.Principal
	err       error
}

func (f fakeIdentity) CurrentPrincipal(context.Context, string) (domain.Principal, error) {
	return f.principal, f.err
}

type fakeStore struct {
	calls   int
	filter  JobFilter
	started Job
	guide   map[string]int
}

func (f *fakeStore) Start(context.Context, StartInput) (Job, error) {
	f.calls++
	return f.started, nil
}
func (f *fakeStore) Lookup(context.Context, string, Actor) (Job, error) {
	f.calls++
	return Job{}, nil
}
func (f *fakeStore) Retry(context.Context, string, Actor, bool) (Job, error) {
	f.calls++
	return Job{Status: "queued"}, nil
}
func (f *fakeStore) Job(context.Context, string, string, bool) (Job, error) {
	f.calls++
	return Job{}, nil
}
func (f *fakeStore) Jobs(_ context.Context, _ string, _ bool, filter JobFilter) (JobPage, error) {
	f.calls++
	f.filter = filter
	return JobPage{Items: []Job{}, PageSize: filter.Limit}, nil
}

func (f *fakeStore) GuideVersion(_ context.Context, userID string) (int, error) {
	f.calls++
	return f.guide[userID], nil
}
func (f *fakeStore) SeeGuide(_ context.Context, userID string, version int) error {
	f.calls++
	if f.guide == nil {
		f.guide = map[string]int{}
	}
	f.guide[userID] = max(f.guide[userID], version)
	return nil
}

// The guided tour is remembered per user: reading it is a plain read, marking it seen needs the
// console's origin and a sensible version.
func TestGuideIsRememberedPerUser(t *testing.T) {
	operator := fakeIdentity{principal: domain.Principal{User: domain.User{ID: "u1"}, Roles: []domain.Role{{Name: "Operator"}}}}
	store := &fakeStore{}
	h := NewHandler(store, operator, "s", "http://x")
	send := func(method, body string, origin bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/releases/guide", strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "s", Value: "v"})
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if origin {
			req.Header.Set("Origin", "http://x")
		}
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		return res
	}
	if res := send(http.MethodGet, "", false); res.Code != 200 || strings.TrimSpace(res.Body.String()) != `{"seen_version":0}` {
		t.Fatalf("unseen guide = %d %s", res.Code, res.Body.String())
	}
	for _, tc := range []struct {
		body   string
		origin bool
		code   int
	}{
		{`{"version":1}`, false, 403},
		{`{"version":0}`, true, 400},
		{`{"version":1001}`, true, 400},
		{`{"version":1,"extra":true}`, true, 400},
		{`{"version":2}`, true, 204},
		{`{"version":1}`, true, 204},
	} {
		if res := send(http.MethodPut, tc.body, tc.origin); res.Code != tc.code {
			t.Fatalf("PUT %s origin=%t = %d %s, want %d", tc.body, tc.origin, res.Code, res.Body.String(), tc.code)
		}
	}
	if res := send(http.MethodGet, "", false); strings.TrimSpace(res.Body.String()) != `{"seen_version":2}` {
		t.Fatalf("seen guide = %s", res.Body.String())
	}
	unknown := NewHandler(store, fakeIdentity{err: application.ErrInvalidCredentials}, "s", "http://x")
	req := httptest.NewRequest(http.MethodGet, "/api/releases/guide", nil)
	req.AddCookie(&http.Cookie{Name: "s", Value: "v"})
	res := httptest.NewRecorder()
	unknown.ServeHTTP(res, req)
	if res.Code != 401 {
		t.Fatalf("signed-out guide = %d", res.Code)
	}
}

// Browsers send an origin without its default port; the configured one names it. Any other
// scheme, port or host, a missing Origin, or more than one, is refused.
func TestSameOriginIgnoresTheDefaultPort(t *testing.T) {
	for _, tc := range []struct {
		want    string
		origins []string
		ok      bool
	}{
		{"http://192.0.2.10:80", []string{"http://192.0.2.10"}, true},
		{"http://192.0.2.10:80", []string{"http://192.0.2.10:80"}, true},
		{"https://example.com:443", []string{"https://EXAMPLE.com"}, true},
		{"http://localhost:28080", []string{"http://localhost:28080"}, true},
		{"http://192.0.2.10:80", []string{"http://192.0.2.10:8080"}, false},
		{"http://192.0.2.10:80", []string{"https://192.0.2.10"}, false},
		{"http://192.0.2.10:80", []string{"http://192.0.2.11"}, false},
		{"http://192.0.2.10:80", []string{"null"}, false},
		{"http://192.0.2.10:80", nil, false},
		{"http://192.0.2.10:80", []string{"http://192.0.2.10", "http://192.0.2.10"}, false},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/releases", nil)
		for _, origin := range tc.origins {
			req.Header.Add("Origin", origin)
		}
		if got := sameOrigin(req, tc.want); got != tc.ok {
			t.Errorf("sameOrigin(%v, %q) = %t, want %t", tc.origins, tc.want, got, tc.ok)
		}
	}
	// End to end: a release from the browser of an origin on the default port goes through.
	operator := fakeIdentity{principal: domain.Principal{User: domain.User{ID: "u1"}, Roles: []domain.Role{{Name: "Operator"}}}}
	h := NewHandler(&fakeStore{}, operator, "s", "http://192.0.2.10:80")
	req := httptest.NewRequest(http.MethodPut, "/api/releases/guide", strings.NewReader(`{"version":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://192.0.2.10")
	req.AddCookie(&http.Cookie{Name: "s", Value: "v"})
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("default-port origin = %d %s", res.Code, res.Body.String())
	}
}

func TestJobsReadsHistoryFilters(t *testing.T) {
	operator := fakeIdentity{principal: domain.Principal{Roles: []domain.Role{{Name: "Operator"}}}}
	for _, tc := range []struct {
		query string
		code  int
		want  JobFilter
	}{
		{"", 200, JobFilter{Limit: 50}},
		{"?page=3&page_size=5", 200, JobFilter{Limit: 5, Offset: 10}},
		{"?q=+10.0.&kind=release&status=needs_manual_confirmation&latest=1", 200, JobFilter{Query: "10.0.", Kind: "release", Status: "needs_manual_confirmation", Latest: true, Limit: 50}},
		{"?latest=yes", 400, JobFilter{}},
		{"?page=0", 400, JobFilter{}},
		{"?page=two", 400, JobFilter{}},
		{"?page_size=51", 400, JobFilter{}},
		{"?kind=delete", 400, JobFilter{}},
		{"?status=DROP%20TABLE", 400, JobFilter{}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			store := &fakeStore{}
			h := NewHandler(store, operator, "session", "http://localhost:5173")
			r := httptest.NewRequest("GET", "/api/releases/jobs"+tc.query, nil)
			r.AddCookie(&http.Cookie{Name: "session", Value: "token"})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.code || store.filter != tc.want {
				t.Fatalf("%d %+v: %s", w.Code, store.filter, w.Body.String())
			}
		})
	}
}

func startRequest(body string) *http.Request {
	r := httptest.NewRequest("POST", "/api/releases", strings.NewReader(body))
	r.Header.Set("Origin", "http://localhost:5173")
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestStartRequiresSessionAndBusinessRole(t *testing.T) {
	for _, tc := range []struct {
		name     string
		identity fakeIdentity
		cookie   bool
		code     int
	}{
		{"anonymous", fakeIdentity{}, false, 401},
		{"still on an initial password", fakeIdentity{err: application.ErrPasswordChangeRequired}, true, 403},
		{"unrelated custom role", fakeIdentity{principal: domain.Principal{Roles: []domain.Role{{Name: "Auditor"}}}}, true, 403},
		{"operator", fakeIdentity{principal: domain.Principal{Roles: []domain.Role{{Name: "Operator"}}}}, true, 202},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{started: Job{ID: "j1", JobID: "j1", Kind: "release", Status: "queued"}}
			h := NewHandler(store, tc.identity, "session", "http://localhost:5173")
			r := startRequest(`{"target_ip":"10.0.0.1","reason":"temporary_test"}`)
			if tc.cookie {
				r.AddCookie(&http.Cookie{Name: "session", Value: "token"})
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.code {
				t.Fatalf("status %d body %s", w.Code, w.Body.String())
			}
			if tc.code != 202 && store.calls != 0 {
				t.Fatal("unauthorized request touched store")
			}
			if tc.code == 202 {
				var body Job
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.JobID != "j1" || body.Kind != "release" || body.Status != "queued" {
					t.Fatalf("response %s: %v", w.Body.String(), err)
				}
			}
		})
	}
}

func TestStartWithoutSwitchQueuesNothing(t *testing.T) {
	store := &fakeStore{started: Job{Status: "stopped_no_switch", TargetIP: "10.0.0.1"}}
	h := NewHandler(store, fakeIdentity{principal: domain.Principal{Roles: []domain.Role{{Name: "Operator"}}}}, "session", "http://localhost:5173")
	r := startRequest(`{"target_ip":"10.0.0.1","reason":"temporary_test"}`)
	r.AddCookie(&http.Cookie{Name: "session", Value: "token"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"stopped_no_switch"`) || strings.Contains(w.Body.String(), "job_id") {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
}

// Nobody forces a release or picks its switch, an admin included: both fields are unknown.
func TestStartRefusesForceAndSwitchChoice(t *testing.T) {
	for _, body := range []string{
		`{"target_ip":"10.0.0.1","reason":"temporary_test","force":true}`,
		`{"target_ip":"10.0.0.1","reason":"temporary_test","selected_switch_id":"00000000-0000-4000-8000-000000000001"}`,
	} {
		store := &fakeStore{}
		h := NewHandler(store, fakeIdentity{principal: domain.Principal{Roles: []domain.Role{{Name: "Admin"}}}}, "session", "http://localhost:5173")
		r := startRequest(body)
		r.AddCookie(&http.Cookie{Name: "session", Value: "token"})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 || store.calls != 0 {
			t.Fatalf("%s: status %d calls %d", body, w.Code, store.calls)
		}
	}
}
