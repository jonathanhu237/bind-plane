package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type userEnvelope struct {
	User userResponseBody `json:"user"`
}

type userResponseBody struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Locale        string `json:"locale,omitempty"`
	AvatarURL     string `json:"avatarUrl,omitempty"`
	HasAvatar     bool   `json:"hasAvatar"`
	AvatarVersion int64  `json:"avatarVersion,omitempty"`
}

type roleResponseBody struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	System          string   `json:"system,omitempty"`
	Permissions     []string `json:"permissions"`
	Revision        int64    `json:"revision"`
	AssignmentCount int      `json:"assignmentCount,omitempty"`
	CreatedAt       string   `json:"createdAt,omitempty"`
	UpdatedAt       string   `json:"updatedAt,omitempty"`
}

type principalResponseBody struct {
	User        userResponseBody   `json:"user"`
	Roles       []roleResponseBody `json:"roles"`
	Permissions []string           `json:"permissions"`
	SuperAdmin  bool               `json:"superAdmin"`
}

type principalEnvelope struct {
	User        userResponseBody   `json:"user"`
	Roles       []roleResponseBody `json:"roles"`
	Permissions []string           `json:"permissions"`
	SuperAdmin  bool               `json:"superAdmin"`
}

type roleListResponse struct {
	Roles        []roleResponseBody        `json:"roles"`
	Permissions  []permissionResponseBody  `json:"permissions"`
	Combinations []combinationResponseBody `json:"combinations"`
}

type permissionResponseBody struct {
	Key          string   `json:"key"`
	Resource     string   `json:"resource"`
	Action       string   `json:"action"`
	LabelKey     string   `json:"labelKey"`
	Description  string   `json:"description"`
	Dependencies []string `json:"dependencies,omitempty"`
}

