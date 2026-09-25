package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	"github.com/jonathanhu237/bind-plane/next/api/internal/config"
)

type SetupService interface {
	Status(context.Context) (application.SetupStatus, error)
	Complete(context.Context, application.SetupInput) (domain.User, error)
}

type AuthenticationService interface {
	Login(context.Context, application.LoginInput) (domain.User, string, error)
	Current(context.Context, string) (domain.User, error)
	Logout(context.Context, string) error
}

type PasswordRecoveryService interface {
	Request(context.Context, application.PasswordResetRequestInput) error
	Complete(context.Context, application.PasswordResetCompleteInput) error
}

type PasswordRecoveryAuditService interface {
	CompleteWithTarget(context.Context, application.PasswordResetCompleteInput) (domain.User, error)
}

type PersonalSettingsService interface {
	Profile(context.Context, string) (domain.User, error)
	UpdateName(context.Context, string, string) (domain.User, error)
	UpdateLocale(context.Context, string, domain.Locale) (domain.User, error)
	SaveAvatar(context.Context, string, []byte, string) (domain.User, error)
	RemoveAvatar(context.Context, string) (domain.User, error)
	Avatar(context.Context, string) (domain.Avatar, error)
	ChangePassword(context.Context, string, string, string) (domain.User, time.Time, error)
	EmailChangeStatus(context.Context, string) (domain.EmailChangeRequest, error)
	RequestEmailChange(context.Context, string, string, string) (domain.EmailChangeRequest, error)
	ResendEmailChange(context.Context, string) (domain.EmailChangeRequest, error)
	CompleteEmailChange(context.Context, string, string, string) (domain.User, time.Time, error)
}

type InvitationAcceptanceSourceService interface {
	CompleteWithTargetAndSource(context.Context, string, string, string) (domain.Invitation, error)
}

type AccessService interface {
	Roles(context.Context, string) (application.RolePage, error)
	RoleOptions(context.Context, string) ([]application.RoleOption, error)
	Role(context.Context, string, string) (domain.Role, error)
	CreateRole(context.Context, string, application.RoleMutationInput) (domain.Role, error)
	ReplaceRole(context.Context, string, string, application.RoleMutationInput) (domain.Role, error)
	DeleteRole(context.Context, string, string) error
	Users(context.Context, string, string, int) (application.UserPage, error)
	ReplaceUserRoles(context.Context, string, string, application.AssignmentInput) (domain.AccessUser, error)
	CreateInvitation(context.Context, string, application.InvitationInput) (domain.Invitation, error)
	Invitations(context.Context, string, string, int) (application.InvitationPage, error)
	ResendInvitation(context.Context, string, string) (domain.Invitation, error)
	RevokeInvitation(context.Context, string, string) error
}

type versionedLifecycleService interface {
	DeactivateUserWithRevision(context.Context, string, string, int64) (domain.AccessUser, error)
	ReactivateUserWithRevision(context.Context, string, string, int64) (domain.AccessUser, error)
	DeleteUserWithRevision(context.Context, string, string, int64) error
}

type SettingsService interface {
	GetEmailSettings(context.Context) (application.EmailSettingsView, error)
	SaveEmailSettings(context.Context, application.EmailSettingsInput) (application.EmailSettingsView, error)
	OperationalWarnings(context.Context) ([]application.OperationalWarning, error)
}

type TestEmailSettingsTaskService interface {
	TestEmailSettingsTask(context.Context, string, application.EmailSettingsInput, string) (application.MailTask, error)
}

type MailTaskService interface {
	List(context.Context, string, application.MailTaskListOptions) (application.MailTaskPage, error)
	Detail(context.Context, string, string) (application.MailTask, error)
	Retry(context.Context, string, string) (application.MailTask, error)
	Delete(context.Context, string, string) error
	BulkRetry(context.Context, string, []string) (application.MailTaskBatchResult, error)
	BulkDelete(context.Context, string, []string) (application.MailTaskBatchResult, error)
}

type MailTaskOwnerStatusService interface {
	OwnStatus(context.Context, string, string) (application.MailTask, error)
}

type SystemIdentityService interface {
	application.SystemIdentityProvider
	GetSystemIdentity(context.Context) (application.SystemIdentityView, error)
	SaveSystemIdentity(context.Context, application.SystemIdentityInput) (application.SystemIdentityView, error)
}

type OperationLogService interface {
	Record(context.Context, application.OperationLogInput) error
	List(context.Context, application.OperationLogListOptions) (application.OperationLogPage, error)
	Detail(context.Context, string) (application.OperationLog, error)
	RecordingStatus(time.Time) application.OperationLogRecordingStatus
	Retention(context.Context) (application.OperationLogRetention, error)
	SaveRetention(context.Context, int64, int) (application.OperationLogRetention, error)
}

type QueryableAccessService interface {
	UsersWithOptions(context.Context, string, application.AccessListOptions) (application.UserPage, error)
	InvitationsWithOptions(context.Context, string, application.AccessListOptions) (application.InvitationPage, error)
}

type InvitationAcceptanceService interface {
	Complete(context.Context, string, string) error
}

type InvitationAcceptanceAuditService interface {
	CompleteWithTarget(context.Context, string, string) (domain.Invitation, error)
}

type Handler struct {
	setup            SetupService
	auth             AuthenticationService
	recovery         PasswordRecoveryService
	access           AccessService
	acceptInvitation InvitationAcceptanceService
	settings         SettingsService
	identity         SystemIdentityService
	operationLogs    OperationLogService
	personal         PersonalSettingsService
	mailTasks        MailTaskService
	cfg              config.Config
	mux              *http.ServeMux
}

func NewHandler(setup SetupService, auth AuthenticationService, cfg config.Config, recovery ...PasswordRecoveryService) http.Handler {
	return newHandler(setup, auth, cfg, firstRecovery(recovery), nil, nil)
}

func NewHandlerWithAccess(setup SetupService, auth AuthenticationService, cfg config.Config, recovery PasswordRecoveryService, access AccessService, accept InvitationAcceptanceService, settings ...SettingsService) http.Handler {
	var service SettingsService
	if len(settings) > 0 {
		service = settings[0]
	}
	return newHandler(setup, auth, cfg, recovery, access, accept, service)
}

// NewHandlerWithAccessAndMailTasks is the compact constructor for embedders
// that have settings and access services but no operation-log or identity
// service. The mail-task capability remains independently authorization gated.
func NewHandlerWithAccessAndMailTasks(setup SetupService, auth AuthenticationService, cfg config.Config, recovery PasswordRecoveryService, access AccessService, accept InvitationAcceptanceService, settings SettingsService, mailTasks MailTaskService) http.Handler {
	return newHandlerWithOperationLogAndIdentityAndPersonal(setup, auth, cfg, recovery, access, accept, settings, nil, nil, nil, mailTasks)
}

// NewHandlerWithMailTasks is the health/auth constructor with only the mail
// task capability added.
func NewHandlerWithMailTasks(setup SetupService, auth AuthenticationService, cfg config.Config, recovery PasswordRecoveryService, mailTasks MailTaskService) http.Handler {
	return newHandlerWithOperationLogAndIdentityAndPersonal(setup, auth, cfg, recovery, nil, nil, nil, nil, nil, nil, mailTasks)
}

// NewHandlerWithAccessAndOperationLog extends the access handler without
// changing the constructor used by embedders that do not persist history.
func NewHandlerWithAccessAndOperationLog(setup SetupService, auth AuthenticationService, cfg config.Config, recovery PasswordRecoveryService, access AccessService, accept InvitationAcceptanceService, settings SettingsService, operations OperationLogService, mailTasks ...MailTaskService) http.Handler {
	return newHandlerWithOperationLog(setup, auth, cfg, recovery, access, accept, settings, operations, mailTasks...)
}

