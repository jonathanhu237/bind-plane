package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/adapter/password"
	postgresadapter "github.com/jonathanhu237/bind-plane/next/api/internal/auth/adapter/postgres"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// TestPersonalSettingsHTTPPostgresIntegration keeps the account-settings
// acceptance boundary at a real HTTP handler, real sessions, real PostgreSQL,
// and the durable mail outbox. The only substitute is the final mail transport;
// its captured message is the same input an SMTP adapter would receive.
func TestPersonalSettingsHTTPPostgresIntegration(t *testing.T) {
	db, ctx := openHTTPIntegrationDatabase(t)
	cfg := testConfig()
	cfg.SessionIdleTimeout = time.Hour
	cfg.SessionAbsoluteTimeout = 2 * time.Hour
	cfg.LoginGlobalCapacity = 100
	cfg.LoginGlobalRefillInterval = time.Millisecond
	cfg.LoginEmailCapacity = 100
	cfg.LoginEmailRefillInterval = time.Millisecond
	cfg.EmailChangeCodeKey = bytes.Repeat([]byte{0x5a}, domain.PasswordResetVerifierBytes)
	cfg.PasswordResetTokenKey = bytes.Repeat([]byte{0x6b}, domain.PasswordResetVerifierBytes)
	cfg.InvitationTokenKey = bytes.Repeat([]byte{0x7c}, domain.PasswordResetVerifierBytes)

	sessions := postgresadapter.NewStore(db, cfg)
	hasher, err := password.NewHasher(2)
	if err != nil {
		t.Fatal(err)
	}
	fixture := createOnlineHTTPFixture(t, ctx, db, hasher)
	mailer := &personalIntegrationMailer{}
	personal := application.NewPersonalSettings(sessions, sessions, sessions, sessions, hasher, application.CryptoRandom(), cfg.EmailChangeCodeKey, time.Hour)
	auth := application.NewAuthentication(sessions, hasher, sessions, sessions, application.CryptoRandom(), domain.DefaultPermissionCatalog())
	operationLogs := application.NewOperationLogService(sessions)
	access := application.NewAccessManagement(sessions, sessions, domain.DefaultPermissionCatalog())
	handler := NewHandlerWithAccessAndOperationLogAndIdentityAndPersonalSettings(
		&setupFake{status: application.SetupComplete},
		auth,
		cfg,
		nil,
		access,
		nil,
		nil,
		operationLogs,
		nil,
		personal,
	)
	dispatcher := application.NewMailDispatcher(sessions, mailer, application.CryptoRandom(), cfg.PasswordResetTokenKey, cfg.PublicURL, 0, time.Minute, time.Millisecond, time.Second, cfg.InvitationTokenKey, cfg.EmailChangeCodeKey)
	mailTaskBox, err := application.NewMailTaskSecretBox(cfg.PasswordResetTokenKey)
	if err != nil {
		t.Fatal(err)
	}
	// The store and worker must share the generated material key. Without this
	// explicit fixture wiring the worker correctly treats persisted messages as
	// an unavailable dependency and the HTTP verification journey never reaches
	// the test mailer.
	dispatcher.SetMailTaskSecretBox(mailTaskBox)

	var cookies []*http.Cookie
	t.Cleanup(func() {
		if err := cleanupSessionCookies(sessions, cookies); err != nil {
			t.Errorf("clean personal HTTP sessions: %v", err)
		}
	})
	adminCookie := personalIntegrationLogin(t, handler, fixture.targetEmail, fixture.targetPassword)
	cookies = append(cookies, adminCookie)
	viewerCookieA := personalIntegrationLogin(t, handler, fixture.managerEmail, fixture.managerPassword)
	cookies = append(cookies, viewerCookieA)
	viewerCookieB := personalIntegrationLogin(t, handler, fixture.managerEmail, fixture.managerPassword)
	cookies = append(cookies, viewerCookieB)

	profile := personalIntegrationJSONRequest(t, handler, http.MethodGet, "/api/auth/me/profile", "", viewerCookieA, nil, http.StatusOK)
	var initial personalIntegrationProfile
	decodePersonalIntegrationJSON(t, profile, &initial)
	if initial.User.ID != fixture.managerID || initial.User.Email != fixture.managerEmail || initial.User.Locale != string(domain.LocaleEnglish) || initial.EmailChange != nil {
		t.Fatalf("initial personal profile projection is not isolated to the viewer account")
	}
	if bytes.Contains(profile.Body.Bytes(), []byte("selector")) || bytes.Contains(profile.Body.Bytes(), []byte("verifier")) || bytes.Contains(profile.Body.Bytes(), []byte("digest")) {
		t.Fatalf("personal profile exposed verifier fields")
	}

	updatedName := "HTTP Personal Viewer"
	if response := onlineHTTPRequest(handler, http.MethodPut, "/api/auth/me/profile", fmt.Sprintf(`{"name":%q}`, updatedName), viewerCookieA); response.Code != http.StatusOK {
		t.Fatalf("profile update status = %d", response.Code)
	}
	if response := onlineHTTPRequest(handler, http.MethodPut, "/api/auth/me/preferences", `{"locale":"zh-CN"}`, viewerCookieA); response.Code != http.StatusOK {
		t.Fatalf("locale update status = %d", response.Code)
	}
	profile = personalIntegrationJSONRequest(t, handler, http.MethodGet, "/api/auth/me/profile", "", viewerCookieA, nil, http.StatusOK)
	decodePersonalIntegrationJSON(t, profile, &initial)
	if initial.User.Name != updatedName || initial.User.Locale != string(domain.LocaleChinese) {
		t.Fatalf("profile/preferences update did not persist to the current account")
	}
	otherProfile := personalIntegrationJSONRequest(t, handler, http.MethodGet, "/api/auth/me/profile", "", adminCookie, nil, http.StatusOK)
	var adminProjection personalIntegrationProfile
	decodePersonalIntegrationJSON(t, otherProfile, &adminProjection)
	if adminProjection.User.Locale != string(domain.LocaleEnglish) || adminProjection.User.Name == updatedName {
		t.Fatalf("preference/profile update leaked across accounts")
	}

	avatarBody, avatarContentType := personalIntegrationMultipart(t, "avatar", "avatar.png")
	avatarResponse := personalIntegrationRequest(handler, http.MethodPut, "/api/auth/me/avatar", string(avatarBody), viewerCookieA, map[string]string{"Content-Type": avatarContentType})
	if avatarResponse.Code != http.StatusOK {
		t.Fatalf("avatar upload status = %d", avatarResponse.Code)
	}
	var avatarEnvelope struct {
		User personalIntegrationUser `json:"user"`
	}
	decodePersonalIntegrationJSON(t, avatarResponse, &avatarEnvelope)
	if !avatarEnvelope.User.HasAvatar || avatarEnvelope.User.AvatarVersion <= 0 || avatarEnvelope.User.AvatarURL == "" {
		t.Fatalf("avatar upload did not return a versioned avatar projection")
	}
	avatarPath := "/api/users/" + fixture.managerID + "/avatar?v=" + fmt.Sprint(avatarEnvelope.User.AvatarVersion)
	avatarRead := personalIntegrationJSONRequest(t, handler, http.MethodGet, avatarPath, "", viewerCookieA, nil, http.StatusOK)
	if avatarRead.Header().Get("Content-Type") != "image/png" || avatarRead.Header().Get("Cache-Control") != "private, no-store" || len(avatarRead.Body.Bytes()) == 0 {
		t.Fatalf("authenticated avatar response did not preserve private image headers")
	}
	if response := personalIntegrationRequest(handler, http.MethodGet, avatarPath, "", nil, map[string]string{"If-None-Match": avatarRead.Header().Get("ETag")}); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous conditional avatar status = %d, want unauthorized", response.Code)
	}
	if response := personalIntegrationRequest(handler, http.MethodGet, avatarPath, "", adminCookie, map[string]string{"If-None-Match": avatarRead.Header().Get("ETag")}); response.Code != http.StatusOK {
		t.Fatalf("privileged conditional avatar status = %d, want ok", response.Code)
	}
	adminAvatarBody, adminAvatarContentType := personalIntegrationMultipart(t, "avatar", "admin-avatar.png")
	if response := personalIntegrationRequest(handler, http.MethodPut, "/api/auth/me/avatar", string(adminAvatarBody), adminCookie, map[string]string{"Content-Type": adminAvatarContentType}); response.Code != http.StatusOK {
		t.Fatalf("privileged avatar upload status = %d", response.Code)
	}
	var adminAvatarProjection struct {
		User personalIntegrationUser `json:"user"`
	}
	adminAvatarResponse := personalIntegrationRequest(handler, http.MethodGet, "/api/auth/me/profile", "", adminCookie, nil)
	if adminAvatarResponse.Code != http.StatusOK {
		t.Fatalf("privileged avatar profile status = %d", adminAvatarResponse.Code)
	}
	decodePersonalIntegrationJSON(t, adminAvatarResponse, &adminAvatarProjection)
	adminAvatarPath := "/api/users/" + fixture.targetID + "/avatar?v=" + fmt.Sprint(adminAvatarProjection.User.AvatarVersion)
	if response := personalIntegrationRequest(handler, http.MethodGet, adminAvatarPath, "", viewerCookieA, nil); response.Code != http.StatusOK {
		t.Fatalf("authenticated non-admin avatar read status = %d, want ok", response.Code)
	}
	users := personalIntegrationJSONRequest(t, handler, http.MethodGet, "/api/users?status=active", "", adminCookie, nil, http.StatusOK)
	if bytes.Contains(users.Body.Bytes(), []byte("image_bytes")) || bytes.Contains(users.Body.Bytes(), []byte("selector")) || bytes.Contains(users.Body.Bytes(), []byte("verifier_digest")) {
		t.Fatalf("user list response exposed image storage or verifier fields")
	}

	newEmail := fmt.Sprintf("personal-http-new-%d@example.com", time.Now().UnixNano())
	requestResponse := personalIntegrationJSONRequest(t, handler, http.MethodPost, "/api/auth/me/email-change", fmt.Sprintf(`{"currentPassword":%q,"newEmail":%q}`, fixture.managerPassword, newEmail), viewerCookieA, nil, http.StatusAccepted)
	var requestEnvelope struct {
		EmailChange personalIntegrationEmailChange `json:"emailChange"`
	}
	decodePersonalIntegrationJSON(t, requestResponse, &requestEnvelope)
	if requestEnvelope.EmailChange.ID == "" || requestEnvelope.EmailChange.NewEmail != newEmail || requestEnvelope.EmailChange.AttemptsRemaining != application.EmailChangeMaxAttempts {
		t.Fatalf("email-change request did not return durable pending state")
	}
	verificationCode := personalIntegrationProcessMail(t, ctx, dispatcher, mailer, newEmail)
	pending := personalIntegrationJSONRequest(t, handler, http.MethodGet, "/api/auth/me/email-change", "", viewerCookieB, nil, http.StatusOK)
	var pendingEnvelope struct {
		EmailChange personalIntegrationEmailChange `json:"emailChange"`
	}
	decodePersonalIntegrationJSON(t, pending, &pendingEnvelope)
	if pendingEnvelope.EmailChange.ID != requestEnvelope.EmailChange.ID || pendingEnvelope.EmailChange.NewEmail != newEmail {
		t.Fatalf("email-change request did not survive a second session")
	}

	if response := onlineHTTPRequest(handler, http.MethodPost, "/api/online-users/"+fixture.managerID+"/kick", "", adminCookie); response.Code != http.StatusNoContent {
		t.Fatalf("force sign-out status = %d", response.Code)
	}
	for name, cookie := range map[string]*http.Cookie{"first session": viewerCookieA, "second session": viewerCookieB} {
		if response := onlineHTTPRequest(handler, http.MethodGet, "/api/auth/session-status", "", cookie); response.Code != http.StatusUnauthorized {
			t.Errorf("%s after force sign-out status = %d", name, response.Code)
		}
	}
	if response := personalIntegrationRequest(handler, http.MethodGet, avatarPath, "", viewerCookieA, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked conditional avatar status = %d, want unauthorized", response.Code)
	}
	if response := personalIntegrationRequest(handler, http.MethodGet, avatarPath, "", adminCookie, nil); response.Code != http.StatusOK {
		t.Fatalf("avatar read after viewer revocation status = %d, want ok", response.Code)
	}

	viewerCookieC := personalIntegrationLogin(t, handler, fixture.managerEmail, fixture.managerPassword)
	cookies = append(cookies, viewerCookieC)
	pending = personalIntegrationJSONRequest(t, handler, http.MethodGet, "/api/auth/me/email-change", "", viewerCookieC, nil, http.StatusOK)
	decodePersonalIntegrationJSON(t, pending, &pendingEnvelope)
	if pendingEnvelope.EmailChange.ID != requestEnvelope.EmailChange.ID {
		t.Fatalf("email-change request did not survive force sign-out")
	}
	newPassword := "PersonalNew1!x"
	if response := onlineHTTPRequest(handler, http.MethodPut, "/api/auth/me/password", fmt.Sprintf(`{"currentPassword":%q,"newPassword":%q,"confirmPassword":%q}`, fixture.managerPassword, newPassword, newPassword), viewerCookieC); response.Code != http.StatusNoContent {
		t.Fatalf("password update status = %d", response.Code)
	}
	if response := onlineHTTPRequest(handler, http.MethodGet, "/api/auth/session-status", "", viewerCookieC); response.Code != http.StatusUnauthorized {
		t.Fatalf("password-changing session status = %d, want unauthorized", response.Code)
	}
	if response := onlineHTTPRequest(handler, http.MethodPost, "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, fixture.managerEmail, fixture.managerPassword), nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("old-password login status = %d, want unauthorized", response.Code)
	}
	viewerCookieD := personalIntegrationLogin(t, handler, fixture.managerEmail, newPassword)
	cookies = append(cookies, viewerCookieD)
	pending = personalIntegrationJSONRequest(t, handler, http.MethodGet, "/api/auth/me/email-change", "", viewerCookieD, nil, http.StatusOK)
	decodePersonalIntegrationJSON(t, pending, &pendingEnvelope)
	if pendingEnvelope.EmailChange.ID != requestEnvelope.EmailChange.ID {
		t.Fatalf("email-change request did not survive password change")
	}

	verifyResponse := personalIntegrationRequest(handler, http.MethodPost, "/api/auth/me/email-change/verify", fmt.Sprintf(`{"requestId":%q,"code":%q}`, requestEnvelope.EmailChange.ID, verificationCode), viewerCookieD, nil)
	if verifyResponse.Code != http.StatusNoContent {
		t.Fatalf("email verification status = %d", verifyResponse.Code)
	}
	if response := onlineHTTPRequest(handler, http.MethodGet, "/api/auth/session-status", "", viewerCookieD); response.Code != http.StatusUnauthorized {
		t.Fatalf("verified session status = %d, want unauthorized", response.Code)
	}
	if response := onlineHTTPRequest(handler, http.MethodPost, "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, fixture.managerEmail, newPassword), nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("old-email login status = %d, want unauthorized", response.Code)
	}
	viewerCookieE := personalIntegrationLogin(t, handler, newEmail, newPassword)
	cookies = append(cookies, viewerCookieE)
	finalProfile := personalIntegrationJSONRequest(t, handler, http.MethodGet, "/api/auth/me/profile", "", viewerCookieE, nil, http.StatusOK)
	var finalProjection personalIntegrationProfile
	decodePersonalIntegrationJSON(t, finalProfile, &finalProjection)
	if finalProjection.User.Email != newEmail || finalProjection.EmailChange != nil {
		t.Fatalf("verified profile did not commit the new email and clear pending state")
	}

	// Drain the two notification jobs committed by the password and email
	// transactions. Their recipients are checked without exposing message
	// bodies, credentials, or verification material in test output.
	for index := 0; index < 8; index++ {
		if err := dispatcher.ProcessOnce(ctx); err != nil {
			t.Fatalf("process security notification: %v", err)
		}
	}
	messages := mailer.snapshot()
	passwordNotice := false
	emailNotice := false
	for _, message := range messages {
		if message.Kind == application.MailPasswordChanged && message.To == fixture.managerEmail {
			passwordNotice = true
		}
		if message.Kind == application.MailEmailChanged && message.To == fixture.managerEmail {
			emailNotice = true
		}
	}
	if !passwordNotice || !emailNotice {
		t.Fatalf("durable security notifications were not delivered to the pre-change address")
	}

	logs := personalIntegrationJSONRequest(t, handler, http.MethodGet, "/api/operation-logs?objectId="+fixture.managerID, "", adminCookie, nil, http.StatusOK)
	logBody := logs.Body.Bytes()
	if bytes.Contains(logBody, []byte(verificationCode)) || bytes.Contains(logBody, []byte(fixture.managerPassword)) || bytes.Contains(logBody, []byte(newPassword)) {
		t.Fatalf("operation log response exposed a verification code or password")
	}
	if bytes.Contains(logBody, []byte("auth.profile.locale.update")) || bytes.Contains(logBody, []byte("theme")) {
		t.Fatalf("operation log response recorded a preference-only change")
	}
}

