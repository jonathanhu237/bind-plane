package httpapi

import (
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	"net/http"
)

type lifecycleRequest struct {
	AuthVersion *int64 `json:"authVersion"`
}

func (h *Handler) deactivateUser(w http.ResponseWriter, r *http.Request) {
	h.mutateUserLifecycle(w, r, "deactivate")
}
func (h *Handler) reactivateUser(w http.ResponseWriter, r *http.Request) {
	h.mutateUserLifecycle(w, r, "reactivate")
}
func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	h.mutateUserLifecycle(w, r, "delete")
}

func (h *Handler) mutateUserLifecycle(w http.ResponseWriter, r *http.Request, kind string) {
	action := "users." + kind
	if h.operationOriginFailure(w, r, action, "user") {
		return
	}
	id := r.PathValue("id")
	principal, ok := h.operationPrincipal(w, r, action, "user", id)
	if !ok {
		return
	}
	failure := func(err error) {
		h.recordOperation(r, operationFailure(principal.User, action, "user", id, err, nil))
		writeApplicationError(w, err)
	}
	if !domain.IsCanonicalUUID(id) {
		failure(application.ErrUserNotFound)
		return
	}
	var input lifecycleRequest
	if err := decodeJSONObject(r, &input, map[string]struct{}{"authVersion": {}}); err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "user", id, err, nil))
		writeDecodeError(w, err)
		return
	}
	if input.AuthVersion == nil || *input.AuthVersion < 0 {
		failure(application.ErrStaleRevision)
		return
	}
	service, ok := h.access.(versionedLifecycleService)
	if !ok {
		failure(application.ErrDependencyUnavailable)
		return
	}
	before, hasBefore := h.snapshotUser(r, principal.User.ID, id)
	var user domain.AccessUser
	var err error
	switch kind {
	case "deactivate":
		user, err = service.DeactivateUserWithRevision(r.Context(), principal.User.ID, id, *input.AuthVersion)
	case "reactivate":
		user, err = service.ReactivateUserWithRevision(r.Context(), principal.User.ID, id, *input.AuthVersion)
	case "delete":
		err = service.DeleteUserWithRevision(r.Context(), principal.User.ID, id, *input.AuthVersion)
	}
	if err != nil {
		failure(err)
		return
	}
	details := map[string]any{"requestedAuthVersion": *input.AuthVersion}
	if hasBefore {
		details["before"] = accessUserSnapshot(before)
	}
	if kind == "delete" {
		details["deleted"] = true
	} else {
		after := accessUserSnapshot(user)
		after["disabled"] = user.User.Disabled
		details["after"] = after
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: action, ObjectType: "user", ObjectID: id, Result: application.OperationLogSuccess, Details: details})
	if kind == "delete" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, map[string]accessUserResponseBody{"user": accessUserResponse(user)})
}
