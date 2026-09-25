package httpapi

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type operationLogResponseBody struct {
	ID               string         `json:"id"`
	Actor            operationActor `json:"actor"`
	Action           string         `json:"action"`
	ObjectType       string         `json:"objectType"`
	ObjectID         string         `json:"objectId,omitempty"`
	ObjectDeleted    bool           `json:"objectDeleted"`
	Result           string         `json:"result"`
	OccurredAt       string         `json:"occurredAt"`
	SourceIP         string         `json:"sourceIp,omitempty"`
	AttemptedAccount string         `json:"attemptedAccount,omitempty"`
	Details          map[string]any `json:"details"`
}

type operationActor struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Email   string `json:"email,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Label   string `json:"label,omitempty"`
}

type operationLogsResponse struct {
	Logs       []operationLogResponseBody `json:"logs"`
	NextCursor string                     `json:"nextCursor,omitempty"`
}

type operationLogStatusResponse struct {
	State         string `json:"state"`
	FailureCount  int64  `json:"failureCount"`
	LastFailureAt string `json:"lastFailureAt,omitempty"`
	LastSuccessAt string `json:"lastSuccessAt,omitempty"`
}

type operationLogRetentionResponse struct {
	RetentionDays int    `json:"retentionDays"`
	Revision      int64  `json:"revision"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
}

func (h *Handler) operationLogsList(w http.ResponseWriter, r *http.Request) {
	principal, err := h.requireOperationLogRead(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	options, err := parseOperationLogQuery(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	page, err := h.operationLogs.List(r.Context(), options)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	_ = principal
	items := make([]operationLogResponseBody, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, operationLogResponse(item))
	}
	writeJSON(w, http.StatusOK, operationLogsResponse{Logs: items, NextCursor: page.NextCursor})
}

func (h *Handler) operationLogsDetail(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireOperationLogRead(r); err != nil {
		writeApplicationError(w, err)
		return
	}
	id := r.PathValue("id")
	if !domain.IsCanonicalUUID(id) {
		writeProblem(w, http.StatusNotFound, "not-found")
		return
	}
	item, err := h.operationLogs.Detail(r.Context(), id)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]operationLogResponseBody{"log": operationLogResponse(item)})
}

func (h *Handler) operationLogsStatus(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireOperationLogReadNoTouch(r); err != nil {
		writeApplicationError(w, err)
		return
	}
	status := h.operationLogs.RecordingStatus(time.Now())
	response := operationLogStatusResponse{State: status.State, FailureCount: status.FailureCount}
	if status.LastFailureAt != nil {
		response.LastFailureAt = status.LastFailureAt.UTC().Format(time.RFC3339Nano)
	}
	if status.LastSuccessAt != nil {
		response.LastSuccessAt = status.LastSuccessAt.UTC().Format(time.RFC3339Nano)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) operationLogRetention(w http.ResponseWriter, r *http.Request) {
	principal, err := h.currentPrincipal(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	if !principal.SuperAdmin && !principal.Has(domain.PermissionSettingsRead) {
		writeProblem(w, http.StatusForbidden, "forbidden")
		return
	}
	retention, err := h.operationLogs.Retention(r.Context())
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, operationLogRetentionResponseBodyFrom(retention))
}

type operationLogRetentionRequest struct {
	RetentionDays int    `json:"retentionDays"`
	Revision      *int64 `json:"revision"`
}

func (h *Handler) saveOperationLogRetention(w http.ResponseWriter, r *http.Request) {
	if h.operationOriginFailure(w, r, "settings.operation_log_retention.update", "operation_log_settings") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, "settings.operation_log_retention.update", "operation_log_settings", "")
	if !ok {
		return
	}
	var err error
	if !principal.SuperAdmin && !principal.Has(domain.PermissionSettingsWrite) {
		h.recordOperation(r, operationFailure(principal.User, "settings.operation_log_retention.update", "operation_log_settings", "", application.ErrForbidden, nil))
		writeProblem(w, http.StatusForbidden, "forbidden")
		return
	}
	before, beforeErr := h.operationLogs.Retention(r.Context())
	var input operationLogRetentionRequest
	if err := decodeJSONObject(r, &input, map[string]struct{}{"retentionDays": {}, "revision": {}}); err != nil {
		h.recordOperation(r, operationFailure(principal.User, "settings.operation_log_retention.update", "operation_log_settings", "", err, nil))
		writeDecodeError(w, err)
		return
	}
	revision := int64(0)
	if input.Revision != nil {
		revision = *input.Revision
	}
	retention, err := h.operationLogs.SaveRetention(r.Context(), revision, input.RetentionDays)
	if err != nil {
		details := map[string]any{"requestedDays": input.RetentionDays, "requestedRevision": revision}
		if beforeErr == nil {
			details["before"] = map[string]any{"retentionDays": before.Days, "revision": before.Revision}
		}
		h.recordOperation(r, operationFailure(principal.User, "settings.operation_log_retention.update", "operation_log_settings", "", err, details))
		writeApplicationError(w, err)
		return
	}
	details := map[string]any{"before": nil, "after": map[string]any{"retentionDays": retention.Days, "revision": retention.Revision}}
	if beforeErr == nil {
		details["before"] = map[string]any{"retentionDays": before.Days, "revision": before.Revision}
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "settings.operation_log_retention.update", ObjectType: "operation_log_settings", Result: application.OperationLogSuccess, Details: details})
	writeJSON(w, http.StatusOK, operationLogRetentionResponseBodyFrom(retention))
}

