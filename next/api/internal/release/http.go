package release

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// StartInput asks for the release of one address. The worker queries the binding first and changes
// nothing unless it is static.
type StartInput struct {
	TargetIP string `json:"target_ip"`
	Reason   string `json:"reason"`
	Actor    Actor  `json:"-"`
}

// Actor is who starts a job, and Via where: "console" (the default), "wecom" or "feishu". The job
// keeps their name and email as they were at the time.
type Actor struct{ ID, Name, Email, Via string }

// source is where a job was started, as stored.
func (a Actor) source() string {
	if a.Via == platformWeCom || a.Via == platformFeishu {
		return a.Via
	}
	return "console"
}

type JobEvent struct {
	Phase     string    `json:"phase"`
	Status    string    `json:"status"`
	Message   *string   `json:"message,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
type Job struct {
	ID                 string     `json:"id,omitempty"`
	JobID              string     `json:"job_id,omitempty"`
	TargetIP           string     `json:"target_ip,omitempty"`
	SwitchID           string     `json:"switch_id,omitempty"`
	SwitchName         string     `json:"switch_name,omitempty"`
	SwitchManagementIP string     `json:"switch_management_ip,omitempty"`
	CommandProfileID   string     `json:"command_profile_id,omitempty"`
	ActorName          string     `json:"actor_name,omitempty"`
	ActorEmail         string     `json:"actor_email,omitempty"`
	RetryOfID          *string    `json:"retry_of_id,omitempty"`
	PreparationJobID   *string    `json:"preparation_job_id,omitempty"` // a release from the old two-step flow: the query it confirmed
	ReleaseJobID       *string    `json:"release_job_id,omitempty"`     // a query from the old two-step flow: the release that confirmed it
	NextReleaseID      *string    `json:"next_release_id,omitempty"`    // the next release of the same address that the reader may see
	CreatedAt          *time.Time `json:"created_at,omitempty"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	FinishedAt         *time.Time `json:"finished_at,omitempty"`
	Events             []JobEvent `json:"events,omitempty"`
	Kind               string     `json:"kind,omitempty"`
	Source             string     `json:"source,omitempty"`
	Status             string     `json:"status"`
	Phase              string     `json:"phase,omitempty"`
	Reason             string     `json:"reason,omitempty"`
	Force              bool       `json:"force"` // forced past a missing record, before forced release was removed
	BeforeState        any        `json:"before_state,omitempty"`
	AfterState         any        `json:"after_state,omitempty"`
	Result             any        `json:"result,omitempty"`
	RawBeforeOutput    *string    `json:"raw_before_output,omitempty"`
	RawReleaseOutput   *string    `json:"raw_release_output,omitempty"`
	RawAfterOutput     *string    `json:"raw_after_output,omitempty"`
	ErrorMessage       *string    `json:"error_message,omitempty"`
}

// JobFilter narrows the release history; empty fields match every job. Latest keeps only jobs that
// no later release of the same address has followed.
type JobFilter struct {
	Query, Kind, Status string
	Latest              bool
	Limit, Offset       int
}
type JobPage struct {
	Items    []Job `json:"items"`
	Total    int64 `json:"total"`
	PageSize int   `json:"page_size"`
}

type Store interface {
	Start(context.Context, StartInput) (Job, error)
	Lookup(context.Context, string, Actor) (Job, error)
	Retry(context.Context, string, Actor, bool) (Job, error)
	Job(context.Context, string, string, bool) (Job, error)
	Jobs(context.Context, string, bool, JobFilter) (JobPage, error)
	// GuideVersion is the newest version of the guided tour the user has seen; 0 for none.
	GuideVersion(ctx context.Context, userID string) (int, error)
	// SeeGuide records that the user finished or skipped a version of the guided tour.
	SeeGuide(ctx context.Context, userID string, version int) error
}
type Identity interface {
	CurrentPrincipal(context.Context, string) (domain.Principal, error)
}
type handler struct {
	store          Store
	identity       Identity
	cookie, origin string
}

