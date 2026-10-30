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
	calls  int
	filter JobFilter
}

func (f *fakeStore) Prepare(context.Context, PrepareInput) (Preparation, error) {
	f.calls++
	return Preparation{Status: "query_queued"}, nil
}
func (f *fakeStore) Confirm(context.Context, ConfirmInput) (Job, error) {
	f.calls++
	return Job{Status: "queued"}, nil
}
func (f *fakeStore) Retry(context.Context, string, string, string, bool) (Job, error) {
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
		{"?q=+10.0.&kind=release&status=needs_manual_confirmation", 200, JobFilter{Query: "10.0.", Kind: "release", Status: "needs_manual_confirmation", Limit: 50}},
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

func TestPrepareRequiresSessionAndBusinessRole(t *testing.T) {
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
			store := &fakeStore{}
			h := NewHandler(store, tc.identity, "session", "http://localhost:5173")
			r := httptest.NewRequest("POST", "/api/releases/prepare", strings.NewReader(`{"target_ip":"10.0.0.1","reason":"temporary_test"}`))
			r.Header.Set("Origin", "http://localhost:5173")
			r.Header.Set("Content-Type", "application/json")
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
				var body Preparation
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Status != "query_queued" {
					t.Fatalf("response %s: %v", w.Body.String(), err)
				}
			}
		})
	}
}

func TestOperatorCannotForceRelease(t *testing.T) {
	store := &fakeStore{}
	h := NewHandler(store, fakeIdentity{principal: domain.Principal{Roles: []domain.Role{{Name: "Operator"}}}}, "session", "http://localhost:5173")
	r := httptest.NewRequest("POST", "/api/releases", strings.NewReader(`{"preparation_job_id":"abc","force":true}`))
	r.Header.Set("Origin", "http://localhost:5173")
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "session", Value: "token"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 || store.calls != 0 {
		t.Fatalf("status %d calls %d", w.Code, store.calls)
	}
}