func (h *Handler) requireOperationLogRead(r *http.Request) (domain.Principal, error) {
	principal, err := h.currentPrincipal(r)
	if err != nil {
		return domain.Principal{}, err
	}
	if !principal.SuperAdmin && !principal.Has(domain.PermissionOperationLogsRead) {
		return domain.Principal{}, application.ErrForbidden
	}
	return principal, nil
}

func (h *Handler) requireOperationLogReadNoTouch(r *http.Request) (domain.Principal, error) {
	principal, err := h.currentPrincipalNoTouch(r)
	if err != nil {
		return domain.Principal{}, err
	}
	if !principal.SuperAdmin && !principal.Has(domain.PermissionOperationLogsRead) {
		return domain.Principal{}, application.ErrForbidden
	}
	return principal, nil
}

func parseOperationLogQuery(r *http.Request) (application.OperationLogListOptions, error) {
	query := r.URL.Query()
	options := application.OperationLogListOptions{Cursor: query.Get("cursor"), Limit: 25, ActorID: query.Get("actorId"), Action: query.Get("action"), ObjectType: query.Get("objectType"), ObjectID: query.Get("objectId"), Result: query.Get("result")}
	if raw := query.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return options, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "limit", Code: "invalid_limit"}}}
		}
		options.Limit = value
	}
	var err error
	if raw := query.Get("from"); raw != "" {
		options.From, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return options, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "from", Code: "invalid_time"}}}
		}
	}
	if raw := query.Get("to"); raw != "" {
		options.To, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return options, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "to", Code: "invalid_time"}}}
		}
	}
	if options.ActorID != "" && !domain.IsCanonicalUUID(options.ActorID) {
		return options, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "actorId", Code: "invalid_uuid"}}}
	}
	if options.Cursor != "" && !domain.IsCanonicalUUID(options.Cursor) {
		return options, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "cursor", Code: "invalid_cursor"}}}
	}
	if options.From.IsZero() == false && options.To.IsZero() == false && !options.From.Before(options.To) {
		return options, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "to", Code: "invalid_time_range"}}}
	}
	if options.Limit < 1 || options.Limit > 100 {
		return options, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "limit", Code: "invalid_limit"}}}
	}
	if options.Result != "" && options.Result != application.OperationLogSuccess && options.Result != application.OperationLogFailure {
		return options, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "result", Code: "invalid_result"}}}
	}
	return options, nil
}

