package release

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
