package httpapi

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	imageDraw "golang.org/x/image/draw"
)

const maxSystemIdentityMultipartBody = application.MaxSystemIconBytes + 128*1024

type systemIdentityResponse struct {
	SystemName    string `json:"systemName"`
	IconURL       string `json:"iconUrl"`
	HasCustomIcon bool   `json:"hasCustomIcon"`
	Revision      int64  `json:"revision"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
}

func systemIdentityResponseBody(r *http.Request, view application.SystemIdentityView) systemIdentityResponse {
	// The revision is part of the URL so a published replacement cannot be
	// hidden by a browser or CDN cache holding the previous icon.
	iconURL := "/api/public/system-identity/icon?v=" + strconv.FormatInt(view.Revision, 10)
	if !view.HasCustomIcon() {
		iconURL = "/api/public/system-identity/icon?default=1&style=layers"
	}
	body := systemIdentityResponse{
		SystemName:    view.SystemName,
		IconURL:       iconURL,
		HasCustomIcon: view.HasCustomIcon(),
		Revision:      view.Revision,
	}
	if !view.UpdatedAt.IsZero() {
		body.UpdatedAt = view.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	return body
}

func (h *Handler) publicSystemIdentity(w http.ResponseWriter, r *http.Request) {
	if h.identity == nil {
		h.notFound(w, r)
		return
	}
	view, err := h.identity.CurrentSystemIdentity(r.Context())
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, systemIdentityResponseBody(r, view))
}

func (h *Handler) publicSystemIdentityIcon(w http.ResponseWriter, r *http.Request) {
	if h.identity == nil {
		h.notFound(w, r)
		return
	}
	view, err := h.identity.CurrentSystemIdentity(r.Context())
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	forceDefault := r.URL.Query().Get("default") == "1"
	mediaType := view.IconMediaType
	data := view.IconBytes
	if forceDefault || !view.HasCustomIcon() {
		mediaType, data = application.DefaultSystemIcon()
	} else {
		var renderErr error
		data, renderErr = centeredSystemIconPNG(data)
		if renderErr != nil {
			writeApplicationError(w, application.ErrDependencyUnavailable)
			return
		}
		mediaType = "image/png"
	}
	// Callers should include the revision query emitted by the identity
	// response. Immutable caching is safe because a new revision gets a new
	// URL; the no-store fallback still serves direct requests correctly.
	if !forceDefault && r.URL.Query().Get("v") == strconv.FormatInt(view.Revision, 10) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.Header().Set("Content-Type", mediaType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// centeredSystemIconPNG makes the browser brand and favicon use the same
// published image while guaranteeing a square, transparent canvas. This keeps
// a non-square upload centered with its aspect ratio intact in user agents
// that otherwise treat favicon pixels as a stretchable bitmap.
func centeredSystemIconPNG(data []byte) ([]byte, error) {
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	const side = 512
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, errors.New("invalid icon dimensions")
	}
	scale := float64(side) / float64(width)
	if heightScale := float64(side) / float64(height); heightScale < scale {
		scale = heightScale
	}
	drawWidth := max(1, int(float64(width)*scale+0.5))
	drawHeight := max(1, int(float64(height)*scale+0.5))
	left := (side - drawWidth) / 2
	top := (side - drawHeight) / 2
	destination := image.NewNRGBA(image.Rect(0, 0, side, side))
	imageDraw.CatmullRom.Scale(destination, image.Rect(left, top, left+drawWidth, top+drawHeight), source, bounds, imageDraw.Over, nil)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, destination); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func (h *Handler) systemIdentity(w http.ResponseWriter, r *http.Request) {
	if h.identity == nil {
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
	view, err := h.identity.GetSystemIdentity(r.Context())
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, systemIdentityResponseBody(r, view))
}

func (h *Handler) saveSystemIdentity(w http.ResponseWriter, r *http.Request) {
	if h.identity == nil {
		h.notFound(w, r)
		return
	}
	const action = "settings.system_identity.update"
	if h.operationOriginFailure(w, r, action, "system_identity") {
		return
	}
	principal, ok := h.operationPrincipal(w, r, action, "system_identity", "")
	if !ok {
		return
	}
	if !principal.SuperAdmin && !principal.Has(domain.PermissionSettingsWrite) {
		h.recordOperation(r, operationFailure(principal.User, action, "system_identity", "", application.ErrForbidden, nil))
		writeProblem(w, http.StatusForbidden, "forbidden")
		return
	}
	input, err := parseSystemIdentityMultipart(w, r)
	if err != nil {
		h.recordOperation(r, operationFailure(principal.User, action, "system_identity", "", err, nil))
		if errors.Is(err, errBodyTooLarge) || errors.Is(err, errUnsupportedMedia) {
			writeDecodeError(w, err)
		} else if isValidation(err) || errors.Is(err, application.ErrInvalidSystemIdentity) {
			writeApplicationError(w, err)
		} else {
			writeProblem(w, http.StatusBadRequest, "invalid-request")
		}
		return
	}
	before, beforeErr := h.identity.GetSystemIdentity(r.Context())
	view, err := h.identity.SaveSystemIdentity(r.Context(), input)
	if err != nil {
		details := map[string]any{"failure": operationErrorCode(err)}
		if beforeErr == nil {
			details["before"] = systemIdentitySnapshot(before)
		}
		h.recordOperation(r, operationFailure(principal.User, action, "system_identity", "", err, details))
		writeApplicationError(w, err)
		return
	}
	details := map[string]any{"before": nil, "after": systemIdentitySnapshot(view), "fieldsModified": []string{"systemName", "icon"}}
	if beforeErr == nil {
		details["before"] = systemIdentitySnapshot(before)
	}
	h.recordOperation(r, application.OperationLogInput{ActorID: principal.User.ID, ActorName: principal.User.Name, ActorEmail: principal.User.Email, Action: action, ObjectType: "system_identity", Result: application.OperationLogSuccess, Details: details})
	writeJSON(w, http.StatusOK, systemIdentityResponseBody(r, view))
}

func parseSystemIdentityMultipart(w http.ResponseWriter, r *http.Request) (application.SystemIdentityInput, error) {
	contentType := r.Header.Get("Content-Type")
	mediaType, _, mediaErr := mime.ParseMediaType(contentType)
	if mediaErr != nil || mediaType != "multipart/form-data" {
		return application.SystemIdentityInput{}, errUnsupportedMedia
	}
	if r.ContentLength > maxSystemIdentityMultipartBody {
		return application.SystemIdentityInput{}, errBodyTooLarge
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSystemIdentityMultipartBody)
	if err := r.ParseMultipartForm(64 * 1024); err != nil {
		if errors.Is(err, http.ErrBodyReadAfterClose) || strings.Contains(strings.ToLower(err.Error()), "request body too large") {
			return application.SystemIdentityInput{}, errBodyTooLarge
		}
		return application.SystemIdentityInput{}, err
	}
	form := r.MultipartForm
	if form == nil {
		return application.SystemIdentityInput{}, errInvalidJSON
	}
	revision, err := parseMultipartInt64(multipartValue(form.Value, "revision"))
	if err != nil {
		return application.SystemIdentityInput{}, fieldValueError{field: "revision"}
	}
	action := strings.TrimSpace(strings.ToLower(multipartValue(form.Value, "iconAction")))
	if action == "" {
		action = application.SystemIconPreserve
	}
	input := application.SystemIdentityInput{
		SystemName: multipartValue(form.Value, "systemName"),
		IconAction: action,
		Revision:   revision,
	}
	if action == application.SystemIconReplace {
		files := form.File["icon"]
		if len(files) != 1 {
			return application.SystemIdentityInput{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "icon", Code: "required"}}}
		}
		file := files[0]
		handle, openErr := file.Open()
		if openErr != nil {
			return application.SystemIdentityInput{}, application.ErrInvalidSystemIdentity
		}
		data, readErr := io.ReadAll(io.LimitReader(handle, application.MaxSystemIconBytes+1))
		_ = handle.Close()
		if readErr != nil {
			return application.SystemIdentityInput{}, application.ErrInvalidSystemIdentity
		}
		input.IconBytes = data
		input.IconMediaType = file.Header.Get("Content-Type")
	}
	return input, nil
}

func parseMultipartInt64(value string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, errors.New("missing revision")
	}
	return strconv.ParseInt(strings.TrimSpace(value), 10, 64)
}

func multipartValue(values map[string][]string, key string) string {
	items := values[key]
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

func systemIdentitySnapshot(view application.SystemIdentityView) map[string]any {
	return map[string]any{
		"systemName":    view.SystemName,
		"hasCustomIcon": view.HasCustomIcon(),
		"revision":      view.Revision,
	}
}
