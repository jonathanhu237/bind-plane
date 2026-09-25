package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/adapter/password"
	postgresadapter "github.com/jonathanhu237/bind-plane/next/api/internal/auth/adapter/postgres"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	"github.com/jonathanhu237/bind-plane/next/api/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// TestOnlineHTTPIntegration exercises the real authentication application,
// PostgreSQL account, session, limiter, and version authority through the HTTP
// handlers and operation log persistence. It is gated so the normal unit suite
// stays self-contained; run it with a disposable migrated database.
func integrationDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn != "" {
		return dsn
	}
	if os.Getenv("TEMVIA_REQUIRED_INTEGRATION") == "1" {
		t.Fatalf("TEST_POSTGRES_DSN is required for mandatory HTTP/PostgreSQL integration tests")
	}
	t.Skip("TEST_POSTGRES_DSN is not set")
	return ""
}

func TestOnlineHTTPIntegration(t *testing.T) {
	db, ctx := openHTTPIntegrationDatabase(t)

	cfg := testConfig()
	cfg.SessionIdleTimeout = time.Hour
	cfg.SessionAbsoluteTimeout = 2 * time.Hour
	cfg.LoginGlobalCapacity = 100
	cfg.LoginGlobalRefillInterval = time.Millisecond
	cfg.LoginEmailCapacity = 100
	cfg.LoginEmailRefillInterval = time.Millisecond
	sessions := postgresadapter.NewStore(db, cfg)

	hasher, err := password.NewHasher(2)
	if err != nil {
		t.Fatal(err)
	}
	accounts := sessions
	fixture := createOnlineHTTPFixture(t, ctx, db, hasher)
	var sessionCookies []*http.Cookie
	t.Cleanup(func() {
		if err := cleanupOnlineHTTPFixture(db, sessions, fixture, sessionCookies); err != nil {
			t.Errorf("clean online HTTP fixture: %v", err)
		}
	})

	auth := application.NewAuthentication(accounts, hasher, sessions, sessions, application.CryptoRandom(), domain.DefaultPermissionCatalog())
	operationLogs := application.NewOperationLogService(accounts)
	handler := NewHandlerWithAccessAndOperationLog(
		&setupFake{status: application.SetupComplete},
		auth,
		cfg,
		nil,
		nil,
		nil,
		nil,
		operationLogs,
	)

	managerCookie := loginOnlineHTTPUser(t, handler, fixture.managerEmail, fixture.managerPassword)
	sessionCookies = append(sessionCookies, managerCookie)
	targetCookieA := loginOnlineHTTPUser(t, handler, fixture.targetEmail, fixture.targetPassword)
	sessionCookies = append(sessionCookies, targetCookieA)
	targetCookieB := loginOnlineHTTPUser(t, handler, fixture.targetEmail, fixture.targetPassword)
	sessionCookies = append(sessionCookies, targetCookieB)
	otherCookie := loginOnlineHTTPUser(t, handler, fixture.otherEmail, fixture.otherPassword)
	sessionCookies = append(sessionCookies, otherCookie)

	users := onlineHTTPList(t, handler, managerCookie)
	target := findOnlineHTTPUser(t, users, fixture.targetID)
	if target.SessionCount != 2 || target.Email != fixture.targetEmail {
		t.Fatalf("target online projection = %+v, want two sessions for %s", target, fixture.targetEmail)
	}

	kickPath := "/api/online-users/" + fixture.targetID + "/kick"
	if response := onlineHTTPRequest(handler, http.MethodPost, kickPath, ``, managerCookie); response.Code != http.StatusNoContent {
		t.Fatalf("kick status = %d, body=%s", response.Code, response.Body.String())
	}
	for name, cookie := range map[string]*http.Cookie{"target A": targetCookieA, "target B": targetCookieB} {
		if response := onlineHTTPRequest(handler, http.MethodGet, "/api/auth/session-status", ``, cookie); response.Code != http.StatusUnauthorized {
			t.Errorf("%s session status = %d, want 401", name, response.Code)
		}
		if response := onlineHTTPRequest(handler, http.MethodGet, "/api/auth/me", ``, cookie); response.Code != http.StatusUnauthorized {
			t.Errorf("%s /me = %d, want 401", name, response.Code)
		}
	}
	if response := onlineHTTPRequest(handler, http.MethodGet, "/api/auth/me", ``, otherCookie); response.Code != http.StatusOK {
		t.Fatalf("unrelated user /me = %d, want 200", response.Code)
	}
	newTargetCookie := loginOnlineHTTPUser(t, handler, fixture.targetEmail, fixture.targetPassword)
	sessionCookies = append(sessionCookies, newTargetCookie)
	if response := onlineHTTPRequest(handler, http.MethodGet, "/api/auth/me", ``, newTargetCookie); response.Code != http.StatusOK {
		t.Fatalf("target relogin /me = %d, want 200", response.Code)
	}
	if response := onlineHTTPRequest(handler, http.MethodPost, "/api/auth/logout", ``, newTargetCookie); response.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, body=%s", response.Code, response.Body.String())
	}
	if response := onlineHTTPRequest(handler, http.MethodGet, "/api/auth/session-status", ``, newTargetCookie); response.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out session status = %d, want 401", response.Code)
	}

	logsResponse := onlineHTTPRequest(handler, http.MethodGet, "/api/operation-logs?action=users.sessions.revoke&objectId="+fixture.targetID, ``, managerCookie)
	if logsResponse.Code != http.StatusOK {
		t.Fatalf("operation log list status = %d, body=%s", logsResponse.Code, logsResponse.Body.String())
	}
	var logs struct {
		Logs []struct {
			Actor struct {
				ID string `json:"id"`
			} `json:"actor"`
			Action   string         `json:"action"`
			ObjectID string         `json:"objectId"`
			Result   string         `json:"result"`
			Details  map[string]any `json:"details"`
		} `json:"logs"`
	}
	if err := json.Unmarshal(logsResponse.Body.Bytes(), &logs); err != nil {
		t.Fatal(err)
	}
	if len(logs.Logs) != 1 || logs.Logs[0].Actor.ID != fixture.managerID || logs.Logs[0].Action != "users.sessions.revoke" || logs.Logs[0].ObjectID != fixture.targetID || logs.Logs[0].Result != application.OperationLogSuccess {
		t.Fatalf("success operation logs = %+v", logs.Logs)
	}
	if strings.Contains(logsResponse.Body.String(), targetCookieA.Value) || strings.Contains(logsResponse.Body.String(), targetCookieB.Value) {
		t.Fatal("operation log response contains a session credential")
	}

	missingID := "10000000-0000-4000-8000-000000000099"
	failedKick := onlineHTTPRequest(handler, http.MethodPost, "/api/online-users/"+missingID+"/kick", ``, managerCookie)
	if failedKick.Code != http.StatusNotFound {
		t.Fatalf("missing target kick status = %d, body=%s", failedKick.Code, failedKick.Body.String())
	}
	failedLogsResponse := onlineHTTPRequest(handler, http.MethodGet, "/api/operation-logs?action=users.sessions.revoke&objectId="+missingID+"&result=failure", ``, managerCookie)
	if failedLogsResponse.Code != http.StatusOK {
		t.Fatalf("failure operation log list status = %d, body=%s", failedLogsResponse.Code, failedLogsResponse.Body.String())
	}
	if !strings.Contains(failedLogsResponse.Body.String(), `"result":"failure"`) {
		t.Fatalf("failure operation log response = %s", failedLogsResponse.Body.String())
	}
}