type personalIntegrationUser struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Locale        string `json:"locale"`
	AvatarURL     string `json:"avatarUrl"`
	HasAvatar     bool   `json:"hasAvatar"`
	AvatarVersion int64  `json:"avatarVersion"`
}

type personalIntegrationEmailChange struct {
	ID                string `json:"id"`
	OldEmail          string `json:"oldEmail"`
	NewEmail          string `json:"newEmail"`
	ResendAvailableAt string `json:"resendAvailableAt"`
	AttemptsRemaining int    `json:"attemptsRemaining"`
}

type personalIntegrationProfile struct {
	User        personalIntegrationUser         `json:"user"`
	EmailChange *personalIntegrationEmailChange `json:"emailChange"`
}

type personalIntegrationMailer struct {
	mu       sync.Mutex
	messages []application.OutgoingMail
}

func (m *personalIntegrationMailer) Send(_ context.Context, message application.OutgoingMail) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, message)
	return nil
}

func (m *personalIntegrationMailer) snapshot() []application.OutgoingMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]application.OutgoingMail(nil), m.messages...)
}

func personalIntegrationLogin(t *testing.T, handler http.Handler, email, password string) *http.Cookie {
	t.Helper()
	response := onlineHTTPRequest(handler, http.MethodPost, "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d", response.Code)
	}
	return sessionCookieFromResponse(t, response)
}

