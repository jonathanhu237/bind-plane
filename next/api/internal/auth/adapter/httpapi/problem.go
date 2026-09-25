package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type problem struct {
	Type   string         `json:"type"`
	Title  string         `json:"title"`
	Status int            `json:"status"`
	Code   string         `json:"code,omitempty"`
	Detail string         `json:"detail,omitempty"`
	Errors []fieldProblem `json:"errors,omitempty"`
}

type fieldProblem struct {
	Pointer string         `json:"pointer"`
	Code    string         `json:"code"`
	Params  map[string]any `json:"params,omitempty"`
}

var problemCatalog = map[string]struct {
	title string
}{
	"invalid-request":              {"Invalid Request"},
	"invalid-credentials":          {"Invalid Credentials"},
	"unauthenticated":              {"Unauthenticated"},
	"forbidden":                    {"Forbidden"},
	"invalid-setup-token":          {"Invalid Setup Token"},
	"invalid-password-reset-token": {"Invalid Password Reset Token"},
	"invalid-invitation":           {"Invalid Invitation"},
	"role-in-use":                  {"Role In Use"},
	"role-immutable":               {"Role Immutable"},
	"last-super-admin":             {"Last Super Administrator"},
	"stale-revision":               {"Stale Revision"},
	"role-already-exists":          {"Role Already Exists"},
	"invitation-pending":           {"Invitation Pending"},
	"not-found":                    {"Not Found"},
	"method-not-allowed":           {"Method Not Allowed"},
	"setup-complete":               {"Setup Complete"},
	"content-too-large":            {"Content Too Large"},
	"unsupported-media-type":       {"Unsupported Media Type"},
	"validation-failed":            {"Validation Failed"},
	"invalid-system-identity":      {"Invalid System Identity"},
	"rate-limited":                 {"Too Many Requests"},
	"internal-error":               {"Internal Server Error"},
	"service-unavailable":          {"Service Unavailable"},
	"mail-not-configured":          {"Mail Service Not Configured"},
	"mail-task-not-found":          {"Mail Task Not Found"},
	"mail-task-sending":            {"Mail Task Is Sending"},
	"mail-task-not-retryable":      {"Mail Task Is Not Retryable"},
	"mail-settings-not-saved":      {"Mail Settings Not Saved"},
	"avatar-not-found":             {"Avatar Not Found"},
	"permission-scope":             {"Permission Outside Actor Scope"},
}

func writeProblem(w http.ResponseWriter, status int, name string) {
	writeProblemWithCode(w, status, name, "", "", nil)
}