// NewHandlerWithAccessAndOperationLogAndIdentity adds the shared public and
// protected system identity endpoints while preserving the older constructor
// seams used by embedders and tests.
func NewHandlerWithAccessAndOperationLogAndIdentity(setup SetupService, auth AuthenticationService, cfg config.Config, recovery PasswordRecoveryService, access AccessService, accept InvitationAcceptanceService, settings SettingsService, operations OperationLogService, identity SystemIdentityService, mailTasks ...MailTaskService) http.Handler {
	return newHandlerWithOperationLogAndIdentity(setup, auth, cfg, recovery, access, accept, settings, operations, identity, mailTasks...)
}

// NewHandlerWithAccessAndOperationLogAndIdentityAndPersonalSettings adds the
// authenticated self-service account settings surface without changing any
// older constructor signatures.
func NewHandlerWithAccessAndOperationLogAndIdentityAndPersonalSettings(setup SetupService, auth AuthenticationService, cfg config.Config, recovery PasswordRecoveryService, access AccessService, accept InvitationAcceptanceService, settings SettingsService, operations OperationLogService, identity SystemIdentityService, personal PersonalSettingsService, mailTasks ...MailTaskService) http.Handler {
	return newHandlerWithOperationLogAndIdentityAndPersonal(setup, auth, cfg, recovery, access, accept, settings, operations, identity, personal, mailTasks...)
}

func firstRecovery(recovery []PasswordRecoveryService) PasswordRecoveryService {
	var passwordRecovery PasswordRecoveryService
	if len(recovery) > 0 {
		passwordRecovery = recovery[0]
	}
	return passwordRecovery
}

func newHandler(setup SetupService, auth AuthenticationService, cfg config.Config, recovery PasswordRecoveryService, access AccessService, accept InvitationAcceptanceService, settings ...SettingsService) http.Handler {
	var settingsService SettingsService
	if len(settings) > 0 {
		settingsService = settings[0]
	}
	return newHandlerWithOperationLog(setup, auth, cfg, recovery, access, accept, settingsService, nil)
}

func newHandlerWithOperationLog(setup SetupService, auth AuthenticationService, cfg config.Config, recovery PasswordRecoveryService, access AccessService, accept InvitationAcceptanceService, settings SettingsService, operations OperationLogService, mailTasks ...MailTaskService) http.Handler {
	return newHandlerWithOperationLogAndIdentity(setup, auth, cfg, recovery, access, accept, settings, operations, nil, mailTasks...)
}

func newHandlerWithOperationLogAndIdentity(setup SetupService, auth AuthenticationService, cfg config.Config, recovery PasswordRecoveryService, access AccessService, accept InvitationAcceptanceService, settings SettingsService, operations OperationLogService, identity SystemIdentityService, mailTasks ...MailTaskService) http.Handler {
	return newHandlerWithOperationLogAndIdentityAndPersonal(setup, auth, cfg, recovery, access, accept, settings, operations, identity, nil, mailTasks...)
}

func newHandlerWithOperationLogAndIdentityAndPersonal(setup SetupService, auth AuthenticationService, cfg config.Config, recovery PasswordRecoveryService, access AccessService, accept InvitationAcceptanceService, settings SettingsService, operations OperationLogService, identity SystemIdentityService, personal PersonalSettingsService, mailTasks ...MailTaskService) http.Handler {
	var mailTaskService MailTaskService
	if len(mailTasks) > 0 {
		mailTaskService = mailTasks[0]
	}
	h := &Handler{setup: setup, auth: auth, recovery: recovery, access: access, acceptInvitation: accept, settings: settings, identity: identity, operationLogs: operations, personal: personal, mailTasks: mailTaskService, cfg: cfg, mux: http.NewServeMux()}
	if h.identity != nil {
		h.mux.HandleFunc("GET /api/public/system-identity", h.publicSystemIdentity)
		h.mux.HandleFunc("GET /api/public/system-identity/icon", h.publicSystemIdentityIcon)
		h.mux.HandleFunc("GET /api/settings/system-identity", h.systemIdentity)
		h.mux.HandleFunc("PUT /api/settings/system-identity", h.saveSystemIdentity)
	}
	h.mux.HandleFunc("GET /api/setup/status", h.setupStatus)
	h.mux.HandleFunc("POST /api/setup", h.setupComplete)
	h.mux.HandleFunc("POST /api/auth/login", h.login)
	h.mux.HandleFunc("GET /api/auth/me", h.me)
	if h.personal != nil {
		h.mux.HandleFunc("PATCH /api/auth/me", h.personalProfileUpdate)
	}
	h.mux.HandleFunc("GET /api/auth/session-status", h.sessionStatus)
	if h.personal != nil {
		h.mux.HandleFunc("GET /api/auth/me/profile", h.personalProfile)
		h.mux.HandleFunc("PUT /api/auth/me/profile", h.personalProfileUpdate)
		h.mux.HandleFunc("PATCH /api/auth/me/profile", h.personalProfileUpdate)
		h.mux.HandleFunc("PUT /api/auth/me/preferences", h.personalPreferencesUpdate)
		h.mux.HandleFunc("PUT /api/auth/me/password", h.personalPasswordUpdate)
		h.mux.HandleFunc("POST /api/auth/me/email-change", h.personalEmailChangeRequest)
		h.mux.HandleFunc("GET /api/auth/me/email-change", h.personalEmailChangeStatus)
		h.mux.HandleFunc("POST /api/auth/me/email-change/resend", h.personalEmailChangeResend)
		h.mux.HandleFunc("POST /api/auth/me/email-change/verify", h.personalEmailChangeVerify)
		h.mux.HandleFunc("PUT /api/auth/me/avatar", h.personalAvatarUpload)
		h.mux.HandleFunc("DELETE /api/auth/me/avatar", h.personalAvatarDelete)
		h.mux.HandleFunc("GET /api/users/{id}/avatar", h.personalAvatarRead)
		h.mux.HandleFunc("GET /api/auth/me/avatar", h.personalAvatarReadSelf)
	}
	if _, ok := auth.(onlineService); ok {
		h.mux.HandleFunc("GET /api/online-users", h.onlineUsers)
		h.mux.HandleFunc("POST /api/online-users/{id}/kick", h.kickUser)
	}
	h.mux.HandleFunc("POST /api/auth/logout", h.logout)
	if h.recovery != nil {
		h.mux.HandleFunc("POST /api/auth/password-reset/request", h.passwordResetRequest)
		h.mux.HandleFunc("POST /api/auth/password-reset/complete", h.passwordResetComplete)
	}
	if h.access != nil {
		h.mux.HandleFunc("GET /api/roles", h.roles)
		h.mux.HandleFunc("GET /api/access/role-options", h.roleOptions)
		h.mux.HandleFunc("GET /api/roles/{id}", h.role)
		h.mux.HandleFunc("POST /api/roles", h.createRole)
		h.mux.HandleFunc("PUT /api/roles/{id}", h.replaceRole)
		h.mux.HandleFunc("DELETE /api/roles/{id}", h.deleteRole)
		h.mux.HandleFunc("GET /api/users", h.users)
		h.mux.HandleFunc("PUT /api/users/{id}/roles", h.replaceUserRoles)
		h.mux.HandleFunc("POST /api/users/{id}/deactivate", h.deactivateUser)
		h.mux.HandleFunc("POST /api/users/{id}/reactivate", h.reactivateUser)
		h.mux.HandleFunc("DELETE /api/users/{id}", h.deleteUser)
		h.mux.HandleFunc("GET /api/user-invitations", h.invitations)
		h.mux.HandleFunc("POST /api/user-invitations", h.createInvitation)
		h.mux.HandleFunc("POST /api/user-invitations/{id}/resend", h.resendInvitation)
		h.mux.HandleFunc("DELETE /api/user-invitations/{id}", h.revokeInvitation)
	}
	if h.settings != nil {
		h.mux.HandleFunc("GET /api/settings/email", h.emailSettings)
		h.mux.HandleFunc("PUT /api/settings/email", h.saveEmailSettings)
		h.mux.HandleFunc("POST /api/settings/email/test", h.testEmailSettings)
		h.mux.HandleFunc("GET /api/operational-warnings", h.operationalWarnings)
	}
	if h.operationLogs != nil {
		h.mux.HandleFunc("GET /api/operation-logs", h.operationLogsList)
		h.mux.HandleFunc("GET /api/operation-logs/{id}", h.operationLogsDetail)
		h.mux.HandleFunc("GET /api/operation-logs/status", h.operationLogsStatus)
	}
	if h.mailTasks != nil {
		h.mux.HandleFunc("GET /api/mail-tasks", h.mailTasksList)
		h.mux.HandleFunc("GET /api/mail-tasks/{id}", h.mailTaskDetail)
		h.mux.HandleFunc("GET /api/mail-tasks/{id}/status", h.mailTaskOwnerStatus)
		h.mux.HandleFunc("POST /api/mail-tasks/{id}/retry", h.mailTaskRetry)
		h.mux.HandleFunc("DELETE /api/mail-tasks/{id}", h.mailTaskDelete)
		h.mux.HandleFunc("POST /api/mail-tasks/bulk-retry", h.mailTasksBulkRetry)
		h.mux.HandleFunc("POST /api/mail-tasks/bulk-delete", h.mailTasksBulkDelete)
		// Aliases keep the collection action shape available to clients that
		// submit one JSON body for a bulk operation.
		h.mux.HandleFunc("POST /api/mail-tasks/retry", h.mailTasksBulkRetry)
		h.mux.HandleFunc("POST /api/mail-tasks/delete", h.mailTasksBulkDelete)
		h.mux.HandleFunc("GET /api/settings/email/test/{id}", h.mailTaskOwnerStatus)
	}
	if h.operationLogs != nil && h.settings != nil {
		h.mux.HandleFunc("GET /api/settings/operation-log", h.operationLogRetention)
		h.mux.HandleFunc("PUT /api/settings/operation-log", h.saveOperationLogRetention)
		h.mux.HandleFunc("GET /api/settings/operation-log-retention", h.operationLogRetention)
		h.mux.HandleFunc("PUT /api/settings/operation-log-retention", h.saveOperationLogRetention)
	}
	if h.acceptInvitation != nil {
		h.mux.HandleFunc("POST /api/auth/invitations/accept", h.acceptInvitationHandler)
	}
	h.mux.HandleFunc("/api/", h.notFound)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api" {
		w.Header().Set("Cache-Control", "no-store")
		writeProblem(w, http.StatusNotFound, "not-found")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
		if expected, ok := expectedMethods(r.URL.Path); ok && !methodAllowed(expected, r.Method) {
			w.Header().Set("Allow", expected)
			writeProblem(w, http.StatusMethodNotAllowed, "method-not-allowed")
			return
		}
	}
	h.mux.ServeHTTP(w, r)
}

