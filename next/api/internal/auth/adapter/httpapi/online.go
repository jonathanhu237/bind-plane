package httpapi

import (
	"context"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"net/http"
)

type onlineService interface {
	OnlineUsers(context.Context, string) ([]application.OnlineUser, error)
	KickUser(context.Context, string, string) error
}

func (h *Handler) onlineUsers(w http.ResponseWriter, r *http.Request) {
	principal, err := h.currentPrincipalNoTouch(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	service, ok := h.auth.(onlineService)
	if !ok {
		writeApplicationError(w, application.ErrDependencyUnavailable)
		return
	}
	users, err := service.OnlineUsers(r.Context(), principal.User.ID)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (h *Handler) kickUser(w http.ResponseWriter, r *http.Request) {
	const action = "users.sessions.revoke"
	if h.operationOriginFailure(w, r, action, "user") {
		return
	}
	id := r.PathValue("id")
	principal, ok := h.operationPrincipal(w, r, action, "user", id)
	if !ok {
		return
	}
	err := h.auth.(onlineService).KickUser(r.Context(), principal.User.ID, id)
	if err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "user", id, err, nil))
		writeApplicationError(w, err)
		return
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: action, ObjectType: "user", ObjectID: id, Result: application.OperationLogSuccess})
	w.WriteHeader(http.StatusNoContent)
}
