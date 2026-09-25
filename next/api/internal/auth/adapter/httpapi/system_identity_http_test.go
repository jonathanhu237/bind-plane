package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"testing"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type identityHTTPStoreFake struct {
	record     application.SystemIdentityRecord
	configured bool
	saveCalls  int
}

func (s *identityHTTPStoreFake) GetSystemIdentity(context.Context) (application.SystemIdentityRecord, error) {
	if !s.configured {
		return application.SystemIdentityRecord{}, application.ErrSystemIdentityNotConfigured
	}
	record := s.record
	record.IconBytes = append([]byte(nil), s.record.IconBytes...)
	return record, nil
}

func (s *identityHTTPStoreFake) SaveSystemIdentity(_ context.Context, expected int64, record application.SystemIdentityRecord) (application.SystemIdentityRecord, error) {
	s.saveCalls++
	if s.configured {
		if expected != s.record.Revision {
			return application.SystemIdentityRecord{}, application.ErrStaleRevision
		}
		record.Revision = s.record.Revision + 1
	} else {
		if expected != 0 {
			return application.SystemIdentityRecord{}, application.ErrStaleRevision
		}
		record.Revision = 1
		s.configured = true
	}
	s.record = record
	return record, nil
}

func newIdentityHTTPHandler(t *testing.T, principal domain.Principal, store *identityHTTPStoreFake, recorder *auditRecorderFake) http.Handler {
	t.Helper()
	if recorder == nil {
		recorder = &auditRecorderFake{}
	}
	auth := &settingsHTTPAuth{principal: principal}
	identity := application.NewSystemIdentityManagement(store)
	return NewHandlerWithAccessAndOperationLogAndIdentity(&setupFake{status: application.SetupComplete}, auth, testConfig(), nil, nil, nil, nil, recorder, identity)
}

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(20 + x*50), G: uint8(30 + y*40), B: 220, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func identityMultipartRequest(t *testing.T, method, path string, revision int64, action string, icon []byte) *http.Request {
	return identityMultipartRequestWithValues(t, method, path, revision, action, "星河管理", "icon.png", "image/png", icon)
}

