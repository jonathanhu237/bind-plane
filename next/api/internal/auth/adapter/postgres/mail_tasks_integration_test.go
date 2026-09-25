package postgres

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestStoreIntegrationMailTaskLifecycle(t *testing.T) {
	dsn := integrationDSN(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	store := newTestStore(db)
	if err := store.CheckSchema(ctx); err != nil {
		t.Fatal(err)
	}
	clearMailTaskRows(t, ctx, db)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := clearMailTaskRowsWithContext(cleanupCtx, db); err != nil {
			t.Errorf("clear mail-task lifecycle rows: %v", err)
		}
	})
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	box, err := application.NewMailTaskSecretBox(key)
	if err != nil {
		t.Fatal(err)
	}
	store.SetMailTaskSecretBox(box)
	created := time.Now().UTC().Add(-time.Minute)
	message := application.OutgoingMail{Kind: application.MailTest, To: "operator@example.com", Locale: domain.LocaleEnglish, Subject: "test", Text: "safe", HTML: "<p>safe</p>"}
	task, err := store.EnqueueMailTask(ctx, application.MailTaskInput{Kind: application.MailTest, RecipientEmail: message.To, RecipientName: "Operator", Locale: domain.LocaleEnglish, SystemName: "Temvia", CreatedAt: created, ExpiresAt: created.Add(24 * time.Hour), Message: &message})
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != application.MailTaskStatusQueued || task.AttemptCount != 0 {
		t.Fatalf("enqueued task = %#v", task)
	}
	page, err := store.ListMailTasks(ctx, application.MailTaskListOptions{Limit: 10, Recipient: message.To})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != task.ID {
		t.Fatalf("list = %#v", page.Items)
	}
	detail, err := store.FindMailTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.RecipientEmail != message.To || len(detail.Attempts) != 0 {
		t.Fatalf("detail = %#v", detail)
	}
	if err := store.DeleteMailTask(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindMailTask(ctx, task.ID); err != application.ErrMailTaskNotFound {
		t.Fatalf("FindMailTask after delete = %v", err)
	}
}

type mailTaskDelivery struct {
	Host    string
	Message application.OutgoingMail
}

type mailTaskWorkerAdapter struct {
	mu         sync.Mutex
	fail       bool
	deliveries []mailTaskDelivery
}

func (a *mailTaskWorkerAdapter) Send(ctx context.Context, host string, message application.OutgoingMail) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deliveries = append(a.deliveries, mailTaskDelivery{Host: host, Message: message})
	if a.fail {
		return &application.MailDeliveryError{Code: "temporary", Temporary: true}
	}
	return nil
}

func (a *mailTaskWorkerAdapter) SetFailure(fail bool) {
	a.mu.Lock()
	a.fail = fail
	a.mu.Unlock()
}

func (a *mailTaskWorkerAdapter) Snapshot() []mailTaskDelivery {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]mailTaskDelivery(nil), a.deliveries...)
}

type mailTaskMailer struct {
	adapter *mailTaskWorkerAdapter
	host    string
}

func (m *mailTaskMailer) Send(ctx context.Context, message application.OutgoingMail) error {
	return m.adapter.Send(ctx, m.host, message)
}

type mailTaskMailerFactory struct {
	adapter *mailTaskWorkerAdapter
}

func (f *mailTaskMailerFactory) Make(settings application.SMTPSettings) (application.Mailer, error) {
	return &mailTaskMailer{adapter: f.adapter, host: settings.Host}, nil
}

func clearMailTaskRows(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if err := clearMailTaskRowsWithContext(ctx, db); err != nil {
		t.Fatal(err)
	}
}

func clearMailTaskRowsWithContext(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM auth_mail_task_attempts`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM auth_mail_outbox`); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `DELETE FROM auth_email_settings`)
	return err
}