func personalIntegrationJSONRequest(t *testing.T, handler http.Handler, method, path, body string, cookie *http.Cookie, headers map[string]string, wantStatus int) *httptest.ResponseRecorder {
	t.Helper()
	response := personalIntegrationRequest(handler, method, path, body, cookie, headers)
	if response.Code != wantStatus {
		t.Fatalf("%s %s status = %d", method, path, response.Code)
	}
	return response
}

func personalIntegrationRequest(handler http.Handler, method, path, body string, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Origin", testConfig().Origin)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodePersonalIntegrationJSON(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode personal settings response: %v", err)
	}
}

func personalIntegrationMultipart(t *testing.T, fieldName, filename string) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(part, personalIntegrationAvatarImage()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func personalIntegrationAvatarImage() image.Image {
	avatar := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 16; x++ {
			if x < 8 {
				avatar.Set(x, y, color.RGBA{R: 220, A: 255})
			} else {
				avatar.Set(x, y, color.RGBA{B: 220, A: 255})
			}
		}
	}
	return avatar
}

var personalIntegrationCodePattern = regexp.MustCompile(`(?:verification code is:\s*|验证码为：\s*)([0-9]{6})`)

func personalIntegrationProcessMail(t *testing.T, ctx context.Context, dispatcher *application.MailDispatcher, mailer *personalIntegrationMailer, recipient string) string {
	t.Helper()
	for index := 0; index < 8; index++ {
		if err := dispatcher.ProcessOnce(ctx); err != nil {
			t.Fatalf("process durable mail: %v", err)
		}
		for _, message := range mailer.snapshot() {
			if message.Kind != application.MailEmailChangeCode || message.To != recipient {
				continue
			}
			match := personalIntegrationCodePattern.FindStringSubmatch(message.Text)
			if len(match) == 2 {
				return match[1]
			}
		}
	}
	t.Fatalf("verification message was not delivered")
	return ""
}