func NewHandler(store Store, identity Identity, cookieName, origin string) http.Handler {
	h := &handler{store: store, identity: identity, cookie: cookieName, origin: origin}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/releases", h.start)
	mux.HandleFunc("POST /api/releases/lookups", h.lookup)
	mux.HandleFunc("GET /api/releases/jobs", h.jobs)
	mux.HandleFunc("GET /api/releases/jobs/{id}", h.job)
	mux.HandleFunc("POST /api/releases/jobs/{id}/retry", h.retry)
	mux.HandleFunc("GET /api/releases/guide", h.guide)
	mux.HandleFunc("PUT /api/releases/guide", h.seeGuide)
	return mux
}
func write(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
func fail(w http.ResponseWriter, code int, message string) {
	write(w, code, map[string]string{"detail": message})
}
func decode(r *http.Request, into any) error { return decodeLimit(r, into, 1<<16) }

func decodeLimit(r *http.Request, into any, limit int64) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("JSON required")
	}
	d := json.NewDecoder(http.MaxBytesReader(nil, r.Body, limit))
	d.DisallowUnknownFields()
	if err := d.Decode(into); err != nil {
		return err
	}
	var rest any
	if err := d.Decode(&rest); err != io.EOF {
		return errors.New("exactly one JSON object required")
	}
	return nil
}
func (h *handler) principal(w http.ResponseWriter, r *http.Request, mutate bool) (domain.Principal, bool, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if mutate && r.Header.Get("Origin") != h.origin {
		fail(w, 403, "invalid origin")
		return domain.Principal{}, false, false
	}
	cookie, err := r.Cookie(h.cookie)
	if err != nil || cookie.Value == "" {
		fail(w, 401, "login required")
		return domain.Principal{}, false, false
	}
	p, err := h.identity.CurrentPrincipal(r.Context(), cookie.Value)
	if errors.Is(err, application.ErrPasswordChangeRequired) {
		// An account an administrator created must replace its initial password first.
		fail(w, 403, "password change required")
		return domain.Principal{}, false, false
	}
	if err != nil {
		fail(w, 401, "login required")
		return domain.Principal{}, false, false
	}
	admin, operator := releaseRoles(p)
	if !operator {
		fail(w, 403, "operator role required")
		return p, false, false
	}
	return p, admin, true
}
func (h *handler) start(w http.ResponseWriter, r *http.Request) {
	p, _, ok := h.principal(w, r, true)
	if !ok {
		return
	}
	var input StartInput
	if err := decode(r, &input); err != nil {
		fail(w, 400, "invalid request")
		return
	}
	if !ValidReason(input.Reason) {
		fail(w, 422, "release reason required")
		return
	}
	if _, err := ResolveNetwork(input.TargetIP, nil); err == ErrInvalidIP {
		fail(w, 422, err.Error())
		return
	}
	input.Actor = Actor{ID: p.User.ID, Name: p.User.Name, Email: p.User.Email}
	job, err := h.store.Start(r.Context(), input)
	if err != nil {
		storeError(w, err)
		return
	}
	// Without a job the release stopped before any switch: none, or several, own the address.
	if job.JobID == "" {
		write(w, http.StatusOK, job)
		return
	}
	write(w, http.StatusAccepted, job)
}

// lookup asks an address's switch for its ARP entry, changing nothing; it answers like start.
func (h *handler) lookup(w http.ResponseWriter, r *http.Request) {
	p, _, ok := h.principal(w, r, true)
	if !ok {
		return
	}
	var input struct {
		TargetIP string `json:"target_ip"`
	}
	if err := decode(r, &input); err != nil {
		fail(w, 400, "invalid request")
		return
	}
	if _, err := ResolveNetwork(input.TargetIP, nil); err == ErrInvalidIP {
		fail(w, 422, err.Error())
		return
	}
	job, err := h.store.Lookup(r.Context(), input.TargetIP, Actor{ID: p.User.ID, Name: p.User.Name, Email: p.User.Email})
	if err != nil {
		storeError(w, err)
		return
	}
	if job.JobID == "" {
		write(w, http.StatusOK, job)
		return
	}
	write(w, http.StatusAccepted, job)
}

