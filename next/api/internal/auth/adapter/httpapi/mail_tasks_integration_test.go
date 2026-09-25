package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/adapter/password"
	postgresadapter "github.com/jonathanhu237/bind-plane/next/api/internal/auth/adapter/postgres"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// This journey is opt-in because it needs an isolated PostgreSQL stack. The
// browser test covers Mailpit delivery; this test exercises the real HTTP
// authorization, PostgreSQL worker projection, physical deletion, partial
// conflicts, and safe response boundary in the same generated API package.
func TestMailTasksHTTPIntegration(t *testing.T) {
	if os.Getenv("E2E_EMAIL_TASKS") != "1" {
		if os.Getenv("TEMVIA_REQUIRED_INTEGRATION") == "1" {
			t.Fatalf("E2E_EMAIL_TASKS=1 is required for mandatory HTTP mail-task integration")
		}
		t.Skip("set E2E_EMAIL_TASKS=1 and TEST_POSTGRES_DSN for HTTP/PostgreSQL mail-task acceptance")
	}
	db, ctx := openHTTPIntegrationDatabase(t)
	cfg := testConfig()
	cfg.SessionIdleTimeout = time.Hour
	cfg.SessionAbsoluteTimeout = 2 * time.Hour
	store := postgresadapter.NewStore(db, cfg)
	hasher, err := password.NewHasher(2)
	if err != nil {
		t.Fatal(err)
	}
	passwordValue := "Integration1!x"
	hash, err := hasher.Hash(ctx, passwordValue)
	if err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	email := "mail-task-http-" + suffix + "@example.com"
	var userID string
	if err := db.QueryRowContext(ctx, `INSERT INTO auth_users (name, email, email_canonical, password_hash) VALUES ($1, $2, $2, $3) RETURNING id::text`, "Mail Task Admin", email, hash).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_user_roles (user_id, role_id) SELECT $1::uuid, id FROM auth_roles WHERE system_key = 'super_admin'`, userID); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("m", 32))
	box, err := application.NewMailTaskSecretBox(key)
	if err != nil {
		t.Fatal(err)
	}
	store.SetMailTaskSecretBox(box)
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_email_settings (smtp_host, smtp_port, smtp_security, smtp_username, from_address, from_name, default_locale, auto_retry_count, retention_days) VALUES ('fixture-smtp', 1025, 'none', '', 'no-reply@example.com', 'Temvia', 'en', 0, 30)`); err != nil {
		t.Fatal(err)
	}
	recipient := "mail-task-recipient-" + suffix + "@example.com"
	created := time.Now().UTC().Add(-time.Minute)
	message := application.OutgoingMail{Kind: application.MailTest, To: recipient, Locale: domain.LocaleEnglish, Subject: "safe test", Text: "private-http-material", HTML: "<p>private-http-material</p>"}
	task, err := store.EnqueueMailTask(ctx, application.MailTaskInput{Kind: application.MailTest, RecipientEmail: recipient, RecipientName: "Operator", Locale: domain.LocaleEnglish, SystemName: "Temvia", CreatedAt: created, ExpiresAt: created.Add(time.Hour), SubmittedBy: userID, Message: &message})
	if err != nil {
		t.Fatal(err)
	}
	workerMailer := &mailTaskHTTPWorkerMailer{fail: true}
	dispatcher := application.NewMailDispatcher(store, workerMailer, application.CryptoRandom(), key, cfg.PublicURL, time.Millisecond, time.Second, time.Millisecond, time.Millisecond)
	dispatcher.SetMailTaskSecretBox(box)
	dispatcher.SetMailRetryPolicyProvider(store)
	if err := dispatcher.ProcessOnce(ctx); err != nil {
		t.Fatalf("HTTP integration worker failure: %v", err)
	}
	failedTask, err := store.FindMailTask(ctx, task.ID)
	if err != nil || failedTask.Status != application.MailTaskStatusFailed || failedTask.AttemptCount != 1 {
		t.Fatalf("HTTP integration failed task = status %s attempts %d error %v", failedTask.Status, failedTask.AttemptCount, err)
	}
	sendingTask := enqueueHTTPMailTask(t, ctx, store, "sending-"+suffix+"@example.com", userID)
	leaseToken := "00000000-0000-4000-8000-000000000401"
	claimed, err := store.ClaimMail(ctx, leaseToken, time.Minute)
	if err != nil || claimed == nil || claimed.ID != sendingTask.ID {
		t.Fatalf("HTTP integration sending claim = %s, %v", httpMailJobSummary(claimed), err)
	}
	queuedTask := enqueueHTTPMailTask(t, ctx, store, "queued-"+suffix+"@example.com", userID)
	auth := application.NewAuthentication(store, hasher, store, store, application.CryptoRandom(), domain.DefaultPermissionCatalog())
	management := application.NewMailTaskManagement(store, store, domain.DefaultPermissionCatalog())
	operations := application.NewOperationLogService(store)
	handler := NewHandlerWithAccessAndOperationLog(&setupFake{status: application.SetupComplete}, auth, cfg, nil, nil, nil, nil, operations, management)
	cookie := abuseHTTPLogin(t, handler, email, passwordValue, "127.0.0.1:4100", nil)

	list := abuseHTTPRequest(handler, http.MethodGet, "/api/mail-tasks?purpose=test_email&recipient="+recipient, "", cookie, "127.0.0.1:4101", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"purpose":"test_email"`) || !strings.Contains(list.Body.String(), recipient) || !strings.Contains(list.Body.String(), `"status":"failed"`) {
		t.Fatalf("mail task list status=%d body=%s", list.Code, list.Body.String())
	}
	for _, forbidden := range []string{"private-http-material", "ciphertext", "material", "password", "smtp"} {
		if strings.Contains(strings.ToLower(list.Body.String()), forbidden) {
			t.Fatalf("mail task list leaked %q: %s", forbidden, list.Body.String())
		}
	}
	detail := abuseHTTPRequest(handler, http.MethodGet, "/api/mail-tasks/"+task.ID, "", cookie, "127.0.0.1:4102", nil)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"recipientEmail":"`+recipient+`"`) || strings.Contains(detail.Body.String(), "private-http-material") {
		t.Fatalf("mail task detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	bulkRetry := abuseHTTPRequest(handler, http.MethodPost, "/api/mail-tasks/bulk-retry", `{"ids":["`+task.ID+`","`+sendingTask.ID+`"]}`, cookie, "127.0.0.1:4103", nil)
	if bulkRetry.Code != http.StatusOK || !strings.Contains(bulkRetry.Body.String(), `"succeeded":1`) || !strings.Contains(bulkRetry.Body.String(), `"skipped":1`) || !strings.Contains(bulkRetry.Body.String(), `"code":"sending"`) {
		t.Fatalf("bulk retry partial status=%d body=%s", bulkRetry.Code, bulkRetry.Body.String())
	}
	bulkDelete := abuseHTTPRequest(handler, http.MethodPost, "/api/mail-tasks/bulk-delete", `{"ids":["`+queuedTask.ID+`","`+sendingTask.ID+`"]}`, cookie, "127.0.0.1:4104", nil)
	if bulkDelete.Code != http.StatusOK || !strings.Contains(bulkDelete.Body.String(), `"succeeded":1`) || !strings.Contains(bulkDelete.Body.String(), `"skipped":1`) || !strings.Contains(bulkDelete.Body.String(), `"code":"sending"`) {
		t.Fatalf("bulk delete partial status=%d body=%s", bulkDelete.Code, bulkDelete.Body.String())
	}
	deleted := abuseHTTPRequest(handler, http.MethodDelete, "/api/mail-tasks/"+task.ID, "", cookie, "127.0.0.1:4105", nil)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("mail task delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	if _, err := store.FindMailTask(ctx, task.ID); err != application.ErrMailTaskNotFound {
		t.Fatalf("FindMailTask after HTTP delete = %v", err)
	}
	var auditCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_operation_logs WHERE action = 'mail_tasks.delete' AND object_id = $1`, task.ID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("mail task delete audit rows = %d, want 1", auditCount)
	}
	if marked, err := store.MarkMailSent(ctx, sendingTask.ID, leaseToken); err != nil || !marked {
		t.Fatalf("finish skipped sending task = %t, %v", marked, err)
	}
	if err := store.DeleteMailTask(ctx, sendingTask.ID); err != nil {
		t.Fatalf("delete finished sending task: %v", err)
	}

	viewerEmail := "mail-task-viewer-" + suffix + "@example.com"
	viewerPassword := "Viewer1!x"
	viewerHash, err := hasher.Hash(ctx, viewerPassword)
	if err != nil {
		t.Fatal(err)
	}
	var viewerID string
	if err := db.QueryRowContext(ctx, `INSERT INTO auth_users (name, email, email_canonical, password_hash) VALUES ('Mail Task Viewer', $1, $1, $2) RETURNING id::text`, viewerEmail, viewerHash).Scan(&viewerID); err != nil {
		t.Fatal(err)
	}
	var viewerRoleID string
	if err := db.QueryRowContext(ctx, `INSERT INTO auth_roles (name, name_canonical, description) VALUES ($1, $2, 'mail-task integration') RETURNING id::text`, "Mail task viewer "+suffix, "mail task viewer "+suffix).Scan(&viewerRoleID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_role_permissions (role_id, permission_key) VALUES ($1::uuid, 'mail-tasks.read')`, viewerRoleID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_user_roles (user_id, role_id) VALUES ($1::uuid, $2::uuid)`, viewerID, viewerRoleID); err != nil {
		t.Fatal(err)
	}
	settingsEmail := "mail-task-settings-" + suffix + "@example.com"
	settingsPassword := "Settings1!x"
	settingsHash, err := hasher.Hash(ctx, settingsPassword)
	if err != nil {
		t.Fatal(err)
	}
	var settingsID string
	if err := db.QueryRowContext(ctx, `INSERT INTO auth_users (name, email, email_canonical, password_hash) VALUES ('Mail Task Settings', $1, $1, $2) RETURNING id::text`, settingsEmail, settingsHash).Scan(&settingsID); err != nil {
		t.Fatal(err)
	}
	var settingsRoleID string
	if err := db.QueryRowContext(ctx, `INSERT INTO auth_roles (name, name_canonical, description) VALUES ($1, $2, 'settings integration') RETURNING id::text`, "Mail task settings "+suffix, "mail task settings "+suffix).Scan(&settingsRoleID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_role_permissions (role_id, permission_key) VALUES ($1::uuid, 'settings.read')`, settingsRoleID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_user_roles (user_id, role_id) VALUES ($1::uuid, $2::uuid)`, settingsID, settingsRoleID); err != nil {
		t.Fatal(err)
	}
	ownerTask := enqueueHTTPMailTask(t, ctx, store, "owner-status-"+suffix+"@example.com", settingsID)
	viewerCookie := abuseHTTPLogin(t, handler, viewerEmail, viewerPassword, "127.0.0.1:4106", nil)
	viewerList := abuseHTTPRequest(handler, http.MethodGet, "/api/mail-tasks", "", viewerCookie, "127.0.0.1:4107", nil)
	if viewerList.Code != http.StatusOK {
		t.Fatalf("read-only task list status=%d body=%s", viewerList.Code, viewerList.Body.String())
	}
	viewerRetry := abuseHTTPRequest(handler, http.MethodPost, "/api/mail-tasks/"+ownerTask.ID+"/retry", "", viewerCookie, "127.0.0.1:4108", nil)
	if viewerRetry.Code != http.StatusForbidden {
		t.Fatalf("read-only task retry status=%d body=%s", viewerRetry.Code, viewerRetry.Body.String())
	}
	settingsCookie := abuseHTTPLogin(t, handler, settingsEmail, settingsPassword, "127.0.0.1:4109", nil)
	settingsList := abuseHTTPRequest(handler, http.MethodGet, "/api/mail-tasks", "", settingsCookie, "127.0.0.1:4110", nil)
	if settingsList.Code != http.StatusForbidden {
		t.Fatalf("settings-only task list status=%d body=%s", settingsList.Code, settingsList.Body.String())
	}
	ownerStatus := abuseHTTPRequest(handler, http.MethodGet, "/api/settings/email/test/"+ownerTask.ID, "", settingsCookie, "127.0.0.1:4111", nil)
	if ownerStatus.Code != http.StatusOK || !strings.Contains(ownerStatus.Body.String(), ownerTask.ID) {
		t.Fatalf("test creator status=%d body=%s", ownerStatus.Code, ownerStatus.Body.String())
	}
	ownerDetail := abuseHTTPRequest(handler, http.MethodGet, "/api/mail-tasks/"+ownerTask.ID, "", settingsCookie, "127.0.0.1:4112", nil)
	if ownerDetail.Code != http.StatusForbidden {
		t.Fatalf("test creator detail without read permission status=%d body=%s", ownerDetail.Code, ownerDetail.Body.String())
	}
}

func enqueueHTTPMailTask(t *testing.T, ctx context.Context, store *postgresadapter.Store, recipient, submittedBy string) application.MailTask {
	t.Helper()
	created := time.Now().UTC().Add(-time.Second)
	message := application.OutgoingMail{Kind: application.MailTest, To: recipient, Locale: domain.LocaleEnglish, Subject: "HTTP integration", Text: "private-http-material", HTML: "<p>private-http-material</p>"}
	task, err := store.EnqueueMailTask(ctx, application.MailTaskInput{Kind: application.MailTest, RecipientEmail: recipient, RecipientName: "Operator", Locale: domain.LocaleEnglish, SystemName: "Temvia", CreatedAt: created, ExpiresAt: created.Add(time.Hour), SubmittedBy: submittedBy, Message: &message})
	if err != nil {
		t.Fatalf("enqueue HTTP mail task: %v", err)
	}
	return task
}

type mailTaskHTTPWorkerMailer struct {
	mu       sync.Mutex
	fail     bool
	messages []application.OutgoingMail
}

func (m *mailTaskHTTPWorkerMailer) Send(_ context.Context, message application.OutgoingMail) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, message)
	if m.fail {
		return &application.MailDeliveryError{Code: "temporary", Temporary: true}
	}
	return nil
}

func httpMailJobSummary(job *application.MailJob) string {
	if job == nil {
		return "<nil>"
	}
	return fmt.Sprintf("id=%s kind=%s recipient=%s attempts=%d round=%d", job.ID, job.Kind, job.Email, job.Attempts, job.Round)
}