func TestOnlineHTTPNoTouchIntegration(t *testing.T) {
	db, ctx := openHTTPIntegrationDatabase(t)
	accounts := postgresadapter.NewStore(db)
	hasher, err := password.NewHasher(2)
	if err != nil {
		t.Fatal(err)
	}
	fixture := createOnlineHTTPFixture(t, ctx, db, hasher)

	baseConfig := testConfig()
	baseConfig.SessionAbsoluteTimeout = 5 * time.Second
	baseConfig.LoginGlobalCapacity = 100
	baseConfig.LoginGlobalRefillInterval = time.Millisecond
	baseConfig.LoginEmailCapacity = 100
	baseConfig.LoginEmailRefillInterval = time.Millisecond
	shortConfig := baseConfig
	shortConfig.SessionIdleTimeout = 800 * time.Millisecond
	shortSessions := postgresadapter.NewStore(db, shortConfig)
	longConfig := baseConfig
	longConfig.SessionIdleTimeout = time.Hour
	longSessions := postgresadapter.NewStore(db, longConfig)
	shortCookies := make([]*http.Cookie, 0, 2)
	longCookies := make([]*http.Cookie, 0, 1)
	t.Cleanup(func() {
		if err := cleanupOnlineHTTPFixture(db, shortSessions, fixture, shortCookies); err != nil {
			t.Errorf("clean no-touch online HTTP fixture: %v", err)
		}
		if err := cleanupSessionCookies(longSessions, longCookies); err != nil {
			t.Errorf("clean no-touch observer sessions: %v", err)
		}
	})

	shortAuth := application.NewAuthentication(accounts, hasher, shortSessions, shortSessions, application.CryptoRandom(), domain.DefaultPermissionCatalog())
	shortHandler := NewHandlerWithAccessAndOperationLog(&setupFake{status: application.SetupComplete}, shortAuth, shortConfig, nil, nil, nil, nil, nil)
	longAuth := application.NewAuthentication(accounts, hasher, longSessions, longSessions, application.CryptoRandom(), domain.DefaultPermissionCatalog())
	longHandler := NewHandlerWithAccessAndOperationLog(&setupFake{status: application.SetupComplete}, longAuth, longConfig, nil, nil, nil, nil, nil)

	targetCookie := loginOnlineHTTPUser(t, shortHandler, fixture.targetEmail, fixture.targetPassword)
	shortCookies = append(shortCookies, targetCookie)
	managerCookie := loginOnlineHTTPUser(t, shortHandler, fixture.managerEmail, fixture.managerPassword)
	shortCookies = append(shortCookies, managerCookie)
	observerCookie := loginOnlineHTTPUser(t, longHandler, fixture.otherEmail, fixture.otherPassword)
	longCookies = append(longCookies, observerCookie)

	seenTarget := false
	managerExpired := false
	var lastUsers []application.OnlineUser
	started := time.Now()
	for time.Since(started) < 1300*time.Millisecond {
		managerStatus := onlineHTTPRequest(shortHandler, http.MethodGet, "/api/auth/session-status", ``, managerCookie)
		elapsed := time.Since(started)
		switch {
		case elapsed < 500*time.Millisecond && managerStatus.Code != http.StatusOK:
			t.Fatalf("early no-touch session status = %d after %s", managerStatus.Code, elapsed)
		case elapsed >= 900*time.Millisecond && managerStatus.Code != http.StatusUnauthorized:
			t.Fatalf("repeated no-touch session status = %d after %s, want 401", managerStatus.Code, elapsed)
		case managerStatus.Code == http.StatusUnauthorized:
			managerExpired = true
		case managerStatus.Code != http.StatusOK:
			t.Fatalf("no-touch session status = %d after %s", managerStatus.Code, elapsed)
		}
		lastUsers = onlineHTTPList(t, longHandler, observerCookie)
		for _, user := range lastUsers {
			if user.ID == fixture.targetID {
				seenTarget = true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !seenTarget {
		t.Fatal("observer never saw the short-lived target session in the online list")
	}
	if !managerExpired {
		t.Fatal("manager session remained valid after its 800ms idle timeout")
	}
	if findOnlineHTTPUserOptional(lastUsers, fixture.targetID) != nil {
		t.Fatalf("expired target remained in online list: %+v", lastUsers)
	}
	if response := onlineHTTPRequest(shortHandler, http.MethodGet, "/api/auth/session-status", ``, targetCookie); response.Code != http.StatusUnauthorized {
		t.Fatalf("target no-touch session status = %d, want 401", response.Code)
	}
	if response := onlineHTTPRequest(longHandler, http.MethodGet, "/api/auth/session-status", ``, observerCookie); response.Code != http.StatusOK {
		t.Fatalf("long-lived observer session status = %d, want 200", response.Code)
	}
}

func TestOnlineHTTPConcurrentKickRejectsStaleLogin(t *testing.T) {
	db, ctx := openHTTPIntegrationDatabase(t)
	accounts := postgresadapter.NewStore(db)
	hasher, err := password.NewHasher(2)
	if err != nil {
		t.Fatal(err)
	}
	fixture := createOnlineHTTPFixture(t, ctx, db, hasher)

	cfg := testConfig()
	cfg.SessionIdleTimeout = time.Hour
	cfg.SessionAbsoluteTimeout = 2 * time.Hour
	cfg.LoginGlobalCapacity = 100
	cfg.LoginGlobalRefillInterval = time.Millisecond
	cfg.LoginEmailCapacity = 100
	cfg.LoginEmailRefillInterval = time.Millisecond
	sessions := postgresadapter.NewStore(db, cfg)
	var sessionCookies []*http.Cookie
	t.Cleanup(func() {
		if err := cleanupOnlineHTTPFixture(db, sessions, fixture, sessionCookies); err != nil {
			t.Errorf("clean concurrent online HTTP fixture: %v", err)
		}
	})

	managerAuth := application.NewAuthentication(accounts, hasher, sessions, sessions, application.CryptoRandom(), domain.DefaultPermissionCatalog())
	managerHandler := NewHandlerWithAccessAndOperationLog(&setupFake{status: application.SetupComplete}, managerAuth, cfg, nil, nil, nil, nil, nil)
	blockedSessions := &blockingVersionedSessionStore{
		Store:         sessions,
		createStarted: make(chan struct{}),
		releaseCreate: make(chan struct{}),
	}
	blockedAuth := application.NewAuthentication(accounts, hasher, blockedSessions, blockedSessions, application.CryptoRandom(), domain.DefaultPermissionCatalog())
	blockedHandler := NewHandlerWithAccessAndOperationLog(&setupFake{status: application.SetupComplete}, blockedAuth, cfg, nil, nil, nil, nil, nil)

	managerCookie := loginOnlineHTTPUser(t, managerHandler, fixture.managerEmail, fixture.managerPassword)
	sessionCookies = append(sessionCookies, managerCookie)
	loginDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		loginDone <- onlineHTTPRequest(blockedHandler, http.MethodPost, "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, fixture.targetEmail, fixture.targetPassword), nil)
	}()
	select {
	case <-blockedSessions.createStarted:
	case <-time.After(5 * time.Second):
		blockedSessions.Unblock()
		t.Fatal("concurrent login did not reach the session creation boundary")
	}

	kickPath := "/api/online-users/" + fixture.targetID + "/kick"
	if response := onlineHTTPRequest(managerHandler, http.MethodPost, kickPath, ``, managerCookie); response.Code != http.StatusNoContent {
		blockedSessions.Unblock()
		t.Fatalf("concurrent kick status = %d, body=%s", response.Code, response.Body.String())
	}
	blockedSessions.Unblock()

	var loginResponse *httptest.ResponseRecorder
	select {
	case loginResponse = <-loginDone:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent login did not finish after session creation was released")
	}
	if loginResponse.Code != http.StatusUnauthorized {
		t.Fatalf("stale concurrent login status = %d, body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	// Session creation now rejects stale authority before issuing a credential.
	if len(loginResponse.Result().Cookies()) != 0 {
		t.Fatal("stale login issued a cookie")
	}

	if users := onlineHTTPList(t, managerHandler, managerCookie); findOnlineHTTPUserOptional(users, fixture.targetID) != nil {
		t.Fatalf("stale concurrent login remained online: %+v", users)
	}
	for path := range map[string]struct{}{"/api/auth/session-status": {}, "/api/auth/me": {}} {
		if response := onlineHTTPRequest(managerHandler, http.MethodGet, path, ``, nil); response.Code != http.StatusUnauthorized {
			t.Errorf("stale concurrent login %s status = %d, want 401", path, response.Code)
		}
	}

	freshCookie := loginOnlineHTTPUser(t, managerHandler, fixture.targetEmail, fixture.targetPassword)
	sessionCookies = append(sessionCookies, freshCookie)
	if response := onlineHTTPRequest(managerHandler, http.MethodGet, "/api/auth/me", ``, freshCookie); response.Code != http.StatusOK {
		t.Fatalf("fresh login after concurrent kick /me status = %d, want 200", response.Code)
	}
}

type blockingVersionedSessionStore struct {
	*postgresadapter.Store
	createStarted chan struct{}
	releaseCreate chan struct{}
	startOnce     sync.Once
	releaseOnce   sync.Once
}

func (s *blockingVersionedSessionStore) CreateVersioned(ctx context.Context, sessionID, userID string, authVersion int64) error {
	s.startOnce.Do(func() { close(s.createStarted) })
	select {
	case <-s.releaseCreate:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.Store.CreateVersioned(ctx, sessionID, userID, authVersion)
}

func (s *blockingVersionedSessionStore) Unblock() {
	s.releaseOnce.Do(func() { close(s.releaseCreate) })
}

type onlineHTTPFixture struct {
	managerID       string
	managerEmail    string
	managerPassword string
	targetID        string
	targetEmail     string
	targetPassword  string
	otherEmail      string
	otherPassword   string
	otherID         string
	roleID          string
}

func createOnlineHTTPFixture(t *testing.T, ctx context.Context, db *sql.DB, hasher *password.Hasher) onlineHTTPFixture {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	fixture := onlineHTTPFixture{
		managerEmail:    "online-http-manager-" + suffix + "@example.com",
		managerPassword: "Manager1!x",
		targetEmail:     "online-http-super-" + suffix + "@example.com",
		targetPassword:  "Target1!x",
		otherEmail:      "online-http-other-" + suffix + "@example.com",
		otherPassword:   "Other1!x",
	}
	for _, user := range []struct {
		name     string
		email    string
		password string
		id       *string
	}{
		{name: "Online HTTP Manager", email: fixture.managerEmail, password: fixture.managerPassword, id: &fixture.managerID},
		{name: "Online HTTP Super Admin", email: fixture.targetEmail, password: fixture.targetPassword, id: &fixture.targetID},
		{name: "Online HTTP Other", email: fixture.otherEmail, password: fixture.otherPassword, id: &fixture.otherID},
	} {
		hash, err := hasher.Hash(ctx, user.password)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `INSERT INTO auth_users (name, email, email_canonical, password_hash) VALUES ($1, $2, $2, $3) RETURNING id::text`, user.name, user.email, hash).Scan(user.id); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO auth_roles (name, name_canonical, description) VALUES ($1, $2, $3) RETURNING id::text`, "Online HTTP Manager "+suffix, "online http manager "+suffix, "Integration role").Scan(&fixture.roleID); err != nil {
		t.Fatal(err)
	}
	for _, permission := range []string{"online-users.read", "online-users.write", "operation-logs.read"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO auth_role_permissions (role_id, permission_key) VALUES ($1::uuid, $2)`, fixture.roleID, permission); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_user_roles (user_id, role_id) VALUES ($1::uuid, $2::uuid)`, fixture.managerID, fixture.roleID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_user_roles (user_id, role_id) SELECT id, $1::uuid FROM auth_users WHERE email = $2`, fixture.roleID, fixture.otherEmail); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_user_roles (user_id, role_id) SELECT $1::uuid, id FROM auth_roles WHERE system_key = 'super_admin'`, fixture.targetID); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func cleanupOnlineHTTPFixture(db *sql.DB, sessions *postgresadapter.Store, fixture onlineHTTPFixture, cookies []*http.Cookie) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var cleanupErr error
	cleanupErr = errors.Join(cleanupErr, cleanupSessionCookiesWithContext(cleanupCtx, sessions, cookies))
	userIDs := []string{fixture.managerID, fixture.targetID, fixture.otherID}
	if _, err := db.ExecContext(cleanupCtx, `DELETE FROM auth_operation_logs WHERE actor_user_id IN ($1::uuid, $2::uuid, $3::uuid) OR object_id IN ($4::text, $5::text, $6::text)`, userIDs[0], userIDs[1], userIDs[2], userIDs[0], userIDs[1], userIDs[2]); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	if _, err := db.ExecContext(cleanupCtx, `DELETE FROM auth_users WHERE id IN ($1::uuid, $2::uuid, $3::uuid)`, userIDs[0], userIDs[1], userIDs[2]); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	if _, err := db.ExecContext(cleanupCtx, `DELETE FROM auth_roles WHERE id = $1::uuid`, fixture.roleID); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	var remainingUsers, remainingRoles, remainingLogs int
	if err := db.QueryRowContext(cleanupCtx, `SELECT count(*) FROM auth_users WHERE id IN ($1::uuid, $2::uuid, $3::uuid)`, userIDs[0], userIDs[1], userIDs[2]).Scan(&remainingUsers); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	} else if remainingUsers != 0 {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("%d fixture users remain", remainingUsers))
	}
	if err := db.QueryRowContext(cleanupCtx, `SELECT count(*) FROM auth_roles WHERE id = $1::uuid`, fixture.roleID).Scan(&remainingRoles); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	} else if remainingRoles != 0 {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("%d fixture roles remain", remainingRoles))
	}
	if err := db.QueryRowContext(cleanupCtx, `SELECT count(*) FROM auth_operation_logs WHERE actor_user_id IN ($1::uuid, $2::uuid, $3::uuid) OR object_id IN ($4::text, $5::text, $6::text)`, userIDs[0], userIDs[1], userIDs[2], userIDs[0], userIDs[1], userIDs[2]).Scan(&remainingLogs); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	} else if remainingLogs != 0 {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("%d fixture operation logs remain", remainingLogs))
	}
	return cleanupErr
}

func cleanupSessionCookies(sessions *postgresadapter.Store, cookies []*http.Cookie) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return cleanupSessionCookiesWithContext(cleanupCtx, sessions, cookies)
}

func cleanupSessionCookiesWithContext(ctx context.Context, sessions *postgresadapter.Store, cookies []*http.Cookie) error {
	var cleanupErr error
	for _, cookie := range cookies {
		if cookie != nil {
			cleanupErr = errors.Join(cleanupErr, sessions.Delete(ctx, cookie.Value))
		}
	}
	return cleanupErr
}

func loginOnlineHTTPUser(t *testing.T, handler http.Handler, email, password string) *http.Cookie {
	t.Helper()
	response := onlineHTTPRequest(handler, http.MethodPost, "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("login %s status = %d, body=%s", email, response.Code, response.Body.String())
	}
	return sessionCookieFromResponse(t, response)
}

func sessionCookieFromResponse(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == testConfig().CookieName {
			return cookie
		}
	}
	t.Fatalf("response did not set a session cookie")
	return nil
}

func onlineHTTPRequest(handler http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Origin", testConfig().Origin)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func onlineHTTPList(t *testing.T, handler http.Handler, cookie *http.Cookie) []application.OnlineUser {
	t.Helper()
	response := onlineHTTPRequest(handler, http.MethodGet, "/api/online-users", ``, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("online list status = %d, body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Users []application.OnlineUser `json:"users"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Users
}

func findOnlineHTTPUser(t *testing.T, users []application.OnlineUser, id string) application.OnlineUser {
	t.Helper()
	for _, user := range users {
		if user.ID == id {
			return user
		}
	}
	t.Fatalf("online list did not contain user %s: %+v", id, users)
	return application.OnlineUser{}
}

func findOnlineHTTPUserOptional(users []application.OnlineUser, id string) *application.OnlineUser {
	for index := range users {
		if users[index].ID == id {
			return &users[index]
		}
	}
	return nil
}

// TestAbuseProtectionHTTPIntegration keeps the acceptance boundary at a real
// HTTP handler, real application services, and PostgreSQL. Mail delivery is a
// controlled substitute, but password reset and invitation work still goes
// through the durable outbox and token transactions.
func TestAbuseProtectionHTTPIntegration(t *testing.T) {
	db, ctx := openHTTPIntegrationDatabase(t)

	key := []byte(strings.Repeat("k", 32))
	invitationKey := []byte(strings.Repeat("i", 32))
	cfg := testConfig()
	cfg.TrustedProxyCIDRs = []string{"10.0.0.0/8"}
	cfg.SetupLinkTTL = time.Hour
	cfg.PasswordResetTokenKey = key
	cfg.InvitationTokenKey = invitationKey
	cfg.PasswordResetLinkTTL = time.Hour
	cfg.InvitationLinkTTL = 24 * time.Hour
	cfg.PasswordResetResponseMin = 0
	cfg.MailOutboxNotificationTTL = 24 * time.Hour
	cfg.SessionIdleTimeout = time.Hour
	cfg.SessionAbsoluteTimeout = 2 * time.Hour
	cfg.LoginGlobalCapacity = 100
	cfg.LoginGlobalRefillInterval = time.Hour
	cfg.LoginEmailCapacity = 100
	cfg.LoginEmailRefillInterval = time.Hour
	cfg.LoginIPCapacity = 2
	cfg.LoginIPRefillInterval = time.Hour
	cfg.PasswordResetGlobalCapacity = 100
	cfg.PasswordResetGlobalRefill = time.Hour
	cfg.PasswordResetEmailCapacity = 1
	cfg.PasswordResetEmailRefill = time.Hour
	cfg.PasswordResetIPCapacity = 100
	cfg.PasswordResetIPRefill = time.Hour
	cfg.PasswordResetCompleteIPCapacity = 1
	cfg.PasswordResetCompleteIPRefill = time.Hour
	cfg.InvitationAcceptIPCapacity = 1
	cfg.InvitationAcceptIPRefill = time.Hour
	cfg.SetupIPCapacity = 1
	cfg.SetupIPRefill = time.Hour
	cfg.InvitationSendActorCapacity = 2
	cfg.InvitationSendActorRefill = time.Hour
	cfg.InvitationSendRecipientCapacity = 1
	cfg.InvitationSendRecipientRefill = time.Hour
	cfg.TestEmailGlobalCapacity = 10
	cfg.TestEmailGlobalRefill = time.Hour
	cfg.TestEmailActorCapacity = 2
	cfg.TestEmailActorRefill = time.Hour
	cfg.TestEmailRecipientCapacity = 1
	cfg.TestEmailRecipientRefill = time.Hour

	hasher, err := password.NewHasher(2)
	if err != nil {
		t.Fatal(err)
	}
	store := postgresadapter.NewStore(db, cfg)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	managerEmail := "abuse-manager-" + suffix + "@example.com"
	otherEmail := "abuse-other-" + suffix + "@example.com"
	limitedEmail := "abuse-limited-" + suffix + "@example.com"
	managerPassword := "Manager1!x"
	otherPassword := "Other1!x"
	insertUser := func(name, email, password string) string {
		t.Helper()
		hash, hashErr := hasher.Hash(ctx, password)
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		var id string
		if insertErr := db.QueryRowContext(ctx, `
			INSERT INTO auth_users (name, email, email_canonical, password_hash)
			VALUES ($1, $2, $2, $3) RETURNING id::text`, name, email, hash).Scan(&id); insertErr != nil {
			t.Fatal(insertErr)
		}
		return id
	}
	managerID := insertUser("Abuse Manager", managerEmail, managerPassword)
	otherID := insertUser("Abuse Other", otherEmail, otherPassword)
	limitedID := insertUser("Abuse Limited", limitedEmail, "Limited1!x")
	var noPermissionRoleID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO auth_roles (name, name_canonical, description)
		VALUES ($1, $2, 'Integration role') RETURNING id::text`, "Abuse Reader "+suffix, "abuse reader "+suffix).Scan(&noPermissionRoleID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_role_permissions (role_id, permission_key) VALUES ($1::uuid, 'users.read')`, noPermissionRoleID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_user_roles (user_id, role_id) VALUES ($1::uuid, $2::uuid)`, limitedID, noPermissionRoleID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_user_roles (user_id, role_id) SELECT $1::uuid, id FROM auth_roles WHERE system_key = 'super_admin'`, managerID); err != nil {
		t.Fatal(err)
	}
	var superRoleID string
	if err := db.QueryRowContext(ctx, `SELECT id::text FROM auth_roles WHERE system_key = 'super_admin'`).Scan(&superRoleID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_email_settings
			(singleton, smtp_host, smtp_port, smtp_security, smtp_username, smtp_password_ciphertext, from_address, from_name, default_locale, revision)
		VALUES (true, 'smtp.example.test', 25, 'none', '', NULL, 'no-reply@example.test', 'Temvia', 'en', 1)`); err != nil {
		t.Fatal(err)
	}

	mailer := &abuseRecordingMailer{}
	settings := application.NewSettingsManagement(store, nil, func(application.SMTPSettings) (application.Mailer, error) {
		return mailer, nil
	}, nil)
	settings.SetTestEmailLimiter(store)
	settings.SetMailTaskEnqueuer(store)
	random := application.CryptoRandom()
	setup := application.NewSetup(store, hasher, random, cfg.SetupLinkTTL, store)
	auth := application.NewAuthentication(store, hasher, store, store, random, domain.DefaultPermissionCatalog())
	recovery := application.NewPasswordRecovery(store, store, hasher, random, cfg.PasswordResetTokenKey, cfg.PasswordResetLinkTTL, cfg.MailOutboxNotificationTTL, 0, settings)
	access := application.NewAccessManagementWithInvitations(store, store, domain.DefaultPermissionCatalog(), cfg.InvitationTokenKey, random, cfg.InvitationLinkTTL, settings)
	access.SetInvitationSendLimiter(store)
	acceptance := application.NewInvitationAcceptance(store, hasher, cfg.InvitationTokenKey, store)
	operations := application.NewOperationLogService(store)
	handler := NewHandlerWithAccessAndOperationLog(setup, auth, cfg, recovery, access, acceptance, settings, operations)
	mailTaskBox, err := application.NewMailTaskSecretBox(key)
	if err != nil {
		t.Fatal(err)
	}
	mailDispatcher := application.NewMailDispatcher(store, mailer, random, key, cfg.PublicURL, time.Second, time.Minute, time.Second, time.Minute, invitationKey)
	mailDispatcher.SetMailTaskSecretBox(mailTaskBox)
	drainMail := func() {
		t.Helper()
		for attempt := 0; attempt < 100; attempt++ {
			var pending int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_mail_outbox WHERE sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL`).Scan(&pending); err != nil {
				t.Fatal(err)
			}
			if pending == 0 {
				return
			}
			if err := mailDispatcher.ProcessOnce(ctx); err != nil {
				t.Fatal(err)
			}
		}
		t.Fatal("mail dispatcher did not drain pending integration tasks")
	}
	drainMail()

	// A trusted multi-hop chain with an attacker-controlled prefix produces one
	// source identity in both the limiter and the operation history. Port
	// changes do not create a fresh source bucket.
	managerCookie := abuseHTTPLogin(t, handler, managerEmail, managerPassword, "10.0.0.2:1000", []string{"not-an-ip, 198.51.100.77, 10.0.0.3"})
	badLogin := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":"wrong-password"}`, managerEmail), nil, "10.0.0.2:1001", []string{"198.51.100.77, 10.0.0.3"})
	if badLogin.Code != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d, body=%s", badLogin.Code, badLogin.Body.String())
	}
	blockedLogin := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, managerEmail, managerPassword), nil, "10.0.0.2:1002", []string{"198.51.100.77, 10.0.0.3"})
	if blockedLogin.Code != http.StatusTooManyRequests {
		t.Fatalf("source-exhausted login status = %d, body=%s", blockedLogin.Code, blockedLogin.Body.String())
	}
	spoofedLogin := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, managerEmail, managerPassword), nil, "198.51.100.88:1003", []string{"198.51.100.77"})
	if spoofedLogin.Code != http.StatusOK {
		t.Fatalf("untrusted forwarded login status = %d, body=%s", spoofedLogin.Code, spoofedLogin.Body.String())
	}
	var loginSource string
	if err := db.QueryRowContext(ctx, `
		SELECT source_ip FROM auth_operation_logs
		WHERE action = 'auth.login' AND result = 'success' AND actor_user_id = $1::uuid AND source_ip = '198.51.100.77'
		ORDER BY occurred_at DESC LIMIT 1`, managerID).Scan(&loginSource); err != nil {
		t.Fatal(err)
	}
	if loginSource != "198.51.100.77" {
		t.Fatalf("login operation source = %q, want limiter source 198.51.100.77", loginSource)
	}
	var spoofedSource string
	if err := db.QueryRowContext(ctx, `
		SELECT source_ip FROM auth_operation_logs
		WHERE action = 'auth.login' AND result = 'success' AND actor_user_id = $1::uuid AND source_ip = '198.51.100.88'
		ORDER BY occurred_at DESC LIMIT 1`, managerID).Scan(&spoofedSource); err != nil {
		t.Fatal(err)
	}
	if spoofedSource != "198.51.100.88" {
		t.Fatalf("untrusted forwarded operation source = %q, want peer 198.51.100.88", spoofedSource)
	}

	limitedCookie := abuseHTTPLogin(t, handler, limitedEmail, "Limited1!x", "198.51.100.90:2000", nil)
	knownReset := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/password-reset/request", fmt.Sprintf(`{"email":%q}`, strings.ToUpper(managerEmail)), nil, "198.51.100.101:3000", nil)
	if knownReset.Code != http.StatusAccepted {
		t.Fatalf("known reset status = %d, body=%s", knownReset.Code, knownReset.Body.String())
	}
	unknownEmail := "abuse-unknown-" + suffix + "@example.com"
	unknownReset := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/password-reset/request", fmt.Sprintf(`{"email":%q}`, unknownEmail), nil, "198.51.100.102:3000", nil)
	if unknownReset.Code != http.StatusAccepted {
		t.Fatalf("unknown reset status = %d, body=%s", unknownReset.Code, unknownReset.Body.String())
	}
	var resetSelector []byte
	if err := db.QueryRowContext(ctx, `SELECT selector FROM auth_password_resets WHERE user_id = $1::uuid`, managerID).Scan(&resetSelector); err != nil {
		t.Fatal(err)
	}
	var resetOutboxCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_mail_outbox WHERE user_id = $1::uuid AND kind = 'password_reset'`, managerID).Scan(&resetOutboxCount); err != nil {
		t.Fatal(err)
	}
	repeatedKnown := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/password-reset/request", fmt.Sprintf(`{"email":%q}`, managerEmail), nil, "198.51.100.103:3000", nil)
	if repeatedKnown.Code != http.StatusTooManyRequests {
		t.Fatalf("repeated known reset status = %d, body=%s", repeatedKnown.Code, repeatedKnown.Body.String())
	}
	repeatedUnknown := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/password-reset/request", fmt.Sprintf(`{"email":%q}`, unknownEmail), nil, "198.51.100.104:3000", nil)
	if repeatedUnknown.Code != http.StatusTooManyRequests {
		t.Fatalf("repeated unknown reset status = %d, body=%s", repeatedUnknown.Code, repeatedUnknown.Body.String())
	}
	var resetSelectorAfter []byte
	var resetOutboxCountAfter int
	if err := db.QueryRowContext(ctx, `SELECT selector FROM auth_password_resets WHERE user_id = $1::uuid`, managerID).Scan(&resetSelectorAfter); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_mail_outbox WHERE user_id = $1::uuid AND kind = 'password_reset'`, managerID).Scan(&resetOutboxCountAfter); err != nil {
		t.Fatal(err)
	}
	if string(resetSelectorAfter) != string(resetSelector) || resetOutboxCountAfter != resetOutboxCount {
		t.Fatalf("limited reset changed durable state: selector=%x/%x outbox=%d/%d", resetSelectorAfter, resetSelector, resetOutboxCountAfter, resetOutboxCount)
	}

	otherReset := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/password-reset/request", fmt.Sprintf(`{"email":%q}`, otherEmail), nil, "198.51.100.105:3000", nil)
	if otherReset.Code != http.StatusAccepted {
		t.Fatalf("other reset status = %d, body=%s", otherReset.Code, otherReset.Body.String())
	}
	var otherSelector []byte
	if err := db.QueryRowContext(ctx, `SELECT selector FROM auth_password_resets WHERE user_id = $1::uuid`, otherID).Scan(&otherSelector); err != nil {
		t.Fatal(err)
	}
	otherToken, err := domain.NewPasswordResetToken(cfg.PasswordResetTokenKey, otherSelector)
	if err != nil {
		t.Fatal(err)
	}
	completed := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/password-reset/complete", fmt.Sprintf(`{"token":%q,"password":"NewOther1!x"}`, otherToken), nil, "198.51.100.106:4000", nil)
	if completed.Code != http.StatusNoContent || len(completed.Result().Cookies()) != 1 {
		t.Fatalf("password completion = %d, cookies=%#v, body=%s", completed.Code, completed.Result().Cookies(), completed.Body.String())
	}
	blockedCompletion := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/password-reset/complete", fmt.Sprintf(`{"token":%q,"password":"NewOther1!x"}`, otherToken), nil, "198.51.100.106:4001", nil)
	if blockedCompletion.Code != http.StatusTooManyRequests {
		t.Fatalf("source-exhausted completion = %d, body=%s", blockedCompletion.Code, blockedCompletion.Body.String())
	}
	var otherVersion int64
	if err := db.QueryRowContext(ctx, `SELECT auth_version FROM auth_users WHERE id = $1::uuid`, otherID).Scan(&otherVersion); err != nil {
		t.Fatal(err)
	}
	if otherVersion != 2 {
		t.Fatalf("password reset auth_version = %d, want 2", otherVersion)
	}

	roleID := superRoleID
	invitePayload := func(name, email string) string {
		return fmt.Sprintf(`{"name":%q,"email":%q,"roleIds":[%q]}`, name, email, roleID)
	}
	unauthorizedInvite := abuseHTTPRequest(handler, http.MethodPost, "/api/user-invitations", invitePayload("Unauthorized", "abuse-unauthorized-"+suffix+"@example.com"), limitedCookie, "198.51.100.90:2001", nil)
	if unauthorizedInvite.Code != http.StatusForbidden {
		t.Fatalf("unauthorized invitation status = %d, body=%s", unauthorizedInvite.Code, unauthorizedInvite.Body.String())
	}
	inviteAEmail := "abuse-invite-a-" + suffix + "@example.com"
	createdA := abuseHTTPRequest(handler, http.MethodPost, "/api/user-invitations", invitePayload("Invite A", inviteAEmail), managerCookie, "198.51.100.110:5000", nil)
	if createdA.Code != http.StatusCreated {
		t.Fatalf("create invitation A status = %d, body=%s", createdA.Code, createdA.Body.String())
	}
	var inviteAID string
	var inviteASelector []byte
	if err := db.QueryRowContext(ctx, `SELECT id::text, selector FROM auth_user_invitations WHERE email_canonical = $1`, inviteAEmail).Scan(&inviteAID, &inviteASelector); err != nil {
		t.Fatal(err)
	}
	var inviteAOutbox int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_mail_outbox WHERE invitation_id = $1::uuid`, inviteAID).Scan(&inviteAOutbox); err != nil {
		t.Fatal(err)
	}
	resentA := abuseHTTPRequest(handler, http.MethodPost, "/api/user-invitations/"+inviteAID+"/resend", ``, managerCookie, "198.51.100.110:5001", nil)
	if resentA.Code != http.StatusTooManyRequests {
		t.Fatalf("limited invitation resend status = %d, body=%s", resentA.Code, resentA.Body.String())
	}
	var inviteASelectorAfter []byte
	var inviteAOutboxAfter int
	if err := db.QueryRowContext(ctx, `SELECT selector FROM auth_user_invitations WHERE id = $1::uuid`, inviteAID).Scan(&inviteASelectorAfter); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_mail_outbox WHERE invitation_id = $1::uuid`, inviteAID).Scan(&inviteAOutboxAfter); err != nil {
		t.Fatal(err)
	}
	if string(inviteASelectorAfter) != string(inviteASelector) || inviteAOutboxAfter != inviteAOutbox {
		t.Fatalf("limited resend changed invitation state: selector=%x/%x outbox=%d/%d", inviteASelectorAfter, inviteASelector, inviteAOutboxAfter, inviteAOutbox)
	}
	inviteBEmail := "abuse-invite-b-" + suffix + "@example.com"
	createdB := abuseHTTPRequest(handler, http.MethodPost, "/api/user-invitations", invitePayload("Invite B", inviteBEmail), managerCookie, "198.51.100.110:5002", nil)
	if createdB.Code != http.StatusCreated {
		t.Fatalf("create invitation B after recipient denial status = %d, body=%s", createdB.Code, createdB.Body.String())
	}
	var inviteBID string
	var inviteBSelector []byte
	if err := db.QueryRowContext(ctx, `SELECT id::text, selector FROM auth_user_invitations WHERE email_canonical = $1`, inviteBEmail).Scan(&inviteBID, &inviteBSelector); err != nil {
		t.Fatal(err)
	}
	inviteCEmail := "abuse-invite-c-" + suffix + "@example.com"
	blockedInvite := abuseHTTPRequest(handler, http.MethodPost, "/api/user-invitations", invitePayload("Invite C", inviteCEmail), managerCookie, "198.51.100.110:5003", nil)
	if blockedInvite.Code != http.StatusTooManyRequests {
		t.Fatalf("actor-exhausted invitation status = %d, body=%s", blockedInvite.Code, blockedInvite.Body.String())
	}
	var inviteCCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_user_invitations WHERE email_canonical = $1`, inviteCEmail).Scan(&inviteCCount); err != nil {
		t.Fatal(err)
	}
	if inviteCCount != 0 {
		t.Fatalf("blocked invitation created %d rows", inviteCCount)
	}
	inviteToken, err := domain.NewInvitationToken(cfg.InvitationTokenKey, inviteBSelector)
	if err != nil {
		t.Fatal(err)
	}
	accepted := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/invitations/accept", fmt.Sprintf(`{"token":%q,"password":"Invitee1!x"}`, inviteToken), nil, "198.51.100.120:6000", nil)
	if accepted.Code != http.StatusNoContent {
		t.Fatalf("invitation acceptance status = %d, body=%s", accepted.Code, accepted.Body.String())
	}
	blockedAccepted := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/invitations/accept", fmt.Sprintf(`{"token":%q,"password":"Invitee1!x"}`, inviteToken), nil, "198.51.100.120:6001", nil)
	if blockedAccepted.Code != http.StatusTooManyRequests {
		t.Fatalf("source-exhausted invitation acceptance = %d, body=%s", blockedAccepted.Code, blockedAccepted.Body.String())
	}
	var acceptedUsers int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_users WHERE email_canonical = $1`, inviteBEmail).Scan(&acceptedUsers); err != nil {
		t.Fatal(err)
	}
	if acceptedUsers != 1 {
		t.Fatalf("accepted invitation created %d users, want one", acceptedUsers)
	}
	drainMail()

	testPayload := func(recipient string) string {
		return fmt.Sprintf(`{"recipient":%q}`, recipient)
	}
	initialMailCount := mailer.Count()
	unauthorizedTest := abuseHTTPRequest(handler, http.MethodPost, "/api/settings/email/test", testPayload("abuse-no-permission-"+suffix+"@example.com"), limitedCookie, "198.51.100.90:2002", nil)
	if unauthorizedTest.Code != http.StatusForbidden {
		t.Fatalf("unauthorized test mail status = %d, body=%s", unauthorizedTest.Code, unauthorizedTest.Body.String())
	}
	if mailer.Count() != initialMailCount {
		t.Fatalf("unauthorized test mail sent %d messages", mailer.Count()-initialMailCount)
	}
	testA := "abuse-test-a-" + suffix + "@example.com"
	allowedTest := abuseHTTPRequest(handler, http.MethodPost, "/api/settings/email/test", testPayload(testA), managerCookie, "198.51.100.110:7000", nil)
	if allowedTest.Code != http.StatusAccepted {
		t.Fatalf("first test mail status = %d, body=%s", allowedTest.Code, allowedTest.Body.String())
	}
	drainMail()
	if mailer.Count() != initialMailCount+1 {
		t.Fatalf("first test mail count=%d, want %d", mailer.Count(), initialMailCount+1)
	}
	limitedTest := abuseHTTPRequest(handler, http.MethodPost, "/api/settings/email/test", testPayload(testA), managerCookie, "198.51.100.110:7001", nil)
	if limitedTest.Code != http.StatusTooManyRequests || mailer.Count() != initialMailCount+1 {
		t.Fatalf("recipient-limited test mail = %d, count=%d, body=%s", limitedTest.Code, mailer.Count(), limitedTest.Body.String())
	}
	testB := "abuse-test-b-" + suffix + "@example.com"
	allowedOtherTest := abuseHTTPRequest(handler, http.MethodPost, "/api/settings/email/test", testPayload(testB), managerCookie, "198.51.100.110:7002", nil)
	if allowedOtherTest.Code != http.StatusAccepted {
		t.Fatalf("second actor test mail status = %d, body=%s", allowedOtherTest.Code, allowedOtherTest.Body.String())
	}
	drainMail()
	if mailer.Count() != initialMailCount+2 {
		t.Fatalf("second test mail count=%d, want %d", mailer.Count(), initialMailCount+2)
	}
	blockedActorTest := abuseHTTPRequest(handler, http.MethodPost, "/api/settings/email/test", testPayload("abuse-test-c-"+suffix+"@example.com"), managerCookie, "198.51.100.110:7003", nil)
	if blockedActorTest.Code != http.StatusTooManyRequests || mailer.Count() != initialMailCount+2 {
		t.Fatalf("actor-limited test mail = %d, count=%d, body=%s", blockedActorTest.Code, mailer.Count(), blockedActorTest.Body.String())
	}

	// The authenticated account used for the permission checks remains valid;
	// the password reset above targeted a different account and the rejected
	// mail actions did not revoke or mutate this session.
	if response := abuseHTTPRequest(handler, http.MethodGet, "/api/auth/me", ``, managerCookie, "198.51.100.110:7004", nil); response.Code != http.StatusOK {
		t.Fatalf("manager session after rejected actions = %d, body=%s", response.Code, response.Body.String())
	}
}