func methodMatches(expected, actual string) bool {
	return expected == actual || (expected == http.MethodGet && actual == http.MethodHead)
}

var knownMethods = map[string]string{
	"/api/setup/status":                     "GET",
	"/api/setup":                            "POST",
	"/api/auth/login":                       "POST",
	"/api/auth/me":                          "GET, PATCH",
	"/api/auth/me/profile":                  "GET, PUT, PATCH",
	"/api/auth/me/preferences":              "PUT",
	"/api/auth/me/password":                 "PUT",
	"/api/auth/me/email-change":             "GET, POST",
	"/api/auth/me/email-change/resend":      "POST",
	"/api/auth/me/email-change/verify":      "POST",
	"/api/auth/me/avatar":                   "GET, PUT, DELETE",
	"/api/auth/session-status":              "GET",
	"/api/auth/logout":                      "POST",
	"/api/auth/password-reset/request":      "POST",
	"/api/auth/password-reset/complete":     "POST",
	"/api/roles":                            "GET, POST",
	"/api/access/role-options":              "GET",
	"/api/users":                            "GET",
	"/api/online-users":                     "GET",
	"/api/user-invitations":                 "GET, POST",
	"/api/settings/email":                   "GET, PUT",
	"/api/settings/email/test":              "POST",
	"/api/settings/email/test/{id}":         "GET",
	"/api/mail-tasks/{id}/status":           "GET",
	"/api/mail-tasks":                       "GET",
	"/api/mail-tasks/bulk-retry":            "POST",
	"/api/mail-tasks/bulk-delete":           "POST",
	"/api/mail-tasks/retry":                 "POST",
	"/api/mail-tasks/delete":                "POST",
	"/api/public/system-identity":           "GET",
	"/api/public/system-identity/icon":      "GET",
	"/api/settings/system-identity":         "GET, PUT",
	"/api/operational-warnings":             "GET",
	"/api/operation-logs":                   "GET",
	"/api/operation-logs/status":            "GET",
	"/api/users/{id}/avatar":                "GET",
	"/api/settings/operation-log":           "GET, PUT",
	"/api/settings/operation-log-retention": "GET, PUT",
	"/api/auth/invitations/accept":          "POST",
}

func expectedMethods(path string) (string, bool) {
	if expected, ok := knownMethods[path]; ok {
		return expected, true
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 3 && parts[0] == "api" {
		switch parts[1] {
		case "roles":
			return "GET, PUT, DELETE", true
		case "user-invitations":
			return "DELETE", true
		case "mail-tasks":
			return "GET, DELETE", true
		}
	}
	if len(parts) == 3 && parts[0] == "api" && parts[1] == "operation-logs" {
		return "GET", true
	}
	if len(parts) == 4 && parts[0] == "api" && parts[1] == "mail-tasks" && parts[3] == "status" {
		return "GET", true
	}
	if len(parts) == 4 && parts[0] == "api" && parts[1] == "mail-tasks" && parts[3] == "retry" {
		return "POST", true
	}
	if len(parts) == 3 && parts[0] == "api" && parts[1] == "settings" && (parts[2] == "operation-log" || parts[2] == "operation-log-retention") {
		return "GET, PUT", true
	}
	if len(parts) == 4 && parts[0] == "api" && parts[1] == "user-invitations" && parts[3] == "resend" {
		return "POST", true
	}
	if len(parts) == 3 && parts[0] == "api" && parts[1] == "users" {
		return "DELETE", true
	}
	if len(parts) == 4 && parts[0] == "api" && parts[1] == "users" && parts[3] == "avatar" {
		return "GET", true
	}
	if len(parts) == 4 && parts[0] == "api" && parts[1] == "users" && (parts[3] == "deactivate" || parts[3] == "reactivate") {
		return "POST", true
	}
	if len(parts) == 4 && parts[0] == "api" && parts[1] == "users" && parts[3] == "roles" {
		return "PUT", true
	}
	if len(parts) == 4 && parts[0] == "api" && parts[1] == "online-users" && parts[3] == "kick" {
		return "POST", true
	}
	if len(parts) == 4 && parts[0] == "api" && parts[1] == "settings" && parts[2] == "email" && parts[3] == "test" {
		return "POST", true
	}
	if len(parts) == 5 && parts[0] == "api" && parts[1] == "settings" && parts[2] == "email" && parts[3] == "test" {
		return "GET", true
	}
	return "", false
}

func methodAllowed(expected, actual string) bool {
	for _, method := range strings.Split(expected, ",") {
		if methodMatches(strings.TrimSpace(method), actual) {
			return true
		}
	}
	return false
}

func (h *Handler) setupStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.setup.Status(r.Context())
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": string(status)})
}

