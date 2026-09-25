package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type mailTaskAttemptResponse struct {
	ID         string `json:"id"`
	Round      int    `json:"round"`
	Attempt    int    `json:"attempt"`
	Outcome    string `json:"outcome"`
	ErrorCode  string `json:"errorCode,omitempty"`
	OccurredAt string `json:"occurredAt"`
}

type mailTaskResponse struct {
	ID                string                    `json:"id"`
	Kind              string                    `json:"kind"`
	Purpose           string                    `json:"purpose"`
	RecipientEmail    string                    `json:"recipientEmail"`
	RecipientName     string                    `json:"recipientName,omitempty"`
	Locale            string                    `json:"locale"`
	Status            string                    `json:"status"`
	CreatedAt         string                    `json:"createdAt"`
	AvailableAt       string                    `json:"availableAt"`
	FinishedAt        string                    `json:"finishedAt,omitempty"`
	AttemptCount      int                       `json:"attemptCount"`
	Round             int                       `json:"round"`
	RoundAttemptCount int                       `json:"roundAttemptCount"`
	LastErrorCode     string                    `json:"lastErrorCode,omitempty"`
	Attempts          []mailTaskAttemptResponse `json:"attempts,omitempty"`
}

type mailTasksResponse struct {
	Tasks      []mailTaskResponse `json:"tasks"`
	NextCursor string             `json:"nextCursor,omitempty"`
}

type mailTaskEnvelope struct {
	Task mailTaskResponse `json:"task"`
}

type mailTaskBulkRequest struct {
	IDs []string `json:"ids"`
}

type mailTaskBulkResponse struct {
	Succeeded int                    `json:"succeeded"`
	Skipped   int                    `json:"skipped"`
	Failed    int                    `json:"failed"`
	Items     []mailTaskBulkItemBody `json:"items"`
}

type mailTaskBulkItemBody struct {
	ID     string `json:"id"`
	Result string `json:"result"`
	Code   string `json:"code,omitempty"`
}

func (h *Handler) mailTasksList(w http.ResponseWriter, r *http.Request) {
	if h.mailTasks == nil {
		h.notFound(w, r)
		return
	}
	principal, err := h.currentPrincipal(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	options, err := parseMailTaskQuery(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	page, err := h.mailTasks.List(r.Context(), principal.User.ID, options)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	items := make([]mailTaskResponse, 0, len(page.Items))
	for _, task := range page.Items {
		items = append(items, mailTaskResponseBody(task))
	}
	writeJSON(w, http.StatusOK, mailTasksResponse{Tasks: items, NextCursor: page.NextCursor})
}

func (h *Handler) mailTaskDetail(w http.ResponseWriter, r *http.Request) {
	if h.mailTasks == nil {
		h.notFound(w, r)
		return
	}
	principal, err := h.currentPrincipal(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	task, err := h.mailTasks.Detail(r.Context(), principal.User.ID, r.PathValue("id"))
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mailTaskEnvelope{Task: mailTaskResponseBody(task)})
}

func (h *Handler) mailTaskOwnerStatus(w http.ResponseWriter, r *http.Request) {
	if h.mailTasks == nil {
		h.notFound(w, r)
		return
	}
	principal, err := h.currentPrincipal(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	owner, ok := h.mailTasks.(MailTaskOwnerStatusService)
	if !ok {
		writeApplicationError(w, application.ErrDependencyUnavailable)
		return
	}
	task, err := owner.OwnStatus(r.Context(), principal.User.ID, r.PathValue("id"))
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mailTaskEnvelope{Task: mailTaskResponseBody(task)})
}

func (h *Handler) mailTaskRetry(w http.ResponseWriter, r *http.Request) {
	if h.mailTasks == nil {
		h.notFound(w, r)
		return
	}
	if h.operationOriginFailure(w, r, "mail_tasks.retry", "mail_task") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, "mail_tasks.retry", "mail_task", r.PathValue("id"))
	if !ok {
		return
	}
	task, err := h.mailTasks.Retry(r.Context(), principal.User.ID, r.PathValue("id"))
	if err != nil {
		h.recordOperation(r, operationFailure(principal.User, "mail_tasks.retry", "mail_task", r.PathValue("id"), err, nil))
		writeApplicationError(w, err)
		return
	}
	h.recordOperation(r, mailTaskOperationSuccess(principal.User, "mail_tasks.retry", "mail_task", task.ID, map[string]any{"status": task.Status}))
	writeJSON(w, http.StatusOK, mailTaskEnvelope{Task: mailTaskResponseBody(task)})
}

func (h *Handler) mailTaskDelete(w http.ResponseWriter, r *http.Request) {
	if h.mailTasks == nil {
		h.notFound(w, r)
		return
	}
	if h.operationOriginFailure(w, r, "mail_tasks.delete", "mail_task") {
		return
	}
	id := r.PathValue("id")
	principal, ok := h.operationPrincipal(w, r, "mail_tasks.delete", "mail_task", id)
	if !ok {
		return
	}
	if err := h.mailTasks.Delete(r.Context(), principal.User.ID, id); err != nil {
		h.recordOperation(r, operationFailure(principal.User, "mail_tasks.delete", "mail_task", id, err, nil))
		writeApplicationError(w, err)
		return
	}
	h.recordOperation(r, mailTaskOperationSuccess(principal.User, "mail_tasks.delete", "mail_task", id, nil))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) mailTasksBulkRetry(w http.ResponseWriter, r *http.Request) {
	h.mailTasksBulk(w, r, true)
}

func (h *Handler) mailTasksBulkDelete(w http.ResponseWriter, r *http.Request) {
	h.mailTasksBulk(w, r, false)
}

func (h *Handler) mailTasksBulk(w http.ResponseWriter, r *http.Request, retry bool) {
	if h.mailTasks == nil {
		h.notFound(w, r)
		return
	}
	action := "mail_tasks.bulk_delete"
	if retry {
		action = "mail_tasks.bulk_retry"
	}
	if h.operationOriginFailure(w, r, action, "mail_task") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, action, "mail_task", "")
	if !ok {
		return
	}
	var input mailTaskBulkRequest
	if err := decodeJSONObject(r, &input, map[string]struct{}{"ids": {}}); err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "mail_task", "", err, nil))
		writeDecodeError(w, err)
		return
	}
	var result application.MailTaskBatchResult
	var err error
	if retry {
		result, err = h.mailTasks.BulkRetry(r.Context(), principal.User.ID, input.IDs)
	} else {
		result, err = h.mailTasks.BulkDelete(r.Context(), principal.User.ID, input.IDs)
	}
	if err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "mail_task", "", err, map[string]any{"count": len(input.IDs)}))
		writeApplicationError(w, err)
		return
	}
	items := make([]mailTaskBulkItemBody, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, mailTaskBulkItemBody{ID: item.ID, Result: item.Result, Code: item.Code})
	}
	auditItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		auditItems = append(auditItems, map[string]any{"id": item.ID, "result": item.Result, "code": item.Code})
	}
	h.recordOperation(r, mailTaskOperationSuccess(principal.User, action, "mail_task", "", map[string]any{"count": len(input.IDs), "succeeded": result.Succeeded, "skipped": result.Skipped, "failed": result.Failed, "items": auditItems}))
	writeJSON(w, http.StatusOK, mailTaskBulkResponse{Succeeded: result.Succeeded, Skipped: result.Skipped, Failed: result.Failed, Items: items})
}