func (h *handler) job(w http.ResponseWriter, r *http.Request) {
	p, admin, ok := h.principal(w, r, false)
	if !ok {
		return
	}
	result, err := h.store.Job(r.Context(), r.PathValue("id"), p.User.ID, admin)
	if err != nil {
		storeError(w, err)
		return
	}
	write(w, 200, result)
}
func (h *handler) retry(w http.ResponseWriter, r *http.Request) {
	p, admin, ok := h.principal(w, r, true)
	if !ok {
		return
	}
	job, err := h.store.Retry(r.Context(), r.PathValue("id"), Actor{ID: p.User.ID, Name: p.User.Name, Email: p.User.Email}, admin)
	if err != nil {
		storeError(w, err)
		return
	}
	write(w, http.StatusAccepted, job)
}
func (h *handler) jobs(w http.ResponseWriter, r *http.Request) {
	p, admin, ok := h.principal(w, r, false)
	if !ok {
		return
	}
	filter, ok := jobFilter(r.URL.Query())
	if !ok {
		fail(w, 400, "invalid filter")
		return
	}
	result, err := h.store.Jobs(r.Context(), p.User.ID, admin, filter)
	if err != nil {
		storeError(w, err)
		return
	}
	write(w, 200, result)
}

// jobPageSize is the longest page of release history one request returns.
const jobPageSize = 50

var jobStatus = regexp.MustCompile(`^[a-z_]{1,40}$`)

// jobFilter reads the history's search text, kind, status, latest flag and page from the query string.
func jobFilter(q url.Values) (JobFilter, bool) {
	filter := JobFilter{Query: strings.TrimSpace(q.Get("q")), Kind: q.Get("kind"), Status: q.Get("status"), Latest: q.Get("latest") == "1", Limit: jobPageSize}
	if len(filter.Query) > 100 || filter.Kind != "" && filter.Kind != "release" && filter.Kind != "pre_release_query" && filter.Kind != "lookup" ||
		filter.Status != "" && !jobStatus.MatchString(filter.Status) || q.Get("latest") != "" && !filter.Latest {
		return JobFilter{}, false
	}
	page := 1
	if value := q.Get("page"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 100000 {
			return JobFilter{}, false
		}
		page = n
	}
	if value := q.Get("page_size"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > jobPageSize {
			return JobFilter{}, false
		}
		filter.Limit = n
	}
	filter.Offset = (page - 1) * filter.Limit
	return filter, true
}

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")

// guide says which version of the guided tour the user has seen, so that it shows once per user
// rather than once per browser.
func (h *handler) guide(w http.ResponseWriter, r *http.Request) {
	p, _, ok := h.principal(w, r, false)
	if !ok {
		return
	}
	version, err := h.store.GuideVersion(r.Context(), p.User.ID)
	if err != nil {
		storeError(w, err)
		return
	}
	write(w, http.StatusOK, map[string]int{"seen_version": version})
}

func (h *handler) seeGuide(w http.ResponseWriter, r *http.Request) {
	p, _, ok := h.principal(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Version int `json:"version"`
	}
	if err := decode(r, &input); err != nil || input.Version < 1 || input.Version > 1000 {
		fail(w, 400, "invalid request")
		return
	}
	if err := h.store.SeeGuide(r.Context(), p.User.ID, input.Version); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNoSwitch), errors.Is(err, ErrAmbiguous), errors.Is(err, ErrConflict), errors.Is(err, ErrAssetChanged):
		fail(w, 409, err.Error())
	case errors.Is(err, ErrNotFound):
		fail(w, 404, err.Error())
	case errors.Is(err, ErrInvalidIP):
		fail(w, 422, err.Error())
	default:
		fail(w, 503, "service unavailable")
	}
}