func saveMailTaskIntegrationSettings(t *testing.T, ctx context.Context, settings *application.SettingsManagement, current application.EmailSettingsView, host string, retryCount, retentionDays int) application.EmailSettingsView {
	t.Helper()
	revision := int64(0)
	if current.Configured {
		revision = current.Revision
	}
	input := application.EmailSettingsInput{
		Host: host, Port: 1025, Security: "none", FromAddress: "no-reply@example.com", FromName: "Temvia",
		DefaultLocale: string(domain.LocaleEnglish), AutoRetryCount: &retryCount, RetentionDays: &retentionDays, Revision: revision,
	}
	saved, err := settings.SaveEmailSettings(ctx, input)
	if err != nil {
		t.Fatalf("save integration mail settings: %v", err)
	}
	return saved
}

func enqueueMailTaskIntegrationTestMail(t *testing.T, ctx context.Context, store *Store, recipient string, createdAt time.Time, submittedBy string) application.MailTask {
	t.Helper()
	message := application.OutgoingMail{Kind: application.MailTest, To: recipient, Locale: domain.LocaleEnglish, Subject: "integration", Text: "private-material-marker", HTML: "<p>private-material-marker</p>"}
	task, err := store.EnqueueMailTask(ctx, application.MailTaskInput{Kind: application.MailTest, RecipientEmail: recipient, RecipientName: "Operator", Locale: domain.LocaleEnglish, SystemName: "Temvia", CreatedAt: createdAt, ExpiresAt: createdAt.Add(24 * time.Hour), SubmittedBy: submittedBy, Message: &message})
	if err != nil {
		t.Fatalf("enqueue integration mail task: %v", err)
	}
	return task
}

func makeMailTaskIntegrationDispatcher(store *Store, settings *application.SettingsManagement, masterKey []byte, tokenKey []byte) *application.MailDispatcher {
	dispatcher := application.NewMailDispatcher(store, nil, application.CryptoRandom(), tokenKey, "https://admin.example", time.Millisecond, time.Second, time.Millisecond, time.Millisecond)
	box, _ := application.NewMailTaskSecretBox(masterKey)
	dispatcher.SetMailTaskSecretBox(box)
	dispatcher.SetMailerProvider(settings)
	dispatcher.SetMailRetryPolicyProvider(store)
	return dispatcher
}

func forceMailTaskAvailable(t *testing.T, ctx context.Context, db *sql.DB, id string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `UPDATE auth_mail_outbox SET available_at = clock_timestamp() - interval '1 second' WHERE id = $1::uuid`, id); err != nil {
		t.Fatal(err)
	}
}