func identityMultipartRequestWithValues(t *testing.T, method, path string, revision int64, action, systemName, fileName, mediaType string, icon []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("systemName", systemName)
	_ = writer.WriteField("revision", strconv.FormatInt(revision, 10))
	_ = writer.WriteField("iconAction", action)
	if icon != nil {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", `form-data; name="icon"; filename="`+fileName+`"`)
		header.Set("Content-Type", mediaType)
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(icon); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", testConfig().Origin)
	req.AddCookie(&http.Cookie{Name: testConfig().CookieName, Value: "session"})
	return req
}

func jpegBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(30 + x*40), G: uint8(80 + y*40), B: 220, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func webpBytes(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString("UklGRjoAAABXRUJQVlA4IC4AAADQAQCdASoDAAIAAgA0JaACdLoB+AADsAD+6mX//SbPE2eJs+EV/5sCua5+agAA")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSystemIdentityHTTPUsesPublicDefaultAndProtectsManagementRead(t *testing.T) {
	store := &identityHTTPStoreFake{}
	handler := newIdentityHTTPHandler(t, domain.Principal{User: domain.User{ID: "admin"}}, store, nil)

	public := httptest.NewRecorder()
	handler.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/api/public/system-identity", nil))
	if public.Code != http.StatusOK || !bytes.Contains(public.Body.Bytes(), []byte(`"systemName":"Temvia"`)) {
		t.Fatalf("public default response = %d %s", public.Code, public.Body.String())
	}
	icon := httptest.NewRecorder()
	handler.ServeHTTP(icon, httptest.NewRequest(http.MethodGet, "/api/public/system-identity/icon?default=1", nil))
	if icon.Code != http.StatusOK || icon.Header().Get("Content-Type") != "image/svg+xml" || !bytes.Contains(icon.Body.Bytes(), []byte("viewBox=\"0 0 64 64\"")) {
		t.Fatalf("default icon response = %d %s %q", icon.Code, icon.Header().Get("Content-Type"), icon.Body.Bytes())
	}
	protected := httptest.NewRecorder()
	protectedRequest := httptest.NewRequest(http.MethodGet, "/api/settings/system-identity", nil)
	protectedRequest.AddCookie(&http.Cookie{Name: testConfig().CookieName, Value: "session"})
	handler.ServeHTTP(protected, protectedRequest)
	if protected.Code != http.StatusForbidden {
		t.Fatalf("protected read without permission = %d", protected.Code)
	}
}

func TestSystemIdentityHTTPAllowsReadOnlyReadButRejectsWrite(t *testing.T) {
	store := &identityHTTPStoreFake{configured: true, record: application.SystemIdentityRecord{SystemName: "星河管理", Revision: 4}}
	readOnly := newIdentityHTTPHandler(t, domain.Principal{User: domain.User{ID: "reader"}, Permissions: []domain.PermissionKey{domain.PermissionSettingsRead}}, store, &auditRecorderFake{})

	read := httptest.NewRecorder()
	readRequest := httptest.NewRequest(http.MethodGet, "/api/settings/system-identity", nil)
	readRequest.AddCookie(&http.Cookie{Name: testConfig().CookieName, Value: "session"})
	readOnly.ServeHTTP(read, readRequest)
	if read.Code != http.StatusOK || !bytes.Contains(read.Body.Bytes(), []byte(`"systemName":"星河管理"`)) {
		t.Fatalf("read-only settings read = %d %s", read.Code, read.Body.String())
	}

	before := store.record
	write := httptest.NewRecorder()
	request := identityMultipartRequest(t, http.MethodPut, "/api/settings/system-identity", before.Revision, application.SystemIconPreserve, nil)
	readOnly.ServeHTTP(write, request)
	if write.Code != http.StatusForbidden || store.saveCalls != 0 || store.record.SystemName != before.SystemName || store.record.Revision != before.Revision {
		t.Fatalf("read-only settings write = %d %s saves=%d record=%#v", write.Code, write.Body.String(), store.saveCalls, store.record)
	}
}

func TestSystemIdentityHTTPValidatesNameBoundariesAndPreservesPublishedIdentity(t *testing.T) {
	icon := pngBytes(t, 2, 2)
	store := &identityHTTPStoreFake{configured: true, record: application.SystemIdentityRecord{SystemName: "Before", IconMediaType: "image/png", IconBytes: icon, Revision: 7}}
	principal := domain.Principal{User: domain.User{ID: "admin"}, Permissions: []domain.PermissionKey{domain.PermissionSettingsWrite}}
	handler := newIdentityHTTPHandler(t, principal, store, nil)

	trimmed := identityMultipartRequestWithValues(t, http.MethodPut, "/api/settings/system-identity", 7, application.SystemIconPreserve, "  "+strings.Repeat("字", 2)+"  ", "icon.png", "image/png", nil)
	trimmedResponse := httptest.NewRecorder()
	handler.ServeHTTP(trimmedResponse, trimmed)
	if trimmedResponse.Code != http.StatusOK || store.record.SystemName != "字字" || bytes.Contains(trimmedResponse.Body.Bytes(), []byte("english"+"SystemName")) {
		t.Fatalf("trimmed name = %d %s record=%#v", trimmedResponse.Code, trimmedResponse.Body.String(), store.record)
	}

	valid50 := strings.Repeat("名", application.MaxSystemNameLength)
	valid50Request := identityMultipartRequestWithValues(t, http.MethodPut, "/api/settings/system-identity", store.record.Revision, application.SystemIconPreserve, valid50, "icon.png", "image/png", nil)
	valid50Response := httptest.NewRecorder()
	handler.ServeHTTP(valid50Response, valid50Request)
	if valid50Response.Code != http.StatusOK || store.record.SystemName != valid50 {
		t.Fatalf("50-character names = %d %s", valid50Response.Code, valid50Response.Body.String())
	}

	beforeInvalid := store.record
	beforeInvalid.IconBytes = append([]byte(nil), store.record.IconBytes...)
	for _, test := range []struct {
		name     string
		system   string
		wantCode int
	}{
		{name: "empty required", system: "   ", wantCode: http.StatusUnprocessableEntity},
		{name: "51 runes", system: strings.Repeat("名", application.MaxSystemNameLength+1), wantCode: http.StatusUnprocessableEntity},
	} {
		t.Run(test.name, func(t *testing.T) {
			revision := store.record.Revision
			request := identityMultipartRequestWithValues(t, http.MethodPut, "/api/settings/system-identity", revision, application.SystemIconPreserve, test.system, "icon.png", "image/png", nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantCode {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
			if store.record.SystemName != beforeInvalid.SystemName || store.record.Revision != beforeInvalid.Revision || !bytes.Equal(store.record.IconBytes, beforeInvalid.IconBytes) {
				t.Fatalf("invalid save changed published identity: %#v", store.record)
			}
		})
	}
}

func TestSystemIdentityHTTPAcceptsJPEGAndWebPAndRejectsInvalidIconPayloads(t *testing.T) {
	store := &identityHTTPStoreFake{configured: true, record: application.SystemIdentityRecord{SystemName: "Brand", Revision: 1}}
	principal := domain.Principal{User: domain.User{ID: "admin"}, Permissions: []domain.PermissionKey{domain.PermissionSettingsWrite}}
	handler := newIdentityHTTPHandler(t, principal, store, nil)

	for _, test := range []struct {
		name      string
		data      []byte
		fileName  string
		mediaType string
		wantType  string
	}{
		{name: "jpeg", data: jpegBytes(t, 3, 2), fileName: "icon.jpg", mediaType: "image/jpeg", wantType: "image/jpeg"},
		{name: "webp", data: webpBytes(t), fileName: "icon.webp", mediaType: "image/webp", wantType: "image/webp"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := identityMultipartRequestWithValues(t, http.MethodPut, "/api/settings/system-identity", store.record.Revision, application.SystemIconReplace, "Brand", test.fileName, test.mediaType, test.data)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || store.record.IconMediaType != test.wantType || !bytes.Equal(store.record.IconBytes, test.data) {
				t.Fatalf("%s save = %d %s type=%s", test.name, response.Code, response.Body.String(), store.record.IconMediaType)
			}
		})
	}

	before := store.record
	before.IconBytes = append([]byte(nil), store.record.IconBytes...)
	for _, test := range []struct {
		name      string
		data      []byte
		fileName  string
		mediaType string
		wantCode  int
	}{
		{name: "mismatched mime", data: pngBytes(t, 2, 2), fileName: "icon.jpg", mediaType: "image/jpeg", wantCode: http.StatusUnprocessableEntity},
		{name: "unsupported type", data: []byte("GIF89a"), fileName: "icon.gif", mediaType: "image/gif", wantCode: http.StatusUnprocessableEntity},
		{name: "oversized", data: bytes.Repeat([]byte{'x'}, application.MaxSystemIconBytes+1), fileName: "icon.png", mediaType: "image/png", wantCode: http.StatusUnprocessableEntity},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := identityMultipartRequestWithValues(t, http.MethodPut, "/api/settings/system-identity", store.record.Revision, application.SystemIconReplace, "Brand", test.fileName, test.mediaType, test.data)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantCode {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
			if store.record.Revision != before.Revision || store.record.SystemName != before.SystemName || store.record.IconMediaType != before.IconMediaType || !bytes.Equal(store.record.IconBytes, before.IconBytes) {
				t.Fatalf("invalid icon changed published identity: %#v", store.record)
			}
		})
	}
}

func TestSystemIdentityHTTPValidatesUploadsConflictAndRecordsAudit(t *testing.T) {
	user := domain.User{ID: "admin", Name: "Ada", Email: "ada@example.com"}
	principal := domain.Principal{User: user, Permissions: []domain.PermissionKey{domain.PermissionSettingsWrite}}
	store := &identityHTTPStoreFake{configured: true, record: application.SystemIdentityRecord{SystemName: "Old", Revision: 3}}
	recorder := &auditRecorderFake{}
	handler := newIdentityHTTPHandler(t, principal, store, recorder)

	valid := pngBytes(t, 3, 1)
	request := identityMultipartRequest(t, http.MethodPut, "/api/settings/system-identity", 3, application.SystemIconReplace, valid)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || store.saveCalls != 1 || len(recorder.inputs) != 1 {
		t.Fatalf("successful save = %d %s saves=%d audit=%#v", response.Code, response.Body.String(), store.saveCalls, recorder.inputs)
	}
	if recorder.inputs[0].Action != "settings.system_identity.update" || recorder.inputs[0].Result != application.OperationLogSuccess {
		t.Fatalf("success audit = %#v", recorder.inputs[0])
	}
	if _, hasIconBytes := recorder.inputs[0].Details["iconBytes"]; hasIconBytes {
		t.Fatalf("audit contains icon bytes: %#v", recorder.inputs[0].Details)
	}
	iconResponse := httptest.NewRecorder()
	handler.ServeHTTP(iconResponse, httptest.NewRequest(http.MethodGet, "/api/public/system-identity/icon?v=4", nil))
	config, format, err := image.DecodeConfig(bytes.NewReader(iconResponse.Body.Bytes()))
	if err != nil || format != "png" || config.Width != config.Height {
		t.Fatalf("favicon derivative = %dx%d format=%q err=%v", config.Width, config.Height, format, err)
	}

	recorder.inputs = nil
	stale := identityMultipartRequest(t, http.MethodPut, "/api/settings/system-identity", 3, application.SystemIconPreserve, nil)
	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, stale)
	if staleResponse.Code != http.StatusConflict || len(recorder.inputs) != 1 || recorder.inputs[0].Result != application.OperationLogFailure {
		t.Fatalf("stale save = %d %s audit=%#v", staleResponse.Code, staleResponse.Body.String(), recorder.inputs)
	}

	recorder.inputs = nil
	full := pngBytes(t, 2, 2)
	truncated := full[:len(full)-8]
	bad := identityMultipartRequest(t, http.MethodPut, "/api/settings/system-identity", 4, application.SystemIconReplace, truncated)
	badResponse := httptest.NewRecorder()
	handler.ServeHTTP(badResponse, bad)
	if badResponse.Code != http.StatusUnprocessableEntity || store.saveCalls != 1 {
		t.Fatalf("truncated upload = %d %s saves=%d", badResponse.Code, badResponse.Body.String(), store.saveCalls)
	}
}