func (h *Handler) setupComplete(w http.ResponseWriter, r *http.Request) {
	if h.operationOriginFailure(w, r, "auth.setup.complete", "account") {
		return
	}
	var input application.SetupInput
	if err := decodeJSONObject(r, &input, map[string]struct{}{"token": {}, "name": {}, "email": {}, "password": {}, "locale": {}}); err != nil {
		var fieldErr fieldValueError
		if errors.As(err, &fieldErr) && fieldErr.field == "token" {
			h.recordOperation(r, unverifiedFailure("auth.setup.complete", "account", "", "", err))
			writeProblem(w, http.StatusForbidden, "invalid-setup-token")
			return
		}
		h.recordOperation(r, unverifiedFailure("auth.setup.complete", "account", "", "", err))
		writeDecodeError(w, err)
		return
	}
	input.SourceIP = h.requestSourceIP(r)
	if !input.Locale.Valid() {
		input.Locale = requestLocale(r)
	}
	created, err := h.setup.Complete(r.Context(), input)
	if err != nil {
		h.recordOperation(r, application.OperationLogInput{Action: "auth.setup.complete", ObjectType: "account", Result: application.OperationLogFailure, AttemptedAccount: input.Email, Details: map[string]any{"failure": operationErrorCode(err)}})
		writeApplicationError(w, err)
		return
	}
	details := map[string]any{"accountCreated": true}
	if created.ID != "" {
		details["target"] = map[string]any{"id": created.ID, "name": created.Name, "email": created.Email}
	}
	h.recordOperation(r, application.OperationLogInput{ActorKind: "system", Action: "auth.setup.complete", ObjectType: "account", ObjectID: created.ID, Result: application.OperationLogSuccess, Details: details})
	writeJSON(w, http.StatusCreated, map[string]string{"status": string(application.SetupComplete)})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	if h.operationOriginFailure(w, r, "auth.login", "session") {
		return
	}
	var input application.LoginInput
	if err := decodeJSONObject(r, &input, map[string]struct{}{"email": {}, "password": {}, "locale": {}}); err != nil {
		h.recordOperation(r, unverifiedFailure("auth.login", "session", "", "", err))
		writeDecodeError(w, err)
		return
	}
	if !input.Locale.Valid() {
		input.Locale = requestLocale(r)
	}
	var user domain.User
	var principal domain.Principal
	var sessionID string
	var err error
	if enriched, ok := h.auth.(application.PrincipalAuthenticationService); ok {
		input.SourceIP = h.requestSourceIP(r)
		principal, sessionID, err = enriched.LoginWithPrincipal(r.Context(), input)
		user = principal.User
	} else {
		input.SourceIP = h.requestSourceIP(r)
		user, sessionID, err = h.auth.Login(r.Context(), input)
	}
	if err != nil {
		h.recordOperation(r, application.OperationLogInput{Action: "auth.login", ObjectType: "session", Result: application.OperationLogFailure, AttemptedAccount: input.Email, Details: map[string]any{"failure": operationErrorCode(err)}})
		writeApplicationError(w, err)
		return
	}
	actorID := user.ID
	if principal.User.ID != "" {
		actorID = principal.User.ID
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: actorID, ActorName: user.Name, ActorEmail: user.Email, ActorKind: "authenticated", Action: "auth.login", ObjectType: "session", Result: application.OperationLogSuccess, Details: map[string]any{"sessionCreated": true}})
	h.setSessionCookie(w, sessionID)
	if principal.User.ID != "" {
		writeJSON(w, http.StatusOK, principalResponse(principal))
		return
	}
	writeJSON(w, http.StatusOK, userResponse(user))
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(h.cfg.CookieName)
	if err != nil {
		writeProblem(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	if enriched, ok := h.auth.(application.PrincipalAuthenticationService); ok {
		principal, err := enriched.CurrentPrincipal(r.Context(), cookie.Value)
		if err != nil {
			writeApplicationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, principalResponse(principal))
		return
	}
	user, err := h.auth.Current(r.Context(), cookie.Value)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, userResponse(user))
}

// sessionStatus is deliberately separate from /api/auth/me. The latter is a
// normal authenticated request and renews the session; this endpoint is used
// by the periodic browser probe and must only inspect session state.
func (h *Handler) sessionStatus(w http.ResponseWriter, r *http.Request) {
	if _, err := h.currentPrincipalNoTouch(r); err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if h.operationOriginFailure(w, r, "auth.logout", "session") {
		return
	}
	cookie, err := r.Cookie(h.cfg.CookieName)
	if err == nil {
		actor := domain.User{}
		verifiedActor := false
		if principal, principalErr := h.currentPrincipal(r); principalErr == nil {
			actor = principal.User
			verifiedActor = true
		}
		if err := h.auth.Logout(r.Context(), cookie.Value); err != nil {
			input := unverifiedFailure("auth.logout", "session", "", "", err)
			if verifiedActor {
				input = operationFailure(actor, "auth.logout", "session", "", err, nil)
			}
			h.recordOperation(r, input)
			writeApplicationError(w, err)
			return
		}
		input := application.OperationLogInput{ActorKind: "unverified", Action: "auth.logout", ObjectType: "session", Result: application.OperationLogSuccess, Details: map[string]any{"sessionRevoked": true}}
		if verifiedActor {
			input = application.OperationLogInput{ActorID: actor.ID, ActorName: actor.Name, ActorEmail: actor.Email, ActorKind: "authenticated", Action: "auth.logout", ObjectType: "session", Result: application.OperationLogSuccess, Details: map[string]any{"sessionRevoked": true}}
		}
		h.recordOperation(r, input)
	}
	if err != nil {
		h.recordOperation(r, unverifiedFailure("auth.logout", "session", "", "", application.ErrUnauthenticated))
	}
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) passwordResetRequest(w http.ResponseWriter, r *http.Request) {
	if h.operationOriginFailure(w, r, "auth.password_reset.request", "password_reset") {
		return
	}
	var input application.PasswordResetRequestInput
	if err := decodeJSONObject(r, &input, map[string]struct{}{"email": {}}); err != nil {
		h.recordOperation(r, unverifiedFailure("auth.password_reset.request", "password_reset", "", "", err))
		writeDecodeError(w, err)
		return
	}
	input.SourceIP = h.requestSourceIP(r)
	if err := h.recovery.Request(r.Context(), input); err != nil {
		h.recordOperation(r, application.OperationLogInput{Action: "auth.password_reset.request", ObjectType: "password_reset", Result: application.OperationLogFailure, AttemptedAccount: input.Email, Details: map[string]any{"failure": operationErrorCode(err)}})
		writeApplicationError(w, err)
		return
	}
	h.recordOperation(r, application.OperationLogInput{Action: "auth.password_reset.request", ObjectType: "password_reset", Result: application.OperationLogSuccess, AttemptedAccount: input.Email, Details: map[string]any{"requestAccepted": true}})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *Handler) passwordResetComplete(w http.ResponseWriter, r *http.Request) {
	if h.operationOriginFailure(w, r, "auth.password_reset.complete", "password") {
		return
	}
	var input application.PasswordResetCompleteInput
	if err := decodeJSONObject(r, &input, map[string]struct{}{"token": {}, "password": {}}); err != nil {
		var fieldErr fieldValueError
		if errors.As(err, &fieldErr) && fieldErr.field == "token" {
			h.recordOperation(r, unverifiedFailure("auth.password_reset.complete", "password", "", "", err))
			writeProblem(w, http.StatusForbidden, "invalid-password-reset-token")
			return
		}
		h.recordOperation(r, unverifiedFailure("auth.password_reset.complete", "password", "", "", err))
		writeDecodeError(w, err)
		return
	}
	input.SourceIP = h.requestSourceIP(r)
	var target domain.User
	var completeErr error
	if audited, ok := h.recovery.(PasswordRecoveryAuditService); ok {
		target, completeErr = audited.CompleteWithTarget(r.Context(), input)
	} else {
		completeErr = h.recovery.Complete(r.Context(), input)
	}
	if completeErr != nil {
		h.recordOperation(r, application.OperationLogInput{Action: "auth.password_reset.complete", ObjectType: "password", Result: application.OperationLogFailure, Details: map[string]any{"failure": operationErrorCode(completeErr)}})
		writeApplicationError(w, completeErr)
		return
	}
	details := map[string]any{"passwordAction": "updated"}
	if target.ID != "" {
		details["target"] = map[string]any{"id": target.ID, "name": target.Name, "email": target.Email}
	}
	h.recordOperation(r, application.OperationLogInput{Action: "auth.password_reset.complete", ObjectType: "password", ObjectID: target.ID, Result: application.OperationLogSuccess, Details: details})
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) notFound(w http.ResponseWriter, _ *http.Request) {
	writeProblem(w, http.StatusNotFound, "not-found")
}

