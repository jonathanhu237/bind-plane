package release

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type PrepareInput struct {
	TargetIP         string `json:"target_ip"`
	Reason           string `json:"reason"`
	Force            bool   `json:"force"`
	SelectedSwitchID string `json:"selected_switch_id,omitempty"`
	ActorID          string `json:"-"`
	ActorEmail       string `json:"-"`
	Admin            bool   `json:"-"`
}
type ConfirmInput struct {
	PreparationJobID string `json:"preparation_job_id"`
	Force            bool   `json:"force"`
	ActorID          string `json:"-"`
	Admin            bool   `json:"-"`
}
type Preparation struct {
	PreparationJobID string `json:"preparation_job_id,omitempty"`
	Status           string `json:"status"`
	TargetIP         string `json:"target_ip"`
	ResolvedSwitch   any    `json:"resolved_switch,omitempty"`
	Force            bool   `json:"force"`
	Reason           string `json:"reason"`
}
type JobEvent struct {
	Phase     string    `json:"phase"`
	Status    string    `json:"status"`
	Message   *string   `json:"message,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
type Job struct {
	ID               string     `json:"id,omitempty"`
	JobID            string     `json:"job_id,omitempty"`
	TargetIP         string     `json:"target_ip,omitempty"`
	SwitchID         string     `json:"switch_id,omitempty"`
	ActorEmail       string     `json:"actor_email,omitempty"`
	RetryOfID        *string    `json:"retry_of_id,omitempty"`
	PreparationJobID *string    `json:"preparation_job_id,omitempty"`
	CreatedAt        *time.Time `json:"created_at,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	Events           []JobEvent `json:"events,omitempty"`
	Kind             string     `json:"kind,omitempty"`
	Status           string     `json:"status"`
	Phase            string     `json:"phase,omitempty"`
	Reason           string     `json:"reason,omitempty"`
	Force            bool       `json:"force"`
	BeforeState      any        `json:"before_state,omitempty"`
	AfterState       any        `json:"after_state,omitempty"`
	Result           any        `json:"result,omitempty"`
	RawBeforeOutput  *string    `json:"raw_before_output,omitempty"`
	RawReleaseOutput *string    `json:"raw_release_output,omitempty"`
	RawAfterOutput   *string    `json:"raw_after_output,omitempty"`
	ErrorMessage     *string    `json:"error_message,omitempty"`
}

type Store interface {
	Prepare(context.Context, PrepareInput) (Preparation, error)
	Confirm(context.Context, ConfirmInput) (Job, error)
	Retry(context.Context, string, string, string, bool) (Job, error)
	Job(context.Context, string, string, bool) (Job, error)
	Jobs(context.Context, string, bool) ([]Job, error)
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
	mux.HandleFunc("POST /api/releases/prepare", h.prepare)
	mux.HandleFunc("POST /api/releases", h.confirm)
	mux.HandleFunc("GET /api/releases/jobs", h.jobs)
	mux.HandleFunc("GET /api/releases/jobs/{id}", h.job)
	mux.HandleFunc("POST /api/releases/jobs/{id}/retry", h.retry)
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
func decode(r *http.Request, into any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("JSON required")
	}
	d := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<16))
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
	if err != nil {
		fail(w, 401, "login required")
		return domain.Principal{}, false, false
	}
	admin := p.SuperAdmin
	operator := admin
	for _, role := range p.Roles {
		switch role.Name {
		case "Admin":
			admin = true
			operator = true
		case "Operator":
			operator = true
		}
	}
	if !operator {
		fail(w, 403, "operator role required")
		return p, false, false
	}
	return p, admin, true
}
func (h *handler) prepare(w http.ResponseWriter, r *http.Request) {
	p, admin, ok := h.principal(w, r, true)
	if !ok {
		return
	}
	var input PrepareInput
	if err := decode(r, &input); err != nil {
		fail(w, 400, "invalid request")
		return
	}
	if input.Force && !admin || input.SelectedSwitchID != "" && (!admin || !input.Force) {
		fail(w, 403, "admin force required")
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
	input.ActorID, input.ActorEmail, input.Admin = p.User.ID, p.User.Email, admin
	result, err := h.store.Prepare(r.Context(), input)
	if err != nil {
		storeError(w, err)
		return
	}
	write(w, 202, result)
}
func (h *handler) confirm(w http.ResponseWriter, r *http.Request) {
	p, admin, ok := h.principal(w, r, true)
	if !ok {
		return
	}
	var input ConfirmInput
	if err := decode(r, &input); err != nil || input.PreparationJobID == "" {
		fail(w, 400, "preparation job required")
		return
	}
	if input.Force && !admin {
		fail(w, 403, "admin force required")
		return
	}
	input.ActorID, input.Admin = p.User.ID, admin
	result, err := h.store.Confirm(r.Context(), input)
	if err != nil {
		storeError(w, err)
		return
	}
	write(w, 202, result)
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
	job, err := h.store.Retry(r.Context(), r.PathValue("id"), p.User.ID, p.User.Email, admin)
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
	result, err := h.store.Jobs(r.Context(), p.User.ID, admin)
	if err != nil {
		storeError(w, err)
		return
	}
	write(w, 200, result)
}

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")

func storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNoSwitch), errors.Is(err, ErrAmbiguous), errors.Is(err, ErrConflict):
		fail(w, 409, err.Error())
	case errors.Is(err, ErrNotFound):
		fail(w, 404, err.Error())
	case errors.Is(err, ErrInvalidIP):
		fail(w, 422, err.Error())
	default:
		fail(w, 503, "service unavailable")
	}
}