func TestAbuseProtectionSetupHTTPIntegration(t *testing.T) {
	db, ctx := openHTTPIntegrationDatabase(t)
	if _, err := db.ExecContext(ctx, `UPDATE auth_setup SET token_digest = NULL, token_expires_at = NULL, completed_at = NULL WHERE singleton = true`); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig()
	cfg.SetupLinkTTL = time.Hour
	cfg.SetupIPCapacity = 1
	cfg.SetupIPRefill = time.Hour
	cfg.SessionIdleTimeout = time.Hour
	cfg.SessionAbsoluteTimeout = 2 * time.Hour
	store := postgresadapter.NewStore(db, cfg)
	hasher, err := password.NewHasher(2)
	if err != nil {
		t.Fatal(err)
	}
	random := application.CryptoRandom()
	setup := application.NewSetup(store, hasher, random, cfg.SetupLinkTTL, store)
	auth := application.NewAuthentication(store, hasher, store, store, random, domain.DefaultPermissionCatalog())
	handler := NewHandler(setup, auth, cfg)
	token, required, err := setup.IssueStartupToken(ctx)
	if err != nil || !required || token == "" {
		t.Fatalf("IssueStartupToken() = %q, %t, %v", token, required, err)
	}
	invalid := abuseHTTPRequest(handler, http.MethodPost, "/api/setup", `{"token":"invalid","name":"Setup User","email":"setup-abuse@example.com","password":"Setup1!x"}`, nil, "198.51.100.220:8000", nil)
	if invalid.Code != http.StatusForbidden {
		t.Fatalf("invalid setup token status = %d, body=%s", invalid.Code, invalid.Body.String())
	}
	limited := abuseHTTPRequest(handler, http.MethodPost, "/api/setup", fmt.Sprintf(`{"token":%q,"name":"Setup User","email":"setup-abuse@example.com","password":"Setup1!x"}`, token), nil, "198.51.100.220:8001", nil)
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("source-limited setup status = %d, body=%s", limited.Code, limited.Body.String())
	}
	allowed := abuseHTTPRequest(handler, http.MethodPost, "/api/setup", fmt.Sprintf(`{"token":%q,"name":"Setup User","email":"setup-abuse@example.com","password":"Setup1!x"}`, token), nil, "198.51.100.221:8000", nil)
	if allowed.Code != http.StatusCreated {
		t.Fatalf("setup after source change status = %d, body=%s", allowed.Code, allowed.Body.String())
	}
	var users int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_users WHERE email_canonical = 'setup-abuse@example.com'`).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if users != 1 {
		t.Fatalf("setup created %d users, want one", users)
	}
}

type abuseRecordingMailer struct {
	mu       sync.Mutex
	messages []application.OutgoingMail
}

func (m *abuseRecordingMailer) Send(_ context.Context, message application.OutgoingMail) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, message)
	return nil
}

func (m *abuseRecordingMailer) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.messages)
}

func abuseHTTPRequest(handler http.Handler, method, path, body string, cookie *http.Cookie, remoteAddr string, forwarded []string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.RemoteAddr = remoteAddr
	request.Header.Set("Origin", testConfig().Origin)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for _, value := range forwarded {
		request.Header.Add("X-Forwarded-For", value)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func abuseHTTPLogin(t *testing.T, handler http.Handler, email, password, remoteAddr string, forwarded []string) *http.Cookie {
	t.Helper()
	response := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password), nil, remoteAddr, forwarded)
	if response.Code != http.StatusOK {
		t.Fatalf("login %s status = %d, body=%s", email, response.Code, response.Body.String())
	}
	return sessionCookieFromResponse(t, response)
}

// openHTTPIntegrationDatabase gives each HTTP integration test its own schema. The
// other integration packages intentionally use the migrated public schema, so
// resetting that schema here would delete their users, roles, and singleton
// settings while their tests are running. Real migrations initialize this
// schema, and the application pool is restricted to it.
func openHTTPIntegrationDatabase(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	dsn := integrationDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin.SetMaxOpenConns(4)
	admin.SetMaxIdleConns(2)
	if err := admin.PingContext(ctx); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	schema := fmt.Sprintf("temvia_abuse_http_%d", time.Now().UnixNano())
	quotedSchema := quoteAbuseSQLIdentifier(schema)
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}

	var appDB *sql.DB
	t.Cleanup(func() {
		if appDB != nil {
			_ = appDB.Close()
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop abuse HTTP schema %s: %v", schema, err)
		}
		_ = admin.Close()
	})

	// ParseConfig supports both PostgreSQL URLs and keyword DSNs. Set the
	// search path on every pooled connection, without inheriting public tables.
	connectionConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	connectionConfig.RuntimeParams["search_path"] = schema
	appDB = stdlib.OpenDB(*connectionConfig)
	appDB.SetMaxOpenConns(32)
	appDB.SetMaxIdleConns(16)
	if err := appDB.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	// Exercise the actual shipped migrations rather than maintaining a second
	// schema, foreign-key list, and seed-data implementation in this fixture.
	migrations, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "migrations", "*.up.sql"))
	if err != nil || len(migrations) == 0 {
		t.Fatalf("find HTTP fixture migrations: %v (found %d)", err, len(migrations))
	}
	var version int64
	for _, migration := range migrations {
		contents, readErr := os.ReadFile(migration)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err := appDB.ExecContext(ctx, string(contents)); err != nil {
			t.Fatalf("apply %s: %v", migration, err)
		}
		version, err = strconv.ParseInt(strings.SplitN(filepath.Base(migration), "_", 2)[0], 10, 64)
		if err != nil {
			t.Fatalf("migration version %s: %v", migration, err)
		}
	}
	if _, err := appDB.ExecContext(ctx, `CREATE TABLE schema_migrations (version bigint PRIMARY KEY, dirty boolean NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := appDB.ExecContext(ctx, `INSERT INTO schema_migrations (version, dirty) VALUES ($1, false)`, version); err != nil {
		t.Fatal(err)
	}
	if err := postgresadapter.NewStore(appDB).CheckSchema(ctx); err != nil {
		t.Fatalf("migrated schema check: %v", err)
	}
	return appDB, ctx
}

func quoteAbuseSQLIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func loadDefaultAbuseConfig(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load(func(key string) string {
		switch key {
		case "PASSWORD_RESET_TOKEN_KEY", "INVITATION_TOKEN_KEY":
			return strings.Repeat("A", 43)
		case "POSTGRES_PASSWORD":
			return "review"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("load default abuse config: %v", err)
	}
	return cfg
}

// TestAbuseProtectionDefaultLoginHTTPIntegration exercises the production
// defaults rather than the small quotas used by the focused acceptance test.
// It demonstrates that shared-IP normal users can log in, a successful login
// does not clear the source bucket, another source remains usable, and an
// exhausted password-reset namespace is independent of login.
func TestAbuseProtectionDefaultLoginHTTPIntegration(t *testing.T) {
	db, ctx := openHTTPIntegrationDatabase(t)
	cfg := loadDefaultAbuseConfig(t)
	if cfg.LoginIPCapacity < 3 || cfg.LoginGlobalCapacity <= cfg.LoginIPCapacity {
		t.Fatalf("default login relationship is not suitable for shared-IP use: global=%d ip=%d", cfg.LoginGlobalCapacity, cfg.LoginIPCapacity)
	}
	store := postgresadapter.NewStore(db, cfg)
	hasher, err := password.NewHasher(2)
	if err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	users := []struct {
		name     string
		email    string
		password string
	}{
		{name: "Default Shared A", email: "default-shared-a-" + suffix + "@example.com", password: "SharedA1!x"},
		{name: "Default Shared B", email: "default-shared-b-" + suffix + "@example.com", password: "SharedB1!x"},
		{name: "Default Other Source", email: "default-other-" + suffix + "@example.com", password: "Other1!x"},
	}
	for _, user := range users {
		hash, hashErr := hasher.Hash(ctx, user.password)
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO auth_users (name, email, email_canonical, password_hash) VALUES ($1, $2, $2, $3)`, user.name, user.email, hash); err != nil {
			t.Fatal(err)
		}
	}
	var superRoleID string
	if err := db.QueryRowContext(ctx, `SELECT id::text FROM auth_roles WHERE system_key = 'super_admin'`).Scan(&superRoleID); err != nil {
		t.Fatal(err)
	}
	for _, user := range users {
		if _, err := db.ExecContext(ctx, `INSERT INTO auth_user_roles (user_id, role_id) SELECT id, $1::uuid FROM auth_users WHERE email_canonical = $2`, superRoleID, user.email); err != nil {
			t.Fatal(err)
		}
	}

	// Exhausting a reset bucket must not consume the login namespace under the
	// same production defaults. Unique recipients avoid the reset email bucket.
	const resetSource = "198.51.100.250"
	for i := 0; i < cfg.PasswordResetGlobalCapacity; i++ {
		allowed, allowErr := store.AllowPasswordResetFromSource(ctx, resetSource, fmt.Sprintf("default-reset-%d-%s@example.com", i, suffix))
		if allowErr != nil || !allowed {
			t.Fatalf("default password-reset request %d = %t, %v", i+1, allowed, allowErr)
		}
	}
	if allowed, allowErr := store.AllowPasswordResetFromSource(ctx, resetSource, "default-reset-final-"+suffix+"@example.com"); allowErr != nil || allowed {
		t.Fatalf("default password-reset bucket after capacity = %t, %v", allowed, allowErr)
	}

	auth := application.NewAuthentication(store, hasher, store, store, application.CryptoRandom(), domain.DefaultPermissionCatalog())
	handler := NewHandler(&setupFake{status: application.SetupComplete}, auth, cfg)
	sharedSource := "198.51.100.240"
	if response := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, users[0].email, users[0].password), nil, sharedSource+":9000", nil); response.Code != http.StatusOK {
		t.Fatalf("first shared-IP login status = %d, body=%s", response.Code, response.Body.String())
	}
	if response := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, users[1].email, users[1].password), nil, sharedSource+":9001", nil); response.Code != http.StatusOK {
		t.Fatalf("second shared-IP login status = %d, body=%s", response.Code, response.Body.String())
	}

	// Consume exactly the remaining source capacity with unknown accounts. A
	// successful login that reset the source bucket would make this loop fail
	// to reach the source denial boundary.
	for i := 0; i < cfg.LoginIPCapacity-2; i++ {
		response := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/login", fmt.Sprintf(`{"email":"default-unknown-%d-%s@example.com","password":"wrong-password"}`, i, suffix), nil, sharedSource+fmt.Sprintf(":%d", 9100+i), nil)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unknown shared-IP login %d status = %d, body=%s", i+1, response.Code, response.Body.String())
		}
	}
	blocked := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, users[0].email, users[0].password), nil, sharedSource+":9200", nil)
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("source-exhausted default login status = %d, body=%s", blocked.Code, blocked.Body.String())
	}
	otherSource := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, users[2].email, users[2].password), nil, "198.51.100.241:9200", nil)
	if otherSource.Code != http.StatusOK {
		t.Fatalf("independent-source default login status = %d, body=%s", otherSource.Code, otherSource.Body.String())
	}
	stillBlocked := abuseHTTPRequest(handler, http.MethodPost, "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, users[1].email, users[1].password), nil, sharedSource+":9201", nil)
	if stillBlocked.Code != http.StatusTooManyRequests {
		t.Fatalf("successful other-source login cleared shared source bucket: status=%d body=%s", stillBlocked.Code, stillBlocked.Body.String())
	}
}