func operationLogResponse(item application.OperationLog) operationLogResponseBody {
	return operationLogResponseBody{
		ID:               item.ID,
		Actor:            operationActor{Deleted: item.ActorDeleted, ID: item.ActorID, Name: item.ActorName, Email: item.ActorEmail, Kind: item.ActorKind, Label: item.ActorLabel},
		Action:           item.Action,
		ObjectType:       item.ObjectType,
		ObjectID:         item.ObjectID,
		ObjectDeleted:    item.ObjectDeleted,
		Result:           item.Result,
		OccurredAt:       item.OccurredAt.UTC().Format(time.RFC3339Nano),
		SourceIP:         item.SourceIP,
		AttemptedAccount: item.AttemptedAccount,
		Details:          item.Details,
	}
}

func operationLogRetentionResponseBodyFrom(retention application.OperationLogRetention) operationLogRetentionResponse {
	response := operationLogRetentionResponse{RetentionDays: retention.Days, Revision: retention.Revision}
	if !retention.UpdatedAt.IsZero() {
		response.UpdatedAt = retention.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	return response
}

func (h *Handler) recordOperation(r *http.Request, input application.OperationLogInput) {
	if h.operationLogs == nil {
		return
	}
	if input.OccurredAt.IsZero() {
		input.OccurredAt = time.Now().UTC()
	}
	if input.SourceIP == "" {
		input.SourceIP = h.requestSourceIP(r)
	}
	if input.ActorKind == "" {
		if input.ActorID != "" {
			input.ActorKind = "authenticated"
		} else {
			input.ActorKind = "unverified"
		}
	}
	_ = h.operationLogs.Record(r.Context(), input)
}

func (h *Handler) requestSourceIP(r *http.Request) string {
	peer, peerIP := parseSourceAddress(r.RemoteAddr)
	if peerIP == nil {
		return "unknown"
	}
	if !h.trustedProxy(peerIP) {
		return peer
	}

	// Walk X-Forwarded-For from the application-facing side. Once the first
	// valid address outside the configured proxy chain is reached, it is the
	// client identity and everything further left is outside our trust
	// boundary. In particular, an attacker-controlled malformed prefix added
	// before a proxy's client address must not turn an otherwise usable chain
	// into a shared fallback identity. A malformed node encountered before that
	// boundary is conservative: use the immediate peer instead.
	if values := r.Header.Values("X-Forwarded-For"); len(values) > 0 {
		client, valid := forwardedClientIP(values, h.trustedProxy)
		if !valid {
			return peer
		}
		if client != nil {
			return canonicalIP(client)
		}
		// A chain containing only configured proxies gives us no client
		// identity; use the immediate peer instead of guessing.
		return peer
	}

	// X-Real-IP is a single-value fallback only when X-Forwarded-For is
	// absent. Multiple values, an empty value, invalid syntax, or a value that
	// is itself a configured proxy are all rejected to avoid header-merging
	// ambiguity.
	if values := r.Header.Values("X-Real-IP"); len(values) > 0 {
		if len(values) != 1 {
			return peer
		}
		candidate := strings.TrimSpace(values[0])
		parsed := net.ParseIP(candidate)
		if candidate == "" || parsed == nil || h.trustedProxy(parsed) {
			return peer
		}
		return canonicalIP(parsed)
	}
	return peer
}

// forwardedClientIP resolves a complete, ordered set of X-Forwarded-For
// header values from right to left. The bool reports whether every node read
// before the trust boundary was syntactically valid. A valid untrusted node
// ends the scan; nodes to its left are deliberately not parsed.
func forwardedClientIP(values []string, trusted func(net.IP) bool) (net.IP, bool) {
	for valueIndex := len(values) - 1; valueIndex >= 0; valueIndex-- {
		parts := strings.Split(values[valueIndex], ",")
		for partIndex := len(parts) - 1; partIndex >= 0; partIndex-- {
			candidate := strings.TrimSpace(parts[partIndex])
			parsed := net.ParseIP(candidate)
			if candidate == "" || parsed == nil {
				return nil, false
			}
			if !trusted(parsed) {
				return parsed, true
			}
		}
	}
	return nil, true
}

func parseSourceAddress(value string) (string, net.IP) {
	host := remoteHost(value)
	parsed := net.ParseIP(strings.TrimSpace(host))
	if parsed == nil {
		return "unknown", nil
	}
	return canonicalIP(parsed), parsed
}

func canonicalIP(value net.IP) string {
	if ipv4 := value.To4(); ipv4 != nil {
		return ipv4.String()
	}
	return value.String()
}

func (h *Handler) trustedProxy(ip net.IP) bool {
	for _, rawCIDR := range h.cfg.TrustedProxyCIDRs {
		_, network, err := net.ParseCIDR(strings.TrimSpace(rawCIDR))
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func remoteHost(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr)); err == nil {
		return host
	}
	return strings.TrimSpace(remoteAddr)
}

func operationErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, application.ErrAccountDisabled):
		return "account_disabled"
	case errors.Is(err, application.ErrSelfUserOperation):
		return "self_user_operation"
	case errors.Is(err, application.ErrLastSuperAdmin):
		return "last_super_admin"
	case errors.Is(err, application.ErrUserNotFound):
		return "not_found"
	case errors.Is(err, application.ErrForbidden):
		return "forbidden"
	case errors.Is(err, application.ErrUnauthenticated):
		return "unauthenticated"
	case errors.Is(err, application.ErrInvalidCredentials):
		return "invalid_credentials"
	case errors.Is(err, application.ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, application.ErrStaleRevision):
		return "stale_revision"
	case errors.Is(err, application.ErrDependencyUnavailable):
		return "dependency_unavailable"
	case errors.Is(err, application.ErrInvalidMailSettings):
		return "invalid_settings"
	case errors.Is(err, application.ErrInvalidPasswordResetToken):
		return "invalid_token"
	case errors.Is(err, application.ErrInvalidEmailChangeCode):
		return "invalid_email_change_code"
	case errors.Is(err, application.ErrInvalidEmailChange):
		return "invalid_email_change"
	case errors.Is(err, application.ErrEmailChangeExpired):
		return "email_change_expired"
	case errors.Is(err, application.ErrEmailChangeAttemptsExceeded):
		return "email_change_attempts_exceeded"
	case errors.Is(err, application.ErrEmailChangeResendTooSoon):
		return "email_change_resend_too_soon"
	case errors.Is(err, application.ErrAvatarNotFound):
		return "avatar_not_found"
	case errors.Is(err, application.ErrInvitationInvalid):
		return "invalid_invitation"
	case errors.Is(err, application.ErrMailTaskNotFound):
		return "not_found"
	case errors.Is(err, application.ErrMailTaskSending):
		return "mail_task_sending"
	case errors.Is(err, application.ErrMailTaskNotRetryable):
		return "mail_task_not_retryable"
	case errors.Is(err, application.ErrMailSettingsNotSaved):
		return "mail_settings_not_saved"
	case errors.Is(err, application.ErrMailTaskDependency), errors.Is(err, application.ErrMailTaskNotConfigured):
		return "mail_task_dependency"
	default:
		return "operation_failed"
	}
}