func requestLocale(r *http.Request) domain.Locale {
	for _, value := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		language := strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
		if strings.HasPrefix(language, "zh") {
			return domain.LocaleChinese
		}
		if strings.HasPrefix(language, "en") {
			return domain.LocaleEnglish
		}
	}
	return domain.LocaleEnglish
}

func (h *Handler) validOrigin(r *http.Request) bool {
	values := r.Header.Values("Origin")
	if len(values) != 1 {
		return false
	}
	value := values[0]
	if value == "" || strings.EqualFold(value, "null") {
		return false
	}
	origin, err := config.CanonicalOrigin(value)
	return err == nil && origin == h.cfg.Origin
}

// operationOriginFailure and operationPrincipal keep the write-handler audit
// boundary in one place. A request which was rejected before identity could be
// verified is still an attempted operation, but is recorded as unverified and
// never copies a caller-controlled identity into actor fields.
func (h *Handler) operationOriginFailure(w http.ResponseWriter, r *http.Request, action, objectType string) bool {
	if h.validOrigin(r) {
		return false
	}
	h.recordOperation(r, unverifiedFailure(action, objectType, "", "", application.ErrForbidden))
	writeProblem(w, http.StatusForbidden, "forbidden")
	return true
}

func (h *Handler) operationPrincipal(w http.ResponseWriter, r *http.Request, action, objectType, objectID string) (domain.Principal, bool) {
	principal, err := h.currentPrincipal(r)
	if err != nil {
		h.recordOperation(r, unverifiedFailure(action, objectType, objectID, "", err))
		writeApplicationError(w, err)
		return domain.Principal{}, false
	}
	return principal, true
}

func (h *Handler) currentPrincipal(r *http.Request) (domain.Principal, error) {
	cookie, err := r.Cookie(h.cfg.CookieName)
	if err != nil {
		return domain.Principal{}, application.ErrUnauthenticated
	}
	if enriched, ok := h.auth.(application.PrincipalAuthenticationService); ok {
		return enriched.CurrentPrincipal(r.Context(), cookie.Value)
	}
	user, err := h.auth.Current(r.Context(), cookie.Value)
	if err != nil {
		return domain.Principal{}, err
	}
	return domain.Principal{User: user}, nil
}

func (h *Handler) currentPrincipalNoTouch(r *http.Request) (domain.Principal, error) {
	cookie, err := r.Cookie(h.cfg.CookieName)
	if err != nil {
		return domain.Principal{}, application.ErrUnauthenticated
	}
	if enriched, ok := h.auth.(application.NoTouchPrincipalAuthenticationService); ok {
		return enriched.CurrentPrincipalNoTouch(r.Context(), cookie.Value)
	}
	if readOnly, ok := h.auth.(application.NoTouchAuthenticationService); ok {
		user, err := readOnly.CurrentNoTouch(r.Context(), cookie.Value)
		if err != nil {
			return domain.Principal{}, err
		}
		return domain.Principal{User: user}, nil
	}
	return domain.Principal{}, application.ErrDependencyUnavailable
}

func (h *Handler) roles(w http.ResponseWriter, r *http.Request) {
	principal, err := h.currentPrincipal(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	result, err := h.access.Roles(r.Context(), principal.User.ID)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	roles := make([]roleResponseBody, 0, len(result.Items))
	for _, role := range result.Items {
		roles = append(roles, roleResponse(role))
	}
	permissions := make([]permissionResponseBody, 0, len(result.Catalog))
	for _, definition := range result.Catalog {
		dependencies := make([]string, 0, len(definition.Dependencies))
		for _, dependency := range definition.Dependencies {
			dependencies = append(dependencies, string(dependency))
		}
		permissions = append(permissions, permissionResponseBody{Key: string(definition.Key), Resource: definition.Resource, Action: definition.Action, LabelKey: definition.LabelKey, Description: definition.Description, Dependencies: dependencies})
	}
	combinations := make([]combinationResponseBody, 0, len(result.Combinations))
	for _, combination := range result.Combinations {
		keys := make([]string, 0, len(combination.Permissions))
		for _, permission := range combination.Permissions {
			keys = append(keys, string(permission))
		}
		triggers := make([]string, 0, len(combination.Trigger))
		for _, trigger := range combination.Trigger {
			triggers = append(triggers, string(trigger))
		}
		combinations = append(combinations, combinationResponseBody{Key: combination.Key, LabelKey: combination.LabelKey, Description: combination.Description, Permissions: keys, Trigger: triggers})
	}
	writeJSON(w, http.StatusOK, roleListResponse{Roles: roles, Permissions: permissions, Combinations: combinations})
}

func (h *Handler) roleOptions(w http.ResponseWriter, r *http.Request) {
	principal, err := h.currentPrincipal(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	options, err := h.access.RoleOptions(r.Context(), principal.User.ID)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	items := make([]roleOptionResponseBody, 0, len(options))
	for _, option := range options {
		items = append(items, roleOptionResponseBody{ID: option.ID, Name: option.Name})
	}
	writeJSON(w, http.StatusOK, roleOptionsResponse{Roles: items})
}

func (h *Handler) role(w http.ResponseWriter, r *http.Request) {
	principal, err := h.currentPrincipal(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	id := r.PathValue("id")
	if !domain.IsCanonicalUUID(id) {
		writeProblem(w, http.StatusNotFound, "not-found")
		return
	}
	item, err := h.access.Role(r.Context(), principal.User.ID, id)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, roleEnvelope{Role: roleResponse(item)})
}

type roleMutationRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Permissions []domain.PermissionKey `json:"permissions"`
	Revision    *int64                 `json:"revision"`
}

func (h *Handler) createRole(w http.ResponseWriter, r *http.Request) {
	if h.operationOriginFailure(w, r, "roles.create", "role") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, "roles.create", "role", "")
	if !ok {
		return
	}
	var err error
	var input roleMutationRequest
	if err := decodeJSONObject(r, &input, map[string]struct{}{"name": {}, "description": {}, "permissions": {}}); err != nil {
		h.recordOperation(r, operationFailure(principal.User, "roles.create", "role", "", err, nil))
		writeDecodeError(w, err)
		return
	}
	item, err := h.access.CreateRole(r.Context(), principal.User.ID, application.RoleMutationInput{Name: input.Name, Description: input.Description, Permissions: input.Permissions})
	if err != nil {
		h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "roles.create", ObjectType: "role", Result: application.OperationLogFailure, Details: map[string]any{"failure": operationErrorCode(err)}})
		writeApplicationError(w, err)
		return
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "roles.create", ObjectType: "role", ObjectID: item.ID, Result: application.OperationLogSuccess, Details: map[string]any{"before": nil, "after": roleSnapshot(item)}})
	writeJSON(w, http.StatusCreated, roleEnvelope{Role: roleResponse(item)})
}

