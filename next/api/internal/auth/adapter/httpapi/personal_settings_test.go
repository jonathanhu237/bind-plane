package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

const personalHTTPUserID = "019535d9-3df7-79fb-b466-fa907fa17f9e"

var personalHTTPUser = domain.User{ID: personalHTTPUserID, Name: "Ada", Email: "ada@example.com", Locale: domain.LocaleEnglish, HasAvatar: true, AvatarVersion: 2}

type personalHTTPFake struct {
	user       domain.User
	avatar     domain.Avatar
	profileErr error
	calls      map[string]int
}

func newPersonalHTTPFake() *personalHTTPFake {
	return &personalHTTPFake{user: personalHTTPUser, avatar: domain.Avatar{MediaType: "image/png", Bytes: []byte("png-bytes"), Version: 2}, calls: make(map[string]int)}
}

func (f *personalHTTPFake) count(name string) { f.calls[name]++ }
func (f *personalHTTPFake) Profile(context.Context, string) (domain.User, error) {
	f.count("profile")
	return f.user, f.profileErr
}
func (f *personalHTTPFake) UpdateName(_ context.Context, id, name string) (domain.User, error) {
	f.count("name")
	f.user.ID, f.user.Name = id, name
	return f.user, nil
}
func (f *personalHTTPFake) UpdateLocale(_ context.Context, id string, locale domain.Locale) (domain.User, error) {
	f.count("locale")
	f.user.ID, f.user.Locale = id, locale
	return f.user, nil
}
func (f *personalHTTPFake) SaveAvatar(_ context.Context, id string, data []byte, mediaType string) (domain.User, error) {
	f.count("avatar-save")
	f.user.ID, f.user.HasAvatar, f.user.AvatarVersion = id, true, f.user.AvatarVersion+1
	f.avatar = domain.Avatar{MediaType: mediaType, Bytes: append([]byte(nil), data...), Version: f.user.AvatarVersion}
	return f.user, nil
}
func (f *personalHTTPFake) RemoveAvatar(_ context.Context, id string) (domain.User, error) {
	f.count("avatar-remove")
	f.user.ID, f.user.HasAvatar, f.user.AvatarVersion = id, false, 0
	f.avatar = domain.Avatar{}
	return f.user, nil
}
func (f *personalHTTPFake) Avatar(context.Context, string) (domain.Avatar, error) {
	f.count("avatar-read")
	return f.avatar, nil
}
func (f *personalHTTPFake) ChangePassword(_ context.Context, id, _, _ string) (domain.User, time.Time, error) {
	f.count("password")
	f.user.ID = id
	return f.user, time.Unix(100, 0), nil
}
func (f *personalHTTPFake) EmailChangeStatus(context.Context, string) (domain.EmailChangeRequest, error) {
	f.count("email-status")
	return domain.EmailChangeRequest{ID: "019535d9-3df7-79fb-b466-fa907fa17f8f", OldEmail: f.user.Email, NewEmail: "new@example.com", Selector: []byte("secret-selector"), VerifierDigest: []byte("secret-digest"), AttemptsRemaining: 5, Revision: 1, ExpiresAt: time.Now().Add(time.Minute), ResendAfter: time.Now()}, nil
}
func (f *personalHTTPFake) RequestEmailChange(context.Context, string, string, string) (domain.EmailChangeRequest, error) {
	f.count("email-request")
	return domain.EmailChangeRequest{ID: "019535d9-3df7-79fb-b466-fa907fa17f8f", OldEmail: f.user.Email, NewEmail: "new@example.com", AttemptsRemaining: 5, Revision: 1, ExpiresAt: time.Now().Add(time.Minute), ResendAfter: time.Now()}, nil
}
func (f *personalHTTPFake) ResendEmailChange(context.Context, string) (domain.EmailChangeRequest, error) {
	f.count("email-resend")
	return domain.EmailChangeRequest{ID: "019535d9-3df7-79fb-b466-fa907fa17f8f", OldEmail: f.user.Email, NewEmail: "new@example.com", AttemptsRemaining: 5, Revision: 2, ExpiresAt: time.Now().Add(time.Minute), ResendAfter: time.Now()}, nil
}
func (f *personalHTTPFake) CompleteEmailChange(_ context.Context, id, _, _ string) (domain.User, time.Time, error) {
	f.count("email-verify")
	f.user.ID, f.user.Email = id, "new@example.com"
	return f.user, time.Unix(101, 0), nil
}

func personalHTTPHandler(personal *personalHTTPFake) http.Handler {
	return NewHandlerWithAccessAndOperationLogAndIdentityAndPersonalSettings(
		&setupFake{status: application.SetupComplete}, &authFake{user: personalHTTPUser}, testConfig(), nil, nil, nil, nil, nil, nil, personal,
	)
}