func writeProblemWithCode(w http.ResponseWriter, status int, name, code, detail string, fields []domain.FieldError) {
	catalog, ok := problemCatalog[name]
	if !ok {
		name = "internal-error"
		catalog = problemCatalog[name]
		status = http.StatusInternalServerError
		code, detail, fields = "", "", nil
	}
	body := problem{Type: "/problems/" + name, Title: catalog.title, Status: status, Code: code, Detail: detail}
	for _, field := range fields {
		body.Errors = append(body.Errors, fieldProblem{Pointer: "/" + field.Field, Code: field.Code, Params: field.Params})
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeApplicationError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case isValidation(err):
		writeProblemWithCode(w, http.StatusUnprocessableEntity, "validation-failed", "validation_failed", "", validationFields(err))
	case applicationError(err, application.ErrInvalidCredentials):
		writeProblem(w, http.StatusUnauthorized, "invalid-credentials")
	case applicationError(err, application.ErrAccountDisabled):
		writeProblemWithCode(w, http.StatusUnauthorized, "unauthenticated", "account_disabled", "", nil)
	case applicationError(err, application.ErrSelfUserOperation):
		writeProblemWithCode(w, http.StatusForbidden, "forbidden", "self_user_operation", "", nil)
	case applicationError(err, application.ErrUnauthenticated):
		writeProblem(w, http.StatusUnauthorized, "unauthenticated")
	case applicationError(err, application.ErrInvalidSetupToken):
		writeProblem(w, http.StatusForbidden, "invalid-setup-token")
	case applicationError(err, application.ErrInvalidPasswordResetToken):
		writeProblem(w, http.StatusForbidden, "invalid-password-reset-token")
	case applicationError(err, application.ErrInvitationInvalid):
		writeProblem(w, http.StatusForbidden, "invalid-invitation")
	case applicationError(err, application.ErrInvitationRoleForbidden):
		writeProblemWithCode(w, http.StatusForbidden, "forbidden", "invitation_role_forbidden", "", nil)
	case applicationError(err, application.ErrInvitationNotManageable):
		writeProblemWithCode(w, http.StatusForbidden, "forbidden", "invitation_not_manageable", "", nil)
	case applicationError(err, application.ErrSetupComplete):
		writeProblem(w, http.StatusConflict, "setup-complete")
	case applicationError(err, application.ErrEmailAlreadyRegistered):
		writeProblemWithCode(w, http.StatusUnprocessableEntity, "validation-failed", "validation_failed", "", []domain.FieldError{{Field: "email", Code: "email_already_registered"}})
	case applicationError(err, application.ErrInvalidEmailChangeCode):
		writeProblemWithCode(w, http.StatusUnprocessableEntity, "validation-failed", "invalid_email_change_code", "", []domain.FieldError{{Field: "code", Code: "invalid_code"}})
	case applicationError(err, application.ErrInvalidEmailChange), applicationError(err, application.ErrEmailChangeExpired):
		writeProblemWithCode(w, http.StatusUnprocessableEntity, "validation-failed", "invalid_email_change", "", nil)
	case applicationError(err, application.ErrEmailChangeAttemptsExceeded):
		writeProblemWithCode(w, http.StatusUnprocessableEntity, "validation-failed", "email_change_attempts_exceeded", "", nil)
	case applicationError(err, application.ErrEmailChangeResendTooSoon):
		writeProblemWithCode(w, http.StatusTooManyRequests, "rate-limited", "email_change_resend_too_soon", "", nil)
	case applicationError(err, application.ErrAvatarNotFound):
		writeProblem(w, http.StatusNotFound, "avatar-not-found")
	case applicationError(err, application.ErrRateLimited):
		writeProblemWithCode(w, http.StatusTooManyRequests, "rate-limited", "rate_limited", "", nil)
	case applicationError(err, application.ErrDependencyUnavailable), applicationError(err, application.ErrPasswordHashBusy), applicationError(err, application.ErrMailTaskDependency), applicationError(err, application.ErrMailTaskNotConfigured):
		writeProblem(w, http.StatusServiceUnavailable, "service-unavailable")
	case applicationError(err, application.ErrMailNotConfigured):
		writeProblemWithCode(w, http.StatusServiceUnavailable, "mail-not-configured", "mail_not_configured", "", nil)
	case applicationError(err, application.ErrMailSettingsNotSaved):
		writeProblemWithCode(w, http.StatusConflict, "mail-settings-not-saved", "mail_settings_not_saved", "", nil)
	case applicationError(err, application.ErrMailTaskNotFound):
		writeProblem(w, http.StatusNotFound, "mail-task-not-found")
	case applicationError(err, application.ErrMailTaskSending):
		writeProblemWithCode(w, http.StatusConflict, "mail-task-sending", "mail_task_sending", "", nil)
	case applicationError(err, application.ErrMailTaskNotRetryable):
		writeProblemWithCode(w, http.StatusConflict, "mail-task-not-retryable", "mail_task_not_retryable", "", nil)
	case applicationError(err, application.ErrPermissionScope):
		writeProblemWithCode(w, http.StatusForbidden, "permission-scope", "permission_scope_forbidden", "", nil)
	case applicationError(err, application.ErrInvalidMailSettings):
		writeProblemWithCode(w, http.StatusUnprocessableEntity, "validation-failed", "invalid_mail_settings", "", nil)
	case applicationError(err, application.ErrInvalidSystemIdentity):
		writeProblemWithCode(w, http.StatusUnprocessableEntity, "invalid-system-identity", "invalid_system_identity", "", nil)
	case applicationError(err, application.ErrForbidden):
		writeProblem(w, http.StatusForbidden, "forbidden")
	case applicationError(err, application.ErrRoleNotFound), applicationError(err, application.ErrUserNotFound), applicationError(err, application.ErrInvitationNotFound), applicationError(err, application.ErrOperationLogNotFound), applicationError(err, application.ErrEmailChangeNotFound):
		writeProblem(w, http.StatusNotFound, "not-found")
	case applicationError(err, application.ErrRoleInUse):
		writeProblemWithCode(w, http.StatusConflict, "role-in-use", "role_in_use", "", nil)
	case applicationError(err, application.ErrImmutableRole):
		writeProblemWithCode(w, http.StatusConflict, "role-immutable", "role_immutable", "", nil)
	case applicationError(err, application.ErrLastSuperAdmin):
		writeProblemWithCode(w, http.StatusConflict, "last-super-admin", "last_super_admin", "", nil)
	case applicationError(err, application.ErrStaleRevision):
		writeProblemWithCode(w, http.StatusConflict, "stale-revision", "stale_revision", "", nil)
	case applicationError(err, application.ErrRoleAlreadyExists):
		writeProblemWithCode(w, http.StatusConflict, "role-already-exists", "role_already_exists", "", nil)
	case applicationError(err, application.ErrInvitationPending):
		writeProblemWithCode(w, http.StatusConflict, "invitation-pending", "invitation_pending", "", nil)
	case applicationError(err, application.ErrInvalidRoleSet):
		writeProblemWithCode(w, http.StatusUnprocessableEntity, "validation-failed", "validation_failed", "", []domain.FieldError{{Field: "roleIds", Code: "invalid_role_set"}})
	default:
		writeProblem(w, http.StatusInternalServerError, "internal-error")
	}
}

func isValidation(err error) bool {
	var validationErr *domain.ValidationErrors
	return errors.As(err, &validationErr)
}

func validationFields(err error) []domain.FieldError {
	var validationErr *domain.ValidationErrors
	if errors.As(err, &validationErr) {
		return validationErr.Items
	}
	return nil
}

func applicationError(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		type unwrapper interface{ Unwrap() error }
		unwrapped, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = unwrapped.Unwrap()
	}
	return false
}