func TestStoreIntegrationMailTaskWorkerPolicyAndMaterial(t *testing.T) {
	db, ctx := openStateIntegrationDB(t)
	t.Cleanup(func() { _ = db.Close() })
	clearMailTaskRows(t, ctx, db)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := clearMailTaskRowsWithContext(cleanupCtx, db); err != nil {
			t.Errorf("clear mail-task policy rows: %v", err)
		}
	})
	store := newTestStore(db)
	masterKey := bytes.Repeat([]byte{0x7d}, 32)
	adapter := &mailTaskWorkerAdapter{}
	settings := application.NewSettingsManagement(store, nil, (&mailTaskMailerFactory{adapter: adapter}).Make, nil)
	if retryCount, err := store.CurrentMailRetryCount(ctx); err != nil || retryCount != application.DefaultAutoRetryCount {
		t.Fatalf("default auto retry count = %d, %v; want %d", retryCount, err, application.DefaultAutoRetryCount)
	}
	if retention, err := store.CurrentMailRetentionDays(ctx); err != nil || retention != application.DefaultMailRetentionDays {
		t.Fatalf("default retention days = %d, %v; want %d", retention, err, application.DefaultMailRetentionDays)
	}
	current := saveMailTaskIntegrationSettings(t, ctx, settings, application.EmailSettingsView{}, "smtp-old", 0, 30)
	workerTokenKey := bytes.Repeat([]byte{0x41}, 32)
	dispatcher := makeMailTaskIntegrationDispatcher(store, settings, masterKey, workerTokenKey)
	adapter.SetFailure(true)
	zeroTask := enqueueMailTaskIntegrationTestMail(t, ctx, store, "zero-retry@example.com", time.Now().UTC().Add(-time.Second), "")
	if err := dispatcher.ProcessOnce(ctx); err != nil {
		t.Fatalf("zero-retry worker pass: %v", err)
	}
	zeroDetail, err := store.FindMailTask(ctx, zeroTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if zeroDetail.Status != application.MailTaskStatusFailed || zeroDetail.AttemptCount != 1 || len(zeroDetail.Attempts) != 1 {
		t.Fatalf("zero-retry task projection = status %s attempts %d history %d", zeroDetail.Status, zeroDetail.AttemptCount, len(zeroDetail.Attempts))
	}

	current = saveMailTaskIntegrationSettings(t, ctx, settings, current, "smtp-old", 2, 30)
	policyTask := enqueueMailTaskIntegrationTestMail(t, ctx, store, "policy@example.com", time.Now().UTC().Add(-time.Second), "")
	for attempt := 1; attempt <= 3; attempt++ {
		if err := dispatcher.ProcessOnce(ctx); err != nil {
			t.Fatalf("policy worker attempt %d: %v", attempt, err)
		}
		if attempt < 3 {
			forceMailTaskAvailable(t, ctx, db, policyTask.ID)
		}
	}
	failed, err := store.FindMailTask(ctx, policyTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != application.MailTaskStatusFailed || failed.AttemptCount != 3 || failed.Round != 1 || failed.RoundAttemptCount != 3 || len(failed.Attempts) != 3 {
		t.Fatalf("retry-budget task projection = status %s total %d round %d/%d history %d", failed.Status, failed.AttemptCount, failed.Round, failed.RoundAttemptCount, len(failed.Attempts))
	}
	current = saveMailTaskIntegrationSettings(t, ctx, settings, current, "smtp-old", 1, 30)
	retried, err := store.RetryMailTask(ctx, policyTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != application.MailTaskStatusQueued || retried.AttemptCount != 3 || retried.Round != 2 || retried.RoundAttemptCount != 0 {
		t.Fatalf("manual retry projection = status %s total %d round %d/%d", retried.Status, retried.AttemptCount, retried.Round, retried.RoundAttemptCount)
	}
	if err := dispatcher.ProcessOnce(ctx); err != nil {
		t.Fatalf("manual round first attempt: %v", err)
	}
	forceMailTaskAvailable(t, ctx, db, policyTask.ID)
	adapter.SetFailure(false)
	current = saveMailTaskIntegrationSettings(t, ctx, settings, current, "smtp-new", 1, 30)
	if err := dispatcher.ProcessOnce(ctx); err != nil {
		t.Fatalf("manual round recovered attempt: %v", err)
	}
	sent, err := store.FindMailTask(ctx, policyTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sent.Status != application.MailTaskStatusSent || sent.AttemptCount != 5 || sent.Round != 2 || sent.RoundAttemptCount != 2 || len(sent.Attempts) != 5 || sent.LastErrorCode != "" {
		t.Fatalf("manual retry final projection = status %s total %d round %d/%d history %d error %q", sent.Status, sent.AttemptCount, sent.Round, sent.RoundAttemptCount, len(sent.Attempts), sent.LastErrorCode)
	}
	deliveries := adapter.Snapshot()
	if len(deliveries) < 5 || deliveries[len(deliveries)-1].Host != "smtp-new" {
		t.Fatalf("worker did not resolve latest saved SMTP host; deliveries=%d", len(deliveries))
	}
	var ciphertext []byte
	if err := db.QueryRowContext(ctx, `SELECT material_ciphertext FROM auth_mail_outbox WHERE id = $1::uuid`, policyTask.ID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if len(ciphertext) == 0 || bytes.Contains(ciphertext, []byte("private-material-marker")) {
		t.Fatal("mail material was empty or persisted in plaintext")
	}

	// A failed reset task keeps its original encrypted credential projection
	// even after the authority is replaced. The worker can resend that exact
	// message, while the authentication boundary continues to reject it.
	userEmail := fmt.Sprintf("stale-reset-%d@example.com", time.Now().UnixNano())
	var userID string
	if err := db.QueryRowContext(ctx, `INSERT INTO auth_users (name, email, email_canonical, password_hash) VALUES ('Stale reset', $1, $1, 'hash') RETURNING id::text`, userEmail).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := db.ExecContext(cleanupCtx, `DELETE FROM auth_users WHERE id = $1::uuid`, userID); err != nil {
			t.Errorf("clear stale-mail fixture user: %v", err)
		}
	})
	// The worker reconstructs credential links with its configured authority
	// key. Use that exact key for both durable snapshots so this fixture tests
	// stale-message retention rather than an artificial key mismatch.
	oldMaterial, err := domain.NewPasswordResetMaterial(workerTokenKey, bytes.Repeat([]byte{0x91}, domain.PasswordResetSelectorBytes))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RequestPasswordReset(ctx, userEmail, oldMaterial.Selector, oldMaterial.VerifierDigest, time.Hour, domain.LocaleEnglish); err != nil {
		t.Fatal(err)
	}
	var oldTaskID string
	if err := db.QueryRowContext(ctx, `SELECT id::text FROM auth_mail_outbox WHERE user_id = $1::uuid AND kind = 'password_reset' AND reset_selector = $2`, userID, oldMaterial.Selector).Scan(&oldTaskID); err != nil {
		t.Fatal(err)
	}
	adapter.SetFailure(true)
	current = saveMailTaskIntegrationSettings(t, ctx, settings, current, "smtp-new", 0, 30)
	if err := dispatcher.ProcessOnce(ctx); err != nil {
		t.Fatalf("stale credential failed delivery: %v", err)
	}
	var oldFirst application.OutgoingMail
	for _, delivery := range adapter.Snapshot() {
		if delivery.Message.Kind == application.MailPasswordReset && delivery.Message.To == userEmail {
			oldFirst = delivery.Message
			break
		}
	}
	if oldFirst.MessageID == "" {
		t.Fatal("stale credential was not composed by the worker")
	}
	newMaterial, err := domain.NewPasswordResetMaterial(workerTokenKey, bytes.Repeat([]byte{0x92}, domain.PasswordResetSelectorBytes))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RequestPasswordReset(ctx, userEmail, newMaterial.Selector, newMaterial.VerifierDigest, time.Hour, domain.LocaleChinese); err != nil {
		t.Fatal(err)
	}
	if err := store.PreflightPasswordReset(ctx, oldMaterial.Selector, oldMaterial.VerifierDigest); !errors.Is(err, application.ErrInvalidPasswordResetToken) {
		t.Fatalf("replaced reset credential error = %v, want invalid", err)
	}
	if err := store.PreflightPasswordReset(ctx, newMaterial.Selector, newMaterial.VerifierDigest); err != nil {
		t.Fatalf("current reset credential error = %v", err)
	}
	if _, err := store.RetryMailTask(ctx, oldTaskID); err != nil {
		t.Fatal(err)
	}
	adapter.SetFailure(false)
	if err := dispatcher.ProcessOnce(ctx); err != nil {
		t.Fatalf("stale credential resend: %v", err)
	}
	var oldSecond application.OutgoingMail
	for _, delivery := range adapter.Snapshot() {
		if delivery.Message.Kind == application.MailPasswordReset && delivery.Message.To == userEmail && delivery.Message.MessageID == oldFirst.MessageID {
			oldSecond = delivery.Message
		}
	}
	if oldSecond.Subject != oldFirst.Subject || oldSecond.Text != oldFirst.Text || oldSecond.HTML != oldFirst.HTML || oldSecond.To != oldFirst.To {
		t.Fatal("stale credential resend changed the original message")
	}
	oldDetail, err := store.FindMailTask(ctx, oldTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if oldDetail.Status != application.MailTaskStatusSent || oldDetail.AttemptCount != 2 {
		t.Fatalf("stale credential task after resend = status %s attempts %d", oldDetail.Status, oldDetail.AttemptCount)
	}
}

func TestStoreIntegrationMailTaskConcurrencyAndRetention(t *testing.T) {
	db, ctx := openStateIntegrationDB(t)
	t.Cleanup(func() { _ = db.Close() })
	clearMailTaskRows(t, ctx, db)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := clearMailTaskRowsWithContext(cleanupCtx, db); err != nil {
			t.Errorf("clear mail-task concurrency rows: %v", err)
		}
	})
	store := newTestStore(db)
	created := time.Now().UTC().Add(-time.Second)
	active := enqueueMailTaskIntegrationTestMail(t, ctx, store, "sending-race@example.com", created, "")
	leaseOne := "019535d9-3df7-79fb-b466-fa907fa17f81"
	claimed, err := store.ClaimMail(ctx, leaseOne, time.Minute)
	if err != nil || claimed == nil || claimed.ID != active.ID {
		t.Fatalf("claim sending task = %s, %v", mailJobSummary(claimed), err)
	}
	deleteResult := make(chan error, 1)
	markResult := make(chan struct {
		marked bool
		err    error
	}, 1)
	go func() {
		deleteResult <- store.DeleteMailTask(ctx, active.ID)
	}()
	go func() {
		marked, markErr := store.MarkMailSent(ctx, active.ID, leaseOne)
		markResult <- struct {
			marked bool
			err    error
		}{marked: marked, err: markErr}
	}()
	deleteErr := <-deleteResult
	mark := <-markResult
	if mark.err != nil {
		t.Fatal(mark.err)
	}
	if deleteErr == nil {
		// The valid serialization is SMTP acknowledgement first, then
		// deletion of the now-terminal task. Deleting the leased task before
		// acknowledgement must instead return ErrMailTaskSending.
		if !mark.marked {
			t.Fatal("leased task deleted before SMTP acknowledgement")
		}
		if _, err := store.FindMailTask(ctx, active.ID); !errors.Is(err, application.ErrMailTaskNotFound) {
			t.Fatalf("deleted sending task lookup = %v", err)
		}
	} else {
		if !errors.Is(deleteErr, application.ErrMailTaskSending) || !mark.marked {
			t.Fatalf("sending/delete race = delete %v marked %t", deleteErr, mark.marked)
		}
		if err := store.DeleteMailTask(ctx, active.ID); err != nil {
			t.Fatalf("delete after SMTP completion: %v", err)
		}
	}

	fenced := enqueueMailTaskIntegrationTestMail(t, ctx, store, "lease-fence@example.com", created, "")
	first, err := store.ClaimMail(ctx, "019535d9-3df7-79fb-b466-fa907fa17f82", time.Minute)
	if err != nil || first == nil {
		t.Fatalf("first fenced claim = %s, %v", mailJobSummary(first), err)
	}
	if _, err := db.ExecContext(ctx, `
		WITH db_clock AS (SELECT clock_timestamp() AS now)
		UPDATE auth_mail_outbox AS o
		SET available_at = db_clock.now - interval '2 seconds',
			lease_expires_at = db_clock.now - interval '1 second'
		FROM db_clock
		WHERE o.id = $1::uuid`, fenced.ID); err != nil {
		t.Fatal(err)
	}
	second, err := store.ClaimMail(ctx, "019535d9-3df7-79fb-b466-fa907fa17f83", time.Minute)
	if err != nil || second == nil || second.Attempts != 2 {
		t.Fatalf("reclaimed fenced claim = %s, %v", mailJobSummary(second), err)
	}
	if marked, err := store.MarkMailSent(ctx, fenced.ID, first.LeaseToken); err != nil || marked {
		t.Fatalf("stale lease completion = %t, %v", marked, err)
	}
	if err := store.RecordMailAttempt(ctx, fenced.ID, first.Round, first.RoundAttempts, application.MailTaskOutcomeSent, ""); err != nil {
		t.Fatal(err)
	}
	if marked, err := store.MarkMailSent(ctx, fenced.ID, second.LeaseToken); err != nil || !marked {
		t.Fatalf("current lease completion = %t, %v", marked, err)
	}
	fencedDetail, err := store.FindMailTask(ctx, fenced.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fencedDetail.Status != application.MailTaskStatusSent || fencedDetail.AttemptCount != 2 || len(fencedDetail.Attempts) != 2 {
		t.Fatalf("fenced task projection = status %s attempts %d history %d", fencedDetail.Status, fencedDetail.AttemptCount, len(fencedDetail.Attempts))
	}

	adapter := &mailTaskWorkerAdapter{}
	settings := application.NewSettingsManagement(store, nil, (&mailTaskMailerFactory{adapter: adapter}).Make, nil)
	_ = saveMailTaskIntegrationSettings(t, ctx, settings, application.EmailSettingsView{}, "smtp-retention", 0, 1)
	retained := enqueueMailTaskIntegrationTestMail(t, ctx, store, "retention@example.com", created, "")
	claim, err := store.ClaimMail(ctx, "019535d9-3df7-79fb-b466-fa907fa17f84", time.Minute)
	if err != nil || claim == nil {
		t.Fatalf("retention claim = %s, %v", mailJobSummary(claim), err)
	}
	if marked, err := store.MarkMailSent(ctx, retained.ID, claim.LeaseToken); err != nil || !marked {
		t.Fatalf("retention terminal send = %t, %v", marked, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE auth_mail_outbox SET sent_at = clock_timestamp() - interval '2 days', finished_at = clock_timestamp() - interval '2 days' WHERE id = $1::uuid`, retained.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.CleanupMail(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindMailTask(ctx, retained.ID); !errors.Is(err, application.ErrMailTaskNotFound) {
		t.Fatalf("old terminal task after cleanup = %v", err)
	}

	resettable := enqueueMailTaskIntegrationTestMail(t, ctx, store, "retention-reset@example.com", created, "")
	claim, err = store.ClaimMail(ctx, "019535d9-3df7-79fb-b466-fa907fa17f85", time.Minute)
	if err != nil || claim == nil {
		t.Fatalf("resettable claim = %s, %v", mailJobSummary(claim), err)
	}
	if dead, err := store.DeadLetterMail(ctx, resettable.ID, claim.LeaseToken, "permanent"); err != nil || !dead {
		t.Fatalf("resettable dead letter = %t, %v", dead, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE auth_mail_outbox SET finished_at = clock_timestamp() - interval '2 days', dead_at = clock_timestamp() - interval '2 days' WHERE id = $1::uuid`, resettable.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RetryMailTask(ctx, resettable.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.CleanupMail(ctx); err != nil {
		t.Fatal(err)
	}
	activeReset, err := store.FindMailTask(ctx, resettable.ID)
	if err != nil || activeReset.Status != application.MailTaskStatusQueued || activeReset.Round != 2 {
		t.Fatalf("manual retry retention reset = status %s round %d error %v", activeReset.Status, activeReset.Round, err)
	}
	claim, err = store.ClaimMail(ctx, "019535d9-3df7-79fb-b466-fa907fa17f86", time.Minute)
	if err != nil || claim == nil {
		t.Fatalf("resettable second claim = %s, %v", mailJobSummary(claim), err)
	}
	if marked, err := store.MarkMailSent(ctx, resettable.ID, claim.LeaseToken); err != nil || !marked {
		t.Fatalf("resettable second send = %t, %v", marked, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE auth_mail_outbox SET sent_at = clock_timestamp() - interval '12 hours', finished_at = clock_timestamp() - interval '12 hours' WHERE id = $1::uuid`, resettable.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.CleanupMail(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindMailTask(ctx, resettable.ID); err != nil {
		t.Fatalf("recent terminal task was cleaned too early: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE auth_mail_outbox SET sent_at = clock_timestamp() - interval '2 days', finished_at = clock_timestamp() - interval '2 days' WHERE id = $1::uuid`, resettable.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.CleanupMail(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindMailTask(ctx, resettable.ID); !errors.Is(err, application.ErrMailTaskNotFound) {
		t.Fatalf("resettable terminal task after retention = %v", err)
	}
}