func personalRequest(handler http.Handler, method, path, body string, cookie bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie {
		req.AddCookie(&http.Cookie{Name: testConfig().CookieName, Value: "session"})
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestPersonalSettingsHTTPRequiresSessionAndDoesNotExposeVerifierMaterial(t *testing.T) {
	personal := newPersonalHTTPFake()
	handler := personalHTTPHandler(personal)
	if response := personalRequest(handler, http.MethodGet, "/api/auth/me/profile", "", false); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous profile status = %d, body=%s", response.Code, response.Body.String())
	}
	response := personalRequest(handler, http.MethodGet, "/api/auth/me/profile", "", true)
	if response.Code != http.StatusOK {
		t.Fatalf("profile status = %d, body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(body)
	if bytes.Contains(encoded, []byte("secret-selector")) || bytes.Contains(encoded, []byte("secret-digest")) || bytes.Contains(encoded, []byte("verifier")) {
		t.Fatalf("profile response exposed verifier material: %s", encoded)
	}
	if personal.calls["email-status"] != 1 {
		t.Fatalf("email status calls = %d", personal.calls["email-status"])
	}
}

func TestPersonalSettingsHTTPEnforcesOriginAndAllowsAuthenticatedAvatarReads(t *testing.T) {
	personal := newPersonalHTTPFake()
	handler := personalHTTPHandler(personal)
	badOrigin := httptest.NewRequest(http.MethodPut, "/api/auth/me/profile", bytes.NewBufferString(`{"name":"Grace"}`))
	badOrigin.Header.Set("Content-Type", "application/json")
	badOrigin.AddCookie(&http.Cookie{Name: testConfig().CookieName, Value: "session"})
	badResponse := httptest.NewRecorder()
	handler.ServeHTTP(badResponse, badOrigin)
	if badResponse.Code != http.StatusForbidden || personal.calls["name"] != 0 {
		t.Fatalf("bad origin response = %d, name calls=%d", badResponse.Code, personal.calls["name"])
	}

	other := "019535d9-3df7-79fb-b466-fa907fa17ff0"
	otherAvatar := personalRequest(handler, http.MethodGet, "/api/users/"+other+"/avatar", "", true)
	if otherAvatar.Code != http.StatusOK || personal.calls["avatar-read"] != 1 {
		t.Fatalf("other avatar response = %d, avatar calls=%d", otherAvatar.Code, personal.calls["avatar-read"])
	}
	owned := personalRequest(handler, http.MethodGet, "/api/users/"+personalHTTPUserID+"/avatar?v=2", "", true)
	if owned.Code != http.StatusOK || owned.Header().Get("Content-Type") != "image/png" || owned.Header().Get("Cache-Control") != "private, no-store" || owned.Body.String() != "png-bytes" {
		t.Fatalf("owned avatar response = %d, headers=%#v, body=%q", owned.Code, owned.Header(), owned.Body.String())
	}
}

func TestPersonalAvatarMultipartValidationReturnsFieldProblem(t *testing.T) {
	personal := newPersonalHTTPFake()
	handler := personalHTTPHandler(personal)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	field, err := writer.CreateFormField("not-avatar")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := field.Write([]byte("ignored")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/auth/me/avatar", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", testConfig().Origin)
	req.AddCookie(&http.Cookie{Name: testConfig().CookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusUnprocessableEntity || !bytes.Contains(response.Body.Bytes(), []byte(`"pointer":"/avatar"`)) || personal.calls["avatar-save"] != 0 {
		t.Fatalf("missing avatar response = %d, calls=%d, body=%s", response.Code, personal.calls["avatar-save"], response.Body.String())
	}
}

func TestPersonalAvatarMultipartUploadUsesServerDecodeAndReturnsVersionedProjection(t *testing.T) {
	personal := newPersonalHTTPFake()
	handler := personalHTTPHandler(personal)
	var imageBytes bytes.Buffer
	avatarImage := image.NewRGBA(image.Rect(0, 0, 2, 2))
	avatarImage.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&imageBytes, avatarImage); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("avatar", "avatar.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(imageBytes.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/auth/me/avatar", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", testConfig().Origin)
	req.AddCookie(&http.Cookie{Name: testConfig().CookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK || personal.calls["avatar-save"] != 1 {
		t.Fatalf("avatar upload response = %d, calls=%d, body=%s", response.Code, personal.calls["avatar-save"], response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"hasAvatar":true`)) || !bytes.Contains(response.Body.Bytes(), []byte(`"avatarVersion":3`)) {
		t.Fatalf("avatar projection = %s", response.Body.String())
	}
}