func operationFailure(user domain.User, action, objectType, objectID string, err error, details map[string]any) application.OperationLogInput {
	if details == nil {
		details = map[string]any{}
	}
	details["failure"] = operationErrorCode(err)
	return application.OperationLogInput{ActorID: user.ID, ActorName: user.Name, ActorEmail: user.Email, ActorKind: "authenticated", Action: action, ObjectType: objectType, ObjectID: objectID, Result: application.OperationLogFailure, Details: details}
}

func unverifiedFailure(action, objectType, objectID, attempted string, err error) application.OperationLogInput {
	return application.OperationLogInput{ActorKind: "unverified", Action: action, ObjectType: objectType, ObjectID: objectID, Result: application.OperationLogFailure, AttemptedAccount: attempted, Details: map[string]any{"failure": operationErrorCode(err)}}
}

func (h *Handler) snapshotRole(r *http.Request, actorID, roleID string) (domain.Role, bool) {
	auditor, ok := h.access.(application.OperationAuditAccessService)
	if !ok {
		return domain.Role{}, false
	}
	role, err := auditor.SnapshotRole(r.Context(), actorID, roleID)
	return role, err == nil
}

func (h *Handler) snapshotUser(r *http.Request, actorID, userID string) (domain.AccessUser, bool) {
	auditor, ok := h.access.(application.OperationAuditAccessService)
	if !ok {
		return domain.AccessUser{}, false
	}
	user, err := auditor.SnapshotUser(r.Context(), actorID, userID)
	return user, err == nil
}