func (h *Handler) replaceRole(w http.ResponseWriter, r *http.Request) {
	if h.operationOriginFailure(w, r, "roles.update", "role") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, "roles.update", "role", r.PathValue("id"))
	if !ok {
		return
	}
	var err error
	var input roleMutationRequest
	if err := decodeJSONObject(r, &input, map[string]struct{}{"name": {}, "description": {}, "permissions": {}, "revision": {}}); err != nil {
		h.recordOperation(r, operationFailure(principal.User, "roles.update", "role", r.PathValue("id"), err, nil))
		writeDecodeError(w, err)
		return
	}
	id := r.PathValue("id")
	if !domain.IsCanonicalUUID(id) {
		writeProblem(w, http.StatusNotFound, "not-found")
		return
	}
	if input.Revision == nil {
		h.recordOperation(r, operationFailure(principal.User, "roles.update", "role", id, application.ErrStaleRevision, map[string]any{"validation": "revision_required"}))
		writeProblemWithCode(w, http.StatusUnprocessableEntity, "validation-failed", "validation_failed", "", []domain.FieldError{{Field: "revision", Code: "required"}})
		return
	}
	before, hasBefore := h.snapshotRole(r, principal.User.ID, id)
	item, err := h.access.ReplaceRole(r.Context(), principal.User.ID, id, application.RoleMutationInput{Name: input.Name, Description: input.Description, Permissions: input.Permissions, Revision: *input.Revision})
	if err != nil {
		h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "roles.update", ObjectType: "role", ObjectID: id, Result: application.OperationLogFailure, Details: map[string]any{"failure": operationErrorCode(err)}})
		writeApplicationError(w, err)
		return
	}
	details := map[string]any{"after": roleSnapshot(item), "requestedRevision": *input.Revision}
	if hasBefore {
		details["before"] = roleSnapshot(before)
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "roles.update", ObjectType: "role", ObjectID: id, Result: application.OperationLogSuccess, Details: details})
	writeJSON(w, http.StatusOK, roleEnvelope{Role: roleResponse(item)})
}

func (h *Handler) deleteRole(w http.ResponseWriter, r *http.Request) {
	if h.operationOriginFailure(w, r, "roles.delete", "role") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, "roles.delete", "role", r.PathValue("id"))
	if !ok {
		return
	}
	var err error
	id := r.PathValue("id")
	if !domain.IsCanonicalUUID(id) {
		writeProblem(w, http.StatusNotFound, "not-found")
		return
	}
	var before domain.Role
	hasBefore := false
	if audited, ok := h.access.(application.OperationAuditMutationService); ok {
		before, err = audited.DeleteRoleWithSnapshot(r.Context(), principal.User.ID, id)
		hasBefore = err == nil
	} else {
		before, hasBefore = h.snapshotRole(r, principal.User.ID, id)
		err = h.access.DeleteRole(r.Context(), principal.User.ID, id)
	}
	if err != nil {
		h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "roles.delete", ObjectType: "role", ObjectID: id, Result: application.OperationLogFailure, Details: map[string]any{"failure": operationErrorCode(err)}})
		writeApplicationError(w, err)
		return
	}
	details := map[string]any{"deleted": true}
	if hasBefore {
		details["before"] = roleSnapshot(before)
		details["objectLabel"] = before.Name
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "roles.delete", ObjectType: "role", ObjectID: id, Result: application.OperationLogSuccess, Details: details})
	w.WriteHeader(http.StatusNoContent)
}

func parsePageQuery(r *http.Request) (string, int, error) {
	query := r.URL.Query()
	cursor := query.Get("cursor")
	limit := 25
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return "", 0, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "limit", Code: "invalid_limit"}}}
		}
		limit = parsed
	}
	if cursor != "" && !domain.IsCanonicalUUID(cursor) {
		return "", 0, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "cursor", Code: "invalid_cursor"}}}
	}
	if limit < 1 || limit > 100 {
		return "", 0, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "limit", Code: "invalid_limit"}}}
	}
	return cursor, limit, nil
}

func parseAccessListQuery(r *http.Request) application.AccessListOptions {
	query := r.URL.Query()
	limit := 25
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			limit = -1
		} else {
			limit = parsed
		}
	}
	return application.AccessListOptions{
		Cursor:    query.Get("cursor"),
		Query:     query.Get("q"),
		RoleID:    query.Get("roleId"),
		Status:    query.Get("status"),
		Sort:      query.Get("sort"),
		Direction: query.Get("direction"),
		Limit:     limit,
	}
}

func (h *Handler) users(w http.ResponseWriter, r *http.Request) {
	principal, err := h.currentPrincipal(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	options := parseAccessListQuery(r)
	var page application.UserPage
	if queryable, ok := h.access.(QueryableAccessService); ok {
		page, err = queryable.UsersWithOptions(r.Context(), principal.User.ID, options)
	} else {
		cursor, limit, parseErr := parsePageQuery(r)
		if parseErr != nil {
			writeApplicationError(w, parseErr)
			return
		}
		page, err = h.access.Users(r.Context(), principal.User.ID, cursor, limit)
	}
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	items := make([]accessUserResponseBody, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, accessUserResponse(item))
	}
	writeJSON(w, http.StatusOK, usersResponse{Users: items, NextCursor: page.NextCursor})
}

type assignmentRequest struct {
	RoleIDs     []string `json:"roleIds"`
	AuthVersion *int64   `json:"authVersion"`
}

