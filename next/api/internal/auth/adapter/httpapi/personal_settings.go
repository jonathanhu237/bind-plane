package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

const maxAvatarMultipartBody = application.MaxAvatarBytes + 128*1024

type emailChangeResponse struct {
	ID                string `json:"id"`
	OldEmail          string `json:"oldEmail"`
	NewEmail          string `json:"newEmail"`
	ExpiresAt         string `json:"expiresAt"`
	ResendAvailableAt string `json:"resendAvailableAt"`
	AttemptsRemaining int    `json:"attemptsRemaining"`
	Revision          int64  `json:"revision"`
}

type personalProfileResponse struct {
	User        userResponseBody     `json:"user"`
	EmailChange *emailChangeResponse `json:"emailChange,omitempty"`
}

func personalEmailChangeResponse(request domain.EmailChangeRequest) *emailChangeResponse {
	if request.ID == "" {
		return nil
	}
	return &emailChangeResponse{
		ID:                request.ID,
		OldEmail:          request.OldEmail,
		NewEmail:          request.NewEmail,
		ExpiresAt:         request.ExpiresAt.UTC().Format(timeRFC3339Nano),
		ResendAvailableAt: request.ResendAfter.UTC().Format(timeRFC3339Nano),
		AttemptsRemaining: request.AttemptsRemaining,
		Revision:          request.Revision,
	}
}

const timeRFC3339Nano = "2006-01-02T15:04:05.999999999Z07:00"

type personalProfileInput struct {
	Name string `json:"name"`
}

type personalPreferencesInput struct {
	Locale string `json:"locale"`
}

type personalPasswordInput struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
	ConfirmPassword string `json:"confirmPassword"`
}

type personalEmailChangeInput struct {
	CurrentPassword string `json:"currentPassword"`
	NewEmail        string `json:"newEmail"`
}

type personalEmailVerifyInput struct {
	RequestID string `json:"requestId"`
	Code      string `json:"code"`
}

func (h *Handler) personalProfile(w http.ResponseWriter, r *http.Request) {
	principal, err := h.currentPrincipal(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	user, err := h.personal.Profile(r.Context(), principal.User.ID)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	request, err := h.personal.EmailChangeStatus(r.Context(), principal.User.ID)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, personalProfileResponse{User: userResponseBodyFor(user), EmailChange: personalEmailChangeResponse(request)})
}

func (h *Handler) personalProfileUpdate(w http.ResponseWriter, r *http.Request) {
	const action = "auth.profile.name.update"
	if h.operationOriginFailure(w, r, action, "user") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, action, "user", "")
	if !ok {
		return
	}
	var input personalProfileInput
	if err := decodeJSONObject(r, &input, map[string]struct{}{"name": {}}); err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "user", principal.User.ID, err, nil))
		writeDecodeError(w, err)
		return
	}
	before := principal.User
	updated, err := h.personal.UpdateName(r.Context(), principal.User.ID, input.Name)
	if err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "user", principal.User.ID, err, nil))
		writeApplicationError(w, err)
		return
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: before.ID, ActorName: before.Name, ActorEmail: before.Email, ActorKind: "authenticated", Action: action, ObjectType: "user", ObjectID: before.ID, Result: application.OperationLogSuccess, Details: map[string]any{"before": map[string]any{"name": before.Name}, "after": map[string]any{"name": updated.Name}, "fieldsModified": []string{"name"}}})
	writeJSON(w, http.StatusOK, userResponse(updated))
}

func (h *Handler) personalPreferencesUpdate(w http.ResponseWriter, r *http.Request) {
	const action = "auth.profile.locale.update"
	if h.operationOriginFailure(w, r, action, "user") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, action, "user", "")
	if !ok {
		return
	}
	var input personalPreferencesInput
	if err := decodeJSONObject(r, &input, map[string]struct{}{"locale": {}}); err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "user", principal.User.ID, err, nil))
		writeDecodeError(w, err)
		return
	}
	updated, err := h.personal.UpdateLocale(r.Context(), principal.User.ID, domain.Locale(input.Locale))
	if err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "user", principal.User.ID, err, nil))
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, userResponse(updated))
}