type combinationResponseBody struct {
	Key         string   `json:"key"`
	LabelKey    string   `json:"labelKey"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	Trigger     []string `json:"trigger"`
}

type roleOptionResponseBody struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type roleOptionsResponse struct {
	Roles []roleOptionResponseBody `json:"roles"`
}

type roleEnvelope struct {
	Role roleResponseBody `json:"role"`
}

type accessUserResponseBody struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Email         string             `json:"email"`
	CreatedAt     string             `json:"createdAt"`
	AuthVersion   int64              `json:"authVersion"`
	Disabled      bool               `json:"disabled"`
	Locale        string             `json:"locale,omitempty"`
	AvatarURL     string             `json:"avatarUrl,omitempty"`
	HasAvatar     bool               `json:"hasAvatar"`
	AvatarVersion int64              `json:"avatarVersion,omitempty"`
	Roles         []roleResponseBody `json:"roles"`
}

type usersResponse struct {
	Users      []accessUserResponseBody `json:"users"`
	NextCursor string                   `json:"nextCursor,omitempty"`
}

type invitationResponseBody struct {
	Creator   operationActor     `json:"creator"`
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Email     string             `json:"email"`
	Locale    string             `json:"locale"`
	Roles     []roleResponseBody `json:"roles"`
	ExpiresAt string             `json:"expiresAt"`
	CreatedAt string             `json:"createdAt"`
	Revision  int64              `json:"revision"`
}

type invitationsResponse struct {
	Invitations []invitationResponseBody `json:"invitations"`
	NextCursor  string                   `json:"nextCursor,omitempty"`
}

type emailSettingsResponse struct {
	Configured     bool   `json:"configured"`
	Host           string `json:"host,omitempty"`
	Port           int    `json:"port,omitempty"`
	Security       string `json:"security,omitempty"`
	Username       string `json:"username,omitempty"`
	PasswordSet    bool   `json:"passwordSet"`
	FromAddress    string `json:"fromAddress,omitempty"`
	FromName       string `json:"fromName,omitempty"`
	DefaultLocale  string `json:"defaultLocale,omitempty"`
	AutoRetryCount int    `json:"autoRetryCount"`
	RetentionDays  int    `json:"retentionDays"`
	Revision       int64  `json:"revision"`
	UpdatedAt      string `json:"updatedAt,omitempty"`
}

type emailSettingsEnvelope struct {
	Email emailSettingsResponse `json:"email"`
}

func emailSettingsResponseBody(view application.EmailSettingsView) emailSettingsResponse {
	body := emailSettingsResponse{Configured: view.Configured, Host: view.Host, Port: view.Port, Security: view.Security, Username: view.Username, PasswordSet: view.PasswordSet, FromAddress: view.FromAddress, FromName: view.FromName, DefaultLocale: string(view.DefaultLocale), AutoRetryCount: view.AutoRetryCount, RetentionDays: view.RetentionDays, Revision: view.Revision}
	if !view.UpdatedAt.IsZero() {
		body.UpdatedAt = view.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	return body
}

func principalResponse(principal domain.Principal) principalEnvelope {
	roles := make([]roleResponseBody, 0, len(principal.Roles))
	for _, role := range principal.Roles {
		roles = append(roles, roleResponse(role))
	}
	permissions := make([]string, 0, len(principal.Permissions))
	for _, permission := range principal.Permissions {
		permissions = append(permissions, string(permission))
	}
	return principalEnvelope{User: userResponseBodyFor(principal.User), Roles: roles, Permissions: permissions, SuperAdmin: principal.SuperAdmin}
}

func roleResponse(role domain.Role) roleResponseBody {
	permissions := make([]string, 0, len(role.Permissions))
	for _, permission := range role.Permissions {
		permissions = append(permissions, string(permission))
	}
	body := roleResponseBody{ID: role.ID, Name: role.Name, Description: role.Description, System: role.SystemKey, Permissions: permissions, Revision: role.Revision, AssignmentCount: role.AssignmentCount}
	if !role.CreatedAt.IsZero() {
		body.CreatedAt = role.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !role.UpdatedAt.IsZero() {
		body.UpdatedAt = role.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	return body
}

func accessUserResponse(user domain.AccessUser) accessUserResponseBody {
	roles := make([]roleResponseBody, 0, len(user.Roles))
	for _, role := range user.Roles {
		roles = append(roles, roleResponse(role))
	}
	return accessUserResponseBody{ID: user.User.ID, Name: user.User.Name, Email: user.User.Email, CreatedAt: user.User.CreatedAt.UTC().Format(time.RFC3339Nano), AuthVersion: user.AuthVersion, Disabled: user.User.Disabled, Locale: string(user.User.Locale), AvatarURL: avatarURL(user.User), HasAvatar: user.User.HasAvatar, AvatarVersion: user.User.AvatarVersion, Roles: roles}
}

func invitationResponse(invitation domain.Invitation) invitationResponseBody {
	roles := make([]roleResponseBody, 0, len(invitation.Roles))
	for _, role := range invitation.Roles {
		roles = append(roles, roleResponse(role))
	}
	return invitationResponseBody{Creator: operationActor{ID: invitation.CreatedBy, Name: invitation.CreatedByName, Email: invitation.CreatedByEmail, Deleted: invitation.CreatorDeleted}, ID: invitation.ID, Name: invitation.Name, Email: invitation.Email, Locale: string(invitation.Locale), Roles: roles, ExpiresAt: invitation.ExpiresAt.UTC().Format(time.RFC3339Nano), CreatedAt: invitation.CreatedAt.UTC().Format(time.RFC3339Nano), Revision: invitation.Revision}
}

func userResponse(user domain.User) userEnvelope {
	return userEnvelope{User: userResponseBodyFor(user)}
}

func userResponseBodyFor(user domain.User) userResponseBody {
	return userResponseBody{ID: user.ID, Name: user.Name, Email: user.Email, Locale: string(user.Locale), AvatarURL: avatarURL(user), HasAvatar: user.HasAvatar, AvatarVersion: user.AvatarVersion}
}

func avatarURL(user domain.User) string {
	if !user.HasAvatar || user.ID == "" || user.AvatarVersion <= 0 {
		return ""
	}
	return "/api/users/" + user.ID + "/avatar?v=" + strconv.FormatInt(user.AvatarVersion, 10)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeDecodeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errBodyTooLarge):
		writeProblem(w, http.StatusRequestEntityTooLarge, "content-too-large")
	case errors.Is(err, errUnsupportedMedia):
		writeProblem(w, http.StatusUnsupportedMediaType, "unsupported-media-type")
	default:
		var fieldErr fieldValueError
		if errors.As(err, &fieldErr) {
			writeProblemWithCode(w, http.StatusUnprocessableEntity, "validation-failed", "validation_failed", "", []domain.FieldError{{Field: fieldErr.field, Code: "invalid_value"}})
			return
		}
		writeProblem(w, http.StatusBadRequest, "invalid-request")
	}
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cfg.CookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.SecureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cfg.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.SecureCookie,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
	})
}