func (h *Handler) replaceUserRoles(w http.ResponseWriter, r *http.Request) {
	if h.operationOriginFailure(w, r, "users.roles.update", "user") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, "users.roles.update", "user", r.PathValue("id"))
	if !ok {
		return
	}
	var err error
	var input assignmentRequest
	if err := decodeJSONObject(r, &input, map[string]struct{}{"roleIds": {}, "authVersion": {}}); err != nil {
		h.recordOperation(r, operationFailure(principal.User, "users.roles.update", "user", r.PathValue("id"), err, nil))
		writeDecodeError(w, err)
		return
	}
	id := r.PathValue("id")
	if !domain.IsCanonicalUUID(id) {
		writeProblem(w, http.StatusNotFound, "not-found")
		return
	}
	if input.AuthVersion == nil {
		h.recordOperation(r, operationFailure(principal.User, "users.roles.update", "user", id, application.ErrStaleRevision, map[string]any{"validation": "auth_version_required"}))
		writeProblemWithCode(w, http.StatusUnprocessableEntity, "validation-failed", "validation_failed", "", []domain.FieldError{{Field: "authVersion", Code: "required"}})
		return
	}
	before, hasBefore := h.snapshotUser(r, principal.User.ID, id)
	item, err := h.access.ReplaceUserRoles(r.Context(), principal.User.ID, id, application.AssignmentInput{RoleIDs: input.RoleIDs, AuthVersion: *input.AuthVersion})
	if err != nil {
		h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "users.roles.update", ObjectType: "user", ObjectID: id, Result: application.OperationLogFailure, Details: map[string]any{"failure": operationErrorCode(err)}})
		writeApplicationError(w, err)
		return
	}
	details := map[string]any{"after": accessUserSnapshot(item), "requestedAuthVersion": *input.AuthVersion}
	if hasBefore {
		details["before"] = accessUserSnapshot(before)
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "users.roles.update", ObjectType: "user", ObjectID: id, Result: application.OperationLogSuccess, Details: details})
	writeJSON(w, http.StatusOK, map[string]accessUserResponseBody{"user": accessUserResponse(item)})
}

func (h *Handler) invitations(w http.ResponseWriter, r *http.Request) {
	principal, err := h.currentPrincipal(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	options := parseAccessListQuery(r)
	var page application.InvitationPage
	if queryable, ok := h.access.(QueryableAccessService); ok {
		page, err = queryable.InvitationsWithOptions(r.Context(), principal.User.ID, options)
	} else {
		cursor, limit, parseErr := parsePageQuery(r)
		if parseErr != nil {
			writeApplicationError(w, parseErr)
			return
		}
		page, err = h.access.Invitations(r.Context(), principal.User.ID, cursor, limit)
	}
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	items := make([]invitationResponseBody, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, invitationResponse(item))
	}
	writeJSON(w, http.StatusOK, invitationsResponse{Invitations: items, NextCursor: page.NextCursor})
}

type invitationRequest struct {
	Name    string   `json:"name"`
	Email   string   `json:"email"`
	RoleIDs []string `json:"roleIds"`
}

func (h *Handler) createInvitation(w http.ResponseWriter, r *http.Request) {
	if h.operationOriginFailure(w, r, "invitations.create", "invitation") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, "invitations.create", "invitation", "")
	if !ok {
		return
	}
	var err error
	var input invitationRequest
	if err := decodeJSONObject(r, &input, map[string]struct{}{"name": {}, "email": {}, "roleIds": {}}); err != nil {
		h.recordOperation(r, operationFailure(principal.User, "invitations.create", "invitation", "", err, nil))
		writeDecodeError(w, err)
		return
	}
	item, err := h.access.CreateInvitation(r.Context(), principal.User.ID, application.InvitationInput{Name: input.Name, Email: input.Email, RoleIDs: input.RoleIDs})
	if err != nil {
		h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "invitations.create", ObjectType: "invitation", Result: application.OperationLogFailure, AttemptedAccount: input.Email, Details: map[string]any{"failure": operationErrorCode(err)}})
		writeApplicationError(w, err)
		return
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "invitations.create", ObjectType: "invitation", ObjectID: item.ID, Result: application.OperationLogSuccess, Details: map[string]any{"before": nil, "after": invitationSnapshot(item), "mailQueued": true}})
	writeJSON(w, http.StatusCreated, map[string]invitationResponseBody{"invitation": invitationResponse(item)})
}

func (h *Handler) resendInvitation(w http.ResponseWriter, r *http.Request) {
	if h.operationOriginFailure(w, r, "invitations.resend", "invitation") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, "invitations.resend", "invitation", r.PathValue("id"))
	if !ok {
		return
	}
	var err error
	id := r.PathValue("id")
	if !domain.IsCanonicalUUID(id) {
		writeProblem(w, http.StatusNotFound, "not-found")
		return
	}
	var before, item domain.Invitation
	hasBefore := false
	if audited, ok := h.access.(application.OperationAuditMutationService); ok {
		before, item, err = audited.ResendInvitationWithSnapshot(r.Context(), principal.User.ID, id)
		hasBefore = err == nil
	} else {
		before, hasBefore = h.snapshotInvitation(r, principal.User.ID, id)
		item, err = h.access.ResendInvitation(r.Context(), principal.User.ID, id)
	}
	if err != nil {
		h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "invitations.resend", ObjectType: "invitation", ObjectID: id, Result: application.OperationLogFailure, Details: map[string]any{"failure": operationErrorCode(err)}})
		writeApplicationError(w, err)
		return
	}
	details := map[string]any{"after": invitationSnapshot(item), "mailQueued": true}
	if hasBefore {
		details["before"] = invitationSnapshot(before)
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "invitations.resend", ObjectType: "invitation", ObjectID: id, Result: application.OperationLogSuccess, Details: details})
	writeJSON(w, http.StatusAccepted, map[string]invitationResponseBody{"invitation": invitationResponse(item)})
}

func (h *Handler) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	if h.operationOriginFailure(w, r, "invitations.revoke", "invitation") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, "invitations.revoke", "invitation", r.PathValue("id"))
	if !ok {
		return
	}
	var err error
	id := r.PathValue("id")
	if !domain.IsCanonicalUUID(id) {
		writeProblem(w, http.StatusNotFound, "not-found")
		return
	}
	var before domain.Invitation
	hasBefore := false
	if audited, ok := h.access.(application.OperationAuditMutationService); ok {
		before, err = audited.RevokeInvitationWithSnapshot(r.Context(), principal.User.ID, id)
		hasBefore = err == nil
	} else {
		before, hasBefore = h.snapshotInvitation(r, principal.User.ID, id)
		err = h.access.RevokeInvitation(r.Context(), principal.User.ID, id)
	}
	if err != nil {
		h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "invitations.revoke", ObjectType: "invitation", ObjectID: id, Result: application.OperationLogFailure, Details: map[string]any{"failure": operationErrorCode(err)}})
		writeApplicationError(w, err)
		return
	}
	details := map[string]any{"revoked": true}
	if hasBefore {
		details["before"] = invitationSnapshot(before)
		details["objectLabel"] = before.Email
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "invitations.revoke", ObjectType: "invitation", ObjectID: id, Result: application.OperationLogSuccess, Details: details})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) acceptInvitationHandler(w http.ResponseWriter, r *http.Request) {
	if h.operationOriginFailure(w, r, "auth.invitation.accept", "invitation") {
		return
	}
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := decodeJSONObject(r, &input, map[string]struct{}{"token": {}, "password": {}}); err != nil {
		var fieldErr fieldValueError
		if errors.As(err, &fieldErr) && strings.EqualFold(fieldErr.field, "token") {
			h.recordOperation(r, unverifiedFailure("auth.invitation.accept", "invitation", "", "", err))
			writeProblem(w, http.StatusForbidden, "invalid-invitation")
			return
		}
		h.recordOperation(r, unverifiedFailure("auth.invitation.accept", "invitation", "", "", err))
		writeDecodeError(w, err)
		return
	}
	sourceIP := h.requestSourceIP(r)
	var target domain.Invitation
	var completeErr error
	if audited, ok := h.acceptInvitation.(InvitationAcceptanceAuditService); ok {
		if sourceAware, sourceOK := h.acceptInvitation.(InvitationAcceptanceSourceService); sourceOK {
			target, completeErr = sourceAware.CompleteWithTargetAndSource(r.Context(), sourceIP, input.Token, input.Password)
		} else {
			target, completeErr = audited.CompleteWithTarget(r.Context(), input.Token, input.Password)
		}
	} else {
		completeErr = h.acceptInvitation.Complete(r.Context(), input.Token, input.Password)
	}
	if completeErr != nil {
		h.recordOperation(r, application.OperationLogInput{Action: "auth.invitation.accept", ObjectType: "invitation", Result: application.OperationLogFailure, Details: map[string]any{"failure": operationErrorCode(completeErr)}})
		writeApplicationError(w, completeErr)
		return
	}
	details := map[string]any{"accountActivated": true}
	if target.ID != "" {
		details["target"] = map[string]any{"id": target.ID, "name": target.Name, "email": target.Email}
		details["objectLabel"] = target.Email
	}
	h.recordOperation(r, application.OperationLogInput{Action: "auth.invitation.accept", ObjectType: "invitation", ObjectID: target.ID, Result: application.OperationLogSuccess, Details: details})
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) emailSettings(w http.ResponseWriter, r *http.Request) {
	if h.settings == nil {
		h.notFound(w, r)
		return
	}
	principal, err := h.currentPrincipal(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	if !principal.SuperAdmin && !principal.Has(domain.PermissionSettingsRead) {
		writeProblem(w, http.StatusForbidden, "forbidden")
		return
	}
	view, err := h.settings.GetEmailSettings(r.Context())
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, emailSettingsEnvelope{Email: emailSettingsResponseBody(view)})
}