func (h *Handler) personalPasswordUpdate(w http.ResponseWriter, r *http.Request) {
	const action = "auth.profile.password.update"
	if h.operationOriginFailure(w, r, action, "password") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, action, "password", "")
	if !ok {
		return
	}
	var input personalPasswordInput
	if err := decodeJSONObject(r, &input, map[string]struct{}{"currentPassword": {}, "newPassword": {}, "confirmPassword": {}}); err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "password", principal.User.ID, err, nil))
		writeDecodeError(w, err)
		return
	}
	newPassword, newPasswordErr := domain.NewPassword(input.NewPassword)
	confirmation, confirmationErr := domain.NewPassword(input.ConfirmPassword)
	if newPasswordErr != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "password", principal.User.ID, newPasswordErr, nil))
		writeApplicationError(w, newPasswordErr)
		return
	}
	if confirmationErr != nil || string(newPassword) != string(confirmation) {
		err := &domain.ValidationErrors{Items: []domain.FieldError{{Field: "confirmPassword", Code: "password_mismatch"}}}
		h.recordOperation(r, operationFailure(principal.User, action, "password", principal.User.ID, err, nil))
		writeApplicationError(w, err)
		return
	}
	_, changedAt, err := h.personal.ChangePassword(r.Context(), principal.User.ID, input.CurrentPassword, input.NewPassword)
	if err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "password", principal.User.ID, err, nil))
		writeApplicationError(w, err)
		return
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, ActorKind: "authenticated", Action: action, ObjectType: "password", ObjectID: principal.User.ID, Result: application.OperationLogSuccess, Details: map[string]any{"changedAt": changedAt.UTC().Format(timeRFC3339Nano), "sessionsRevoked": true}})
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) personalEmailChangeStatus(w http.ResponseWriter, r *http.Request) {
	principal, err := h.currentPrincipal(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	request, err := h.personal.EmailChangeStatus(r.Context(), principal.User.ID)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"emailChange": personalEmailChangeResponse(request)})
}

func (h *Handler) personalEmailChangeRequest(w http.ResponseWriter, r *http.Request) {
	const action = "auth.profile.email.request"
	if h.operationOriginFailure(w, r, action, "email_change") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, action, "email_change", "")
	if !ok {
		return
	}
	var input personalEmailChangeInput
	if err := decodeJSONObject(r, &input, map[string]struct{}{"currentPassword": {}, "newEmail": {}}); err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "email_change", principal.User.ID, err, nil))
		writeDecodeError(w, err)
		return
	}
	request, err := h.personal.RequestEmailChange(r.Context(), principal.User.ID, input.CurrentPassword, input.NewEmail)
	if err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "email_change", principal.User.ID, err, map[string]any{"newEmailProvided": input.NewEmail != ""}))
		writeApplicationError(w, err)
		return
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, ActorKind: "authenticated", Action: action, ObjectType: "email_change", ObjectID: request.ID, Result: application.OperationLogSuccess, Details: map[string]any{"verificationQueued": true, "newEmail": request.NewEmail}})
	writeJSON(w, http.StatusAccepted, map[string]any{"emailChange": personalEmailChangeResponse(request)})
}

func (h *Handler) personalEmailChangeResend(w http.ResponseWriter, r *http.Request) {
	const action = "auth.profile.email.resend"
	if h.operationOriginFailure(w, r, action, "email_change") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, action, "email_change", "")
	if !ok {
		return
	}
	request, err := h.personal.ResendEmailChange(r.Context(), principal.User.ID)
	if err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "email_change", principal.User.ID, err, nil))
		writeApplicationError(w, err)
		return
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, ActorKind: "authenticated", Action: action, ObjectType: "email_change", ObjectID: request.ID, Result: application.OperationLogSuccess, Details: map[string]any{"verificationQueued": true}})
	writeJSON(w, http.StatusAccepted, map[string]any{"emailChange": personalEmailChangeResponse(request)})
}