func mailTaskOperationSuccess(user domain.User, action, objectType, objectID string, details map[string]any) application.OperationLogInput {
	return application.OperationLogInput{ActorID: user.ID, ActorName: user.Name, ActorEmail: user.Email, Action: action, ObjectType: objectType, ObjectID: objectID, Result: application.OperationLogSuccess, Details: details}
}

func parseMailTaskQuery(r *http.Request) (application.MailTaskListOptions, error) {
	query := r.URL.Query()
	kind := query.Get("kind")
	if kind == "" {
		kind = query.Get("purpose")
	}
	options := application.MailTaskListOptions{Cursor: query.Get("cursor"), Recipient: query.Get("recipient"), Kind: application.MailKind(kind), Status: query.Get("status"), Limit: application.MailTaskDefaultPageSize}
	if options.Recipient == "" {
		options.Recipient = query.Get("recipientEmail")
	}
	if raw := query.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return options, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "limit", Code: "invalid_limit"}}}
		}
		options.Limit = value
	}
	var err error
	fromRaw := query.Get("from")
	if fromRaw == "" {
		fromRaw = query.Get("createdFrom")
	}
	if raw := fromRaw; raw != "" {
		options.From, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return options, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "from", Code: "invalid_time"}}}
		}
	}
	toRaw := query.Get("to")
	if toRaw == "" {
		toRaw = query.Get("createdTo")
	}
	if raw := toRaw; raw != "" {
		options.To, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return options, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "to", Code: "invalid_time"}}}
		}
	}
	failedRaw := query.Get("failedOnly")
	if failedRaw == "" {
		failedRaw = query.Get("failed")
	}
	if raw := failedRaw; raw != "" {
		switch strings.ToLower(raw) {
		case "1", "true":
			options.FailedOnly = true
		case "0", "false":
		default:
			return options, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "failedOnly", Code: "invalid_value"}}}
		}
	}
	if options.Limit < 1 || options.Limit > application.MailTaskMaxPageSize {
		return options, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "limit", Code: "invalid_limit"}}}
	}
	if options.Kind != "" && !options.Kind.Valid() {
		return options, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "kind", Code: "invalid_value"}}}
	}
	switch options.Status {
	case "", application.MailTaskStatusQueued, application.MailTaskStatusSending, application.MailTaskStatusWaitingRetry, application.MailTaskStatusSent, application.MailTaskStatusFailed:
	default:
		return options, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "status", Code: "invalid_value"}}}
	}
	if !options.From.IsZero() && !options.To.IsZero() && !options.From.Before(options.To) {
		return options, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "to", Code: "invalid_time_range"}}}
	}
	return options, nil
}

func mailTaskResponseBody(task application.MailTask) mailTaskResponse {
	body := mailTaskResponse{ID: task.ID, Kind: string(task.Kind), Purpose: string(task.Kind), RecipientEmail: task.RecipientEmail, RecipientName: task.RecipientName, Locale: string(task.Locale), Status: task.Status, CreatedAt: formatMailTaskTime(task.CreatedAt), AvailableAt: formatMailTaskTime(task.AvailableAt), AttemptCount: task.AttemptCount, Round: task.Round, RoundAttemptCount: task.RoundAttemptCount, LastErrorCode: task.LastErrorCode}
	if !task.FinishedAt.IsZero() {
		body.FinishedAt = formatMailTaskTime(task.FinishedAt)
	}
	if len(task.Attempts) > 0 {
		body.Attempts = make([]mailTaskAttemptResponse, 0, len(task.Attempts))
		for _, attempt := range task.Attempts {
			body.Attempts = append(body.Attempts, mailTaskAttemptResponse{ID: attempt.ID, Round: attempt.Round, Attempt: attempt.Attempt, Outcome: attempt.Outcome, ErrorCode: attempt.ErrorCode, OccurredAt: formatMailTaskTime(attempt.OccurredAt)})
		}
	}
	return body
}

func formatMailTaskTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