type emailSettingsRequest struct {
	Host           string                  `json:"host"`
	Port           int                     `json:"port"`
	Security       string                  `json:"security"`
	Username       string                  `json:"username"`
	Password       *string                 `json:"password"`
	ClearPassword  bool                    `json:"clearPassword"`
	FromAddress    string                  `json:"fromAddress"`
	FromName       string                  `json:"fromName"`
	DefaultLocale  string                  `json:"defaultLocale"`
	AutoRetryCount emailRetryCountField    `json:"autoRetryCount"`
	RetentionDays  emailRetentionDaysField `json:"retentionDays"`
	Revision       *int64                  `json:"revision"`
}

func (h *Handler) saveEmailSettings(w http.ResponseWriter, r *http.Request) {
	if h.settings == nil {
		h.notFound(w, r)
		return
	}
	if h.operationOriginFailure(w, r, "settings.email.update", "email_settings") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, "settings.email.update", "email_settings", "")
	if !ok {
		return
	}
	var err error
	if !principal.SuperAdmin && !principal.Has(domain.PermissionSettingsWrite) {
		h.recordOperation(r, operationFailure(principal.User, "settings.email.update", "email_settings", "", application.ErrForbidden, nil))
		writeProblem(w, http.StatusForbidden, "forbidden")
		return
	}
	before, beforeErr := h.settings.GetEmailSettings(r.Context())
	var input emailSettingsRequest
	if err := decodeJSONObject(r, &input, map[string]struct{}{"host": {}, "port": {}, "security": {}, "username": {}, "password": {}, "clearPassword": {}, "fromAddress": {}, "fromName": {}, "defaultLocale": {}, "autoRetryCount": {}, "retentionDays": {}, "revision": {}}); err != nil {
		h.recordOperation(r, operationFailure(principal.User, "settings.email.update", "email_settings", "", err, nil))
		writeDecodeError(w, err)
		return
	}
	revision := int64(0)
	if input.Revision != nil {
		revision = *input.Revision
	}
	view, err := h.settings.SaveEmailSettings(r.Context(), application.EmailSettingsInput{Host: input.Host, Port: input.Port, Security: input.Security, Username: input.Username, Password: input.Password, ClearPassword: input.ClearPassword, FromAddress: input.FromAddress, FromName: input.FromName, DefaultLocale: input.DefaultLocale, AutoRetryCount: input.AutoRetryCount.pointer(), RetentionDays: input.RetentionDays.pointer(), Revision: revision})
	if err != nil {
		details := map[string]any{}
		if beforeErr == nil {
			details["before"] = emailSettingsSnapshot(before)
		}
		h.recordOperation(r, operationFailure(principal.User, "settings.email.update", "email_settings", "", err, details))
		writeApplicationError(w, err)
		return
	}
	details := map[string]any{"before": nil, "after": emailSettingsSnapshot(view), "passwordAction": emailPasswordAction(input), "fieldsModified": []string{"smtp", "sender", "locale", "retryPolicy", "retention"}}
	if beforeErr == nil {
		details["before"] = emailSettingsSnapshot(before)
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "settings.email.update", ObjectType: "email_settings", Result: application.OperationLogSuccess, Details: details})
	writeJSON(w, http.StatusOK, emailSettingsEnvelope{Email: emailSettingsResponseBody(view)})
}

func (h *Handler) testEmailSettings(w http.ResponseWriter, r *http.Request) {
	if h.settings == nil {
		h.notFound(w, r)
		return
	}
	if h.operationOriginFailure(w, r, "settings.email.test", "email_settings") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, "settings.email.test", "email_settings", "")
	if !ok {
		return
	}
	var err error
	if !principal.SuperAdmin && !principal.Has(domain.PermissionSettingsWrite) {
		h.recordOperation(r, operationFailure(principal.User, "settings.email.test", "email_settings", "", application.ErrForbidden, nil))
		writeProblem(w, http.StatusForbidden, "forbidden")
		return
	}
	var input struct {
		Recipient string `json:"recipient"`
	}
	// Testing is intentionally a saved-settings operation. Do not accept SMTP
	// fields here: allowing a draft snapshot would silently send with config
	// that the dispatcher never uses.
	if err := decodeJSONObject(r, &input, map[string]struct{}{"recipient": {}}); err != nil {
		h.recordOperation(r, operationFailure(principal.User, "settings.email.test", "email_settings", "", err, nil))
		writeDecodeError(w, err)
		return
	}
	settingsInput := application.EmailSettingsInput{}
	var task application.MailTask
	if taskService, taskOK := h.settings.(TestEmailSettingsTaskService); taskOK {
		task, err = taskService.TestEmailSettingsTask(r.Context(), principal.User.ID, settingsInput, input.Recipient)
	} else {
		// The generated endpoint has one ownership model: a test email is a
		// durable task. A settings-only embedder that has not wired the outbox
		// must fail closed rather than silently sending synchronously.
		err = application.ErrDependencyUnavailable
	}
	if err != nil {
		h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "settings.email.test", ObjectType: "email_settings", Result: application.OperationLogFailure, Details: map[string]any{"failure": operationErrorCode(err)}})
		writeApplicationError(w, err)
		return
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: "settings.email.test", ObjectType: "email_settings", Result: application.OperationLogSuccess, Details: map[string]any{"recipientProvided": input.Recipient != "", "taskID": task.ID}})
	if task.ID != "" {
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "task": mailTaskResponseBody(task)})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (r emailSettingsRequest) RevisionValue() int64 {
	if r.Revision == nil {
		return 0
	}
	return *r.Revision
}

func (h *Handler) operationalWarnings(w http.ResponseWriter, r *http.Request) {
	if h.settings == nil {
		h.notFound(w, r)
		return
	}
	principal, err := h.currentPrincipal(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	if !principal.SuperAdmin && !principal.Has(domain.PermissionSettingsRead) {
		writeJSON(w, http.StatusOK, map[string]any{"warnings": []application.OperationalWarning{}})
		return
	}
	warnings, err := h.settings.OperationalWarnings(r.Context())
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"warnings": warnings})
}