func (h *Handler) personalEmailChangeVerify(w http.ResponseWriter, r *http.Request) {
	const action = "auth.profile.email.update"
	if h.operationOriginFailure(w, r, action, "user") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, action, "user", "")
	if !ok {
		return
	}
	var input personalEmailVerifyInput
	if err := decodeJSONObject(r, &input, map[string]struct{}{"requestId": {}, "code": {}}); err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "user", principal.User.ID, err, nil))
		writeDecodeError(w, err)
		return
	}
	if !domain.IsCanonicalUUID(input.RequestID) {
		err := &domain.ValidationErrors{Items: []domain.FieldError{{Field: "requestId", Code: "invalid_request"}}}
		h.recordOperation(r, operationFailure(principal.User, action, "user", principal.User.ID, err, nil))
		writeApplicationError(w, err)
		return
	}
	updated, changedAt, err := h.personal.CompleteEmailChange(r.Context(), principal.User.ID, input.RequestID, input.Code)
	if err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "user", principal.User.ID, err, nil))
		writeApplicationError(w, err)
		return
	}
	details := map[string]any{"before": map[string]any{"email": principal.User.Email}, "after": map[string]any{"email": updated.Email}, "changedAt": changedAt.UTC().Format(timeRFC3339Nano), "sessionsRevoked": true}
	h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, ActorKind: "authenticated", Action: action, ObjectType: "user", ObjectID: principal.User.ID, Result: application.OperationLogSuccess, Details: details})
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) personalAvatarUpload(w http.ResponseWriter, r *http.Request) {
	const action = "auth.profile.avatar.update"
	if h.operationOriginFailure(w, r, action, "user") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, action, "user", "")
	if !ok {
		return
	}
	data, mediaType, err := parseAvatarMultipart(w, r)
	if err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "user", principal.User.ID, err, nil))
		var validationErr *domain.ValidationErrors
		if errors.Is(err, application.ErrDependencyUnavailable) || errors.As(err, &validationErr) {
			writeApplicationError(w, err)
		} else {
			writeDecodeError(w, err)
		}
		return
	}
	before := principal.User
	updated, err := h.personal.SaveAvatar(r.Context(), principal.User.ID, data, mediaType)
	if err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "user", principal.User.ID, err, nil))
		writeApplicationError(w, err)
		return
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: before.ID, ActorName: before.Name, ActorEmail: before.Email, ActorKind: "authenticated", Action: action, ObjectType: "user", ObjectID: before.ID, Result: application.OperationLogSuccess, Details: map[string]any{"before": map[string]any{"hasAvatar": before.HasAvatar}, "after": map[string]any{"hasAvatar": true, "avatarVersion": updated.AvatarVersion}, "fieldsModified": []string{"avatar"}}})
	writeJSON(w, http.StatusOK, userResponse(updated))
}

func (h *Handler) personalAvatarDelete(w http.ResponseWriter, r *http.Request) {
	const action = "auth.profile.avatar.delete"
	if h.operationOriginFailure(w, r, action, "user") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, action, "user", "")
	if !ok {
		return
	}
	before := principal.User
	updated, err := h.personal.RemoveAvatar(r.Context(), principal.User.ID)
	if err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "user", principal.User.ID, err, nil))
		writeApplicationError(w, err)
		return
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: before.ID, ActorName: before.Name, ActorEmail: before.Email, ActorKind: "authenticated", Action: action, ObjectType: "user", ObjectID: before.ID, Result: application.OperationLogSuccess, Details: map[string]any{"before": map[string]any{"hasAvatar": before.HasAvatar}, "after": map[string]any{"hasAvatar": false}, "fieldsModified": []string{"avatar"}}})
	writeJSON(w, http.StatusOK, userResponse(updated))
}

func (h *Handler) personalAvatarReadSelf(w http.ResponseWriter, r *http.Request) {
	principal, err := h.currentPrincipal(r)
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	h.writeAvatar(w, r, principal, principal.User.ID)
}

func (h *Handler) personalAvatarRead(w http.ResponseWriter, r *http.Request) {
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
	// Avatar reads require an authenticated session, but do not add a second
	// per-user permission gate. Existing list endpoints still enforce their own
	// permissions; an authenticated user may render the avatars returned by a
	// page they can already access.
	h.writeAvatar(w, r, principal, id)
}

func (h *Handler) writeAvatar(w http.ResponseWriter, r *http.Request, _ domain.Principal, userID string) {
	avatar, err := h.personal.Avatar(r.Context(), userID)
	if errors.Is(err, application.ErrAvatarNotFound) {
		writeProblem(w, http.StatusNotFound, "avatar-not-found")
		return
	}
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", avatar.MediaType)
	if version := r.URL.Query().Get("v"); version != "" && version == strconv.FormatInt(avatar.Version, 10) {
		w.Header().Set("ETag", `"`+version+`"`)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(avatar.Bytes)
}

func parseAvatarMultipart(w http.ResponseWriter, r *http.Request) ([]byte, string, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return nil, "", errUnsupportedMedia
	}
	if r.ContentLength > maxAvatarMultipartBody {
		return nil, "", errBodyTooLarge
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarMultipartBody)
	if err := r.ParseMultipartForm(64 * 1024); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "request body too large") {
			return nil, "", errBodyTooLarge
		}
		return nil, "", err
	}
	if r.MultipartForm == nil {
		return nil, "", errInvalidJSON
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	files := r.MultipartForm.File["avatar"]
	if len(files) != 1 {
		return nil, "", &domain.ValidationErrors{Items: []domain.FieldError{{Field: "avatar", Code: "required"}}}
	}
	file := files[0]
	handle, err := file.Open()
	if err != nil {
		return nil, "", application.ErrDependencyUnavailable
	}
	defer handle.Close()
	data, err := io.ReadAll(io.LimitReader(handle, application.MaxAvatarBytes+1))
	if err != nil {
		return nil, "", errBodyTooLarge
	}
	if len(data) > application.MaxAvatarBytes {
		return nil, "", errBodyTooLarge
	}
	return data, file.Header.Get("Content-Type"), nil
}