func (h *Handler) snapshotInvitation(r *http.Request, actorID, invitationID string) (domain.Invitation, bool) {
	auditor, ok := h.access.(application.OperationAuditAccessService)
	if !ok {
		return domain.Invitation{}, false
	}
	invitation, err := auditor.SnapshotInvitation(r.Context(), actorID, invitationID)
	return invitation, err == nil
}

func roleSnapshot(role domain.Role) map[string]any {
	permissions := make([]string, 0, len(role.Permissions))
	for _, permission := range role.Permissions {
		permissions = append(permissions, string(permission))
	}
	return map[string]any{"id": role.ID, "name": role.Name, "description": role.Description, "permissions": permissions, "revision": role.Revision}
}

func accessUserSnapshot(user domain.AccessUser) map[string]any {
	roles := make([]map[string]any, 0, len(user.Roles))
	for _, role := range user.Roles {
		roles = append(roles, roleSnapshot(role))
	}
	return map[string]any{"id": user.User.ID, "name": user.User.Name, "email": user.User.Email, "roleIds": roleIDs(user.Roles), "roles": roles, "authVersion": user.AuthVersion, "disabled": user.User.Disabled}
}

func invitationSnapshot(invitation domain.Invitation) map[string]any {
	roles := make([]map[string]any, 0, len(invitation.Roles))
	for _, role := range invitation.Roles {
		roles = append(roles, roleSnapshot(role))
	}
	return map[string]any{"creator": map[string]any{"id": invitation.CreatedBy, "name": invitation.CreatedByName, "email": invitation.CreatedByEmail, "deleted": invitation.CreatorDeleted}, "id": invitation.ID, "name": invitation.Name, "email": invitation.Email, "roleIds": roleIDs(invitation.Roles), "roles": roles, "expiresAt": invitation.ExpiresAt.UTC().Format(time.RFC3339Nano), "revision": invitation.Revision}
}

func roleIDs(roles []domain.Role) []string {
	ids := make([]string, 0, len(roles))
	for _, role := range roles {
		ids = append(ids, role.ID)
	}
	return ids
}

func emailSettingsSnapshot(view application.EmailSettingsView) map[string]any {
	return map[string]any{
		"configured":     view.Configured,
		"host":           view.Host,
		"port":           view.Port,
		"security":       view.Security,
		"username":       view.Username,
		"passwordSet":    view.PasswordSet,
		"fromAddress":    view.FromAddress,
		"fromName":       view.FromName,
		"defaultLocale":  view.DefaultLocale,
		"autoRetryCount": view.AutoRetryCount,
		"retentionDays":  view.RetentionDays,
		"revision":       view.Revision,
	}
}

func emailPasswordAction(input emailSettingsRequest) string {
	if input.ClearPassword {
		return "cleared"
	}
	if input.Password != nil && *input.Password != "" {
		return "updated"
	}
	return "unchanged"
}
