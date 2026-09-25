package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// TestPersonalSettingsStoreIntegrationEmailAuthorities exercises the durable
// PostgreSQL boundary rather than a fake store. In particular, the email
// change row is the authority for cooldowns, attempts, replacement, and
// consumption; auth_version is deliberately not involved in those checks.
func TestPersonalSettingsStoreIntegrationEmailAuthorities(t *testing.T) {
	db, ctx := openStateIntegrationDB(t)
	t.Cleanup(func() { _ = db.Close() })
	if err := resetAuthState(ctx, db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := resetAuthState(cleanupCtx, db); err != nil {
			t.Errorf("reset personal settings state: %v", err)
		}
	})

	store := newTestStore(db)
	key := bytes.Repeat([]byte{0x42}, domain.PasswordResetVerifierBytes)
	insertPersonalStoreUser(t, ctx, db, "00000000-0000-4000-8000-000000000101", "Cooldown owner", "cooldown-owner@example.com")

	firstSelector, firstDigest := personalEmailMaterial(t, key, 0x11)
	firstEmail, _ := domain.NewEmail("first-destination@example.com")
	first, err := store.RequestEmailChange(ctx, "00000000-0000-4000-8000-000000000101", firstEmail, firstSelector, firstDigest, application.EmailChangeValidity, domain.LocaleEnglish)
	if err != nil {
		t.Fatalf("first email-change request: %v", err)
	}

	secondSelector, secondDigest := personalEmailMaterial(t, key, 0x12)
	secondEmail, _ := domain.NewEmail("second-destination@example.com")
	if _, err := store.RequestEmailChange(ctx, "00000000-0000-4000-8000-000000000101", secondEmail, secondSelector, secondDigest, application.EmailChangeValidity, domain.LocaleEnglish); !errors.Is(err, application.ErrEmailChangeResendTooSoon) {
		t.Fatalf("immediate replacement error = %v, want cooldown", err)
	}
	var storedDestination string
	if err := db.QueryRowContext(ctx, `SELECT new_email FROM auth_email_change_requests WHERE user_id = $1::uuid`, first.UserID).Scan(&storedDestination); err != nil {
		t.Fatal(err)
	}
	if storedDestination != firstEmail.Display {
		t.Fatalf("cooldown replacement changed the pending destination")
	}

	// Cooldown is checked before the attempt count, so an exhausted request
	// cannot be replaced early. Once the database clock passes the fixed
	// window, replacement is allowed and starts a fresh attempt budget.
	if _, err := db.ExecContext(ctx, `UPDATE auth_email_change_requests SET attempts_remaining = 0, resend_after = clock_timestamp() + interval '60 seconds' WHERE user_id = $1::uuid`, first.UserID); err != nil {
		t.Fatal(err)
	}
	thirdSelector, thirdDigest := personalEmailMaterial(t, key, 0x13)
	thirdEmail, _ := domain.NewEmail("third-destination@example.com")
	if _, err := store.RequestEmailChange(ctx, first.UserID, thirdEmail, thirdSelector, thirdDigest, application.EmailChangeValidity, domain.LocaleEnglish); !errors.Is(err, application.ErrEmailChangeResendTooSoon) {
		t.Fatalf("exhausted early replacement error = %v, want cooldown", err)
	}
	// Mark the cooldown elapsed without moving created_at. Keeping the
	// authority timestamp intact satisfies resend_after >= created_at and lets
	// the replacement assertion exercise timestamp preservation.
	if _, err := db.ExecContext(ctx, `UPDATE auth_email_change_requests SET resend_after = created_at WHERE user_id = $1::uuid`, first.UserID); err != nil {
		t.Fatal(err)
	}
	replaced, err := store.RequestEmailChange(ctx, first.UserID, thirdEmail, thirdSelector, thirdDigest, application.EmailChangeValidity, domain.LocaleEnglish)
	if err != nil || replaced.NewEmail != thirdEmail.Display || replaced.AttemptsRemaining != application.EmailChangeMaxAttempts || replaced.Revision != first.Revision+1 {
		t.Fatalf("replacement after cooldown did not start a fresh request: attempts %d revision %d error %v", replaced.AttemptsRemaining, replaced.Revision, err)
	}
	if !replaced.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("replacement changed the authority creation timestamp: got %s, want %s", replaced.CreatedAt, first.CreatedAt)
	}
	var resentTaskCreatedAt time.Time
	if err := db.QueryRowContext(ctx, `SELECT created_at FROM auth_mail_outbox WHERE email_change_request_id = $1::uuid ORDER BY created_at DESC LIMIT 1`, replaced.ID).Scan(&resentTaskCreatedAt); err != nil {
		t.Fatal(err)
	}
	if !resentTaskCreatedAt.After(first.CreatedAt) {
		t.Fatalf("resent task creation timestamp = %s, want after original request %s", resentTaskCreatedAt, first.CreatedAt)
	}
	if _, _, err := store.CompleteEmailChange(ctx, first.UserID, first.ID, first.VerifierDigest, domain.LocaleEnglish, time.Hour); !errors.Is(err, application.ErrInvalidEmailChange) {
		t.Fatalf("superseded code completion error = %v, want invalid request", err)
	}

	// Different destinations still serialize on the owning request row. Only
	// one initial request can commit; the others observe its newly-created
	// resend_after under the row lock.
	const concurrentRequestUser = "00000000-0000-4000-8000-000000000102"
	insertPersonalStoreUser(t, ctx, db, concurrentRequestUser, "Concurrent requester", "concurrent-requester@example.com")
	requestResults := make(chan error, 12)
	start := make(chan struct{})
	var requestWait sync.WaitGroup
	for index := 0; index < 12; index++ {
		requestWait.Add(1)
		go func(index int) {
			defer requestWait.Done()
			<-start
			selector, digest := personalEmailMaterial(t, key, byte(0x30+index))
			email, _ := domain.NewEmail(fmt.Sprintf("concurrent-%d@example.com", index))
			_, err := store.RequestEmailChange(ctx, concurrentRequestUser, email, selector, digest, application.EmailChangeValidity, domain.LocaleEnglish)
			requestResults <- err
		}(index)
	}
	close(start)
	requestWait.Wait()
	close(requestResults)
	requestSuccesses := 0
	requestCooldowns := 0
	for err := range requestResults {
		switch {
		case err == nil:
			requestSuccesses++
		case errors.Is(err, application.ErrEmailChangeResendTooSoon):
			requestCooldowns++
		default:
			t.Fatalf("concurrent email-change request error = %v", err)
		}
	}
	if requestSuccesses != 1 || requestCooldowns != 11 {
		t.Fatalf("concurrent requests = %d successes, %d cooldowns; want 1 and 11", requestSuccesses, requestCooldowns)
	}
	var requestRows, activeCodeJobs int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_email_change_requests WHERE user_id = $1::uuid`, concurrentRequestUser).Scan(&requestRows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_mail_outbox WHERE user_id = $1::uuid AND kind = 'email_change_code' AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL`, concurrentRequestUser).Scan(&activeCodeJobs); err != nil {
		t.Fatal(err)
	}
	if requestRows != 1 || activeCodeJobs != 1 {
		t.Fatalf("concurrent request durable rows = %d request, %d active code jobs; want one each", requestRows, activeCodeJobs)
	}
	// Expire the cooldown using the already-valid authority timestamp instead
	// of backdating created_at and changing the value the fixture later checks.
	if _, err := db.ExecContext(ctx, `UPDATE auth_email_change_requests SET resend_after = created_at WHERE user_id = $1::uuid`, concurrentRequestUser); err != nil {
		t.Fatal(err)
	}
	resendResults := make(chan error, 8)
	start = make(chan struct{})
	var resendWait sync.WaitGroup
	for index := 0; index < 8; index++ {
		resendWait.Add(1)
		go func(index int) {
			defer resendWait.Done()
			<-start
			selector, digest := personalEmailMaterial(t, key, byte(0x40+index))
			_, err := store.ResendEmailChange(ctx, concurrentRequestUser, selector, digest, application.EmailChangeValidity, domain.LocaleEnglish)
			resendResults <- err
		}(index)
	}
	close(start)
	resendWait.Wait()
	close(resendResults)
	resendSuccesses := 0
	resendCooldowns := 0
	for err := range resendResults {
		switch {
		case err == nil:
			resendSuccesses++
		case errors.Is(err, application.ErrEmailChangeResendTooSoon):
			resendCooldowns++
		default:
			t.Fatalf("concurrent email-change resend error = %v", err)
		}
	}
	if resendSuccesses != 1 || resendCooldowns != 7 {
		t.Fatalf("concurrent resend = %d successes, %d cooldowns; want one and seven", resendSuccesses, resendCooldowns)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_mail_outbox WHERE user_id = $1::uuid AND kind = 'email_change_code' AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL`, concurrentRequestUser).Scan(&activeCodeJobs); err != nil {
		t.Fatal(err)
	}
	if activeCodeJobs != 2 {
		t.Fatalf("concurrent resend active code jobs = %d, want two durable code snapshots", activeCodeJobs)
	}

	// Failed code submissions are decremented by the locked request row. Ten
	// callers cannot spend the same attempt concurrently or revive an exhausted
	// request.
	const attemptUser = "00000000-0000-4000-8000-000000000103"
	insertPersonalStoreUser(t, ctx, db, attemptUser, "Attempt owner", "attempt-owner@example.com")
	attemptSelector, attemptDigest := personalEmailMaterial(t, key, 0x51)
	attemptEmail, _ := domain.NewEmail("attempt-destination@example.com")
	attemptRequest, err := store.RequestEmailChange(ctx, attemptUser, attemptEmail, attemptSelector, attemptDigest, application.EmailChangeValidity, domain.LocaleEnglish)
	if err != nil {
		t.Fatal(err)
	}
	wrongDigest := bytes.Repeat([]byte{0xdd}, domain.PasswordResetVerifierBytes)
	attemptResults := make(chan error, 12)
	start = make(chan struct{})
	var attemptWait sync.WaitGroup
	for index := 0; index < 12; index++ {
		attemptWait.Add(1)
		go func() {
			defer attemptWait.Done()
			<-start
			_, _, err := store.CompleteEmailChange(ctx, attemptUser, attemptRequest.ID, wrongDigest, domain.LocaleEnglish, time.Hour)
			attemptResults <- err
		}()
	}
	close(start)
	attemptWait.Wait()
	close(attemptResults)
	invalidAttempts := 0
	exhaustedAttempts := 0
	for err := range attemptResults {
		switch {
		case errors.Is(err, application.ErrInvalidEmailChangeCode):
			invalidAttempts++
		case errors.Is(err, application.ErrEmailChangeAttemptsExceeded):
			exhaustedAttempts++
		default:
			t.Fatalf("concurrent invalid code error = %v", err)
		}
	}
	if invalidAttempts != application.EmailChangeMaxAttempts-1 || exhaustedAttempts != 12-(application.EmailChangeMaxAttempts-1) {
		t.Fatalf("concurrent invalid codes = %d invalid, %d exhausted; want %d and %d", invalidAttempts, exhaustedAttempts, application.EmailChangeMaxAttempts-1, 12-(application.EmailChangeMaxAttempts-1))
	}
	var attemptsRemaining int
	if err := db.QueryRowContext(ctx, `SELECT attempts_remaining FROM auth_email_change_requests WHERE id = $1::uuid`, attemptRequest.ID).Scan(&attemptsRemaining); err != nil {
		t.Fatal(err)
	}
	if attemptsRemaining != 0 {
		t.Fatalf("attempts remaining after concurrent invalid codes = %d, want zero", attemptsRemaining)
	}

	// Correct submissions are single-use even when every caller presents the
	// same valid material.
	const consumeUser = "00000000-0000-4000-8000-000000000104"
	insertPersonalStoreUser(t, ctx, db, consumeUser, "Consume owner", "consume-owner@example.com")
	consumeSelector, consumeDigest := personalEmailMaterial(t, key, 0x61)
	consumeEmail, _ := domain.NewEmail("consume-destination@example.com")
	consumeRequest, err := store.RequestEmailChange(ctx, consumeUser, consumeEmail, consumeSelector, consumeDigest, application.EmailChangeValidity, domain.LocaleEnglish)
	if err != nil {
		t.Fatal(err)
	}
	consumeResults := make(chan error, 8)
	start = make(chan struct{})
	var consumeWait sync.WaitGroup
	for index := 0; index < 8; index++ {
		consumeWait.Add(1)
		go func() {
			defer consumeWait.Done()
			<-start
			_, _, err := store.CompleteEmailChange(ctx, consumeUser, consumeRequest.ID, consumeDigest, domain.LocaleEnglish, time.Hour)
			consumeResults <- err
		}()
	}
	close(start)
	consumeWait.Wait()
	close(consumeResults)
	consumeSuccesses := 0
	consumeInvalid := 0
	for err := range consumeResults {
		switch {
		case err == nil:
			consumeSuccesses++
		case errors.Is(err, application.ErrInvalidEmailChange):
			consumeInvalid++
		default:
			t.Fatalf("concurrent valid code error = %v", err)
		}
	}
	if consumeSuccesses != 1 || consumeInvalid != 7 {
		t.Fatalf("concurrent valid codes = %d successes, %d invalid; want 1 and 7", consumeSuccesses, consumeInvalid)
	}
	var consumedEmail string
	if err := db.QueryRowContext(ctx, `SELECT email FROM auth_users WHERE id = $1::uuid`, consumeUser).Scan(&consumedEmail); err != nil {
		t.Fatal(err)
	}
	if consumedEmail != consumeEmail.Display {
		t.Fatalf("consumed account email was not updated to the destination")
	}
	var consumedRequests int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_email_change_requests WHERE user_id = $1::uuid`, consumeUser).Scan(&consumedRequests); err != nil {
		t.Fatal(err)
	}
	if consumedRequests != 0 {
		t.Fatalf("consumed request rows = %d, want zero", consumedRequests)
	}

	// Two accounts can request the same destination because a pending request
	// is not a reservation. The final locked transaction rechecks the canonical
	// address, so exactly one completion can win.
	const uniqueOwnerA = "00000000-0000-4000-8000-000000000105"
	const uniqueOwnerB = "00000000-0000-4000-8000-000000000106"
	insertPersonalStoreUser(t, ctx, db, uniqueOwnerA, "Unique owner A", "unique-owner-a@example.com")
	insertPersonalStoreUser(t, ctx, db, uniqueOwnerB, "Unique owner B", "unique-owner-b@example.com")
	uniqueEmail, _ := domain.NewEmail("shared-destination@example.com")
	uniqueSelectorA, uniqueDigestA := personalEmailMaterial(t, key, 0x71)
	uniqueSelectorB, uniqueDigestB := personalEmailMaterial(t, key, 0x72)
	requestA, err := store.RequestEmailChange(ctx, uniqueOwnerA, uniqueEmail, uniqueSelectorA, uniqueDigestA, application.EmailChangeValidity, domain.LocaleEnglish)
	if err != nil {
		t.Fatal(err)
	}
	requestB, err := store.RequestEmailChange(ctx, uniqueOwnerB, uniqueEmail, uniqueSelectorB, uniqueDigestB, application.EmailChangeValidity, domain.LocaleEnglish)
	if err != nil {
		t.Fatal(err)
	}
	uniqueResults := make(chan error, 2)
	start = make(chan struct{})
	var uniqueWait sync.WaitGroup
	uniqueWait.Add(2)
	go func() {
		defer uniqueWait.Done()
		<-start
		_, _, err := store.CompleteEmailChange(ctx, uniqueOwnerA, requestA.ID, uniqueDigestA, domain.LocaleEnglish, time.Hour)
		uniqueResults <- err
	}()
	go func() {
		defer uniqueWait.Done()
		<-start
		_, _, err := store.CompleteEmailChange(ctx, uniqueOwnerB, requestB.ID, uniqueDigestB, domain.LocaleEnglish, time.Hour)
		uniqueResults <- err
	}()
	close(start)
	uniqueWait.Wait()
	close(uniqueResults)
	uniqueSuccesses := 0
	uniqueConflicts := 0
	for err := range uniqueResults {
		switch {
		case err == nil:
			uniqueSuccesses++
		case errors.Is(err, application.ErrEmailAlreadyRegistered):
			uniqueConflicts++
		default:
			t.Fatalf("same destination completion error = %v", err)
		}
	}
	if uniqueSuccesses != 1 || uniqueConflicts != 1 {
		t.Fatalf("same destination completions = %d successes, %d conflicts; want one each", uniqueSuccesses, uniqueConflicts)
	}
	var destinationOwners int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_users WHERE email_canonical = $1`, uniqueEmail.Canonical).Scan(&destinationOwners); err != nil {
		t.Fatal(err)
	}
	if destinationOwners != 1 {
		t.Fatalf("same destination active users = %d, want one", destinationOwners)
	}

	// Invitation creation and email-change completion use the same canonical
	// advisory lock. Neither side can commit an active account and a pending
	// invitation for one address.
	const invitationRaceOwner = "00000000-0000-4000-8000-000000000107"
	const invitationCreator = "00000000-0000-4000-8000-000000000108"
	insertPersonalStoreUser(t, ctx, db, invitationRaceOwner, "Invitation race owner", "invitation-race-owner@example.com")
	insertPersonalStoreUser(t, ctx, db, invitationCreator, "Invitation creator", "invitation-creator@example.com")
	var superRoleID string
	if err := db.QueryRowContext(ctx, `SELECT id::text FROM auth_roles WHERE system_key = 'super_admin'`).Scan(&superRoleID); err != nil {
		t.Fatal(err)
	}
	invitationTarget, _ := domain.NewEmail("invitation-race-target@example.com")
	invitationSelector, invitationDigest := personalEmailMaterial(t, key, 0x81)
	invitationRequestSelector, invitationRequestDigest := personalEmailMaterial(t, key, 0x82)
	invitationRequest, err := store.RequestEmailChange(ctx, invitationRaceOwner, invitationTarget, invitationRequestSelector, invitationRequestDigest, application.EmailChangeValidity, domain.LocaleEnglish)
	if err != nil {
		t.Fatal(err)
	}
	invitationResults := make(chan error, 2)
	start = make(chan struct{})
	var invitationWait sync.WaitGroup
	invitationWait.Add(2)
	go func() {
		defer invitationWait.Done()
		<-start
		_, _, err := store.CompleteEmailChange(ctx, invitationRaceOwner, invitationRequest.ID, invitationRequestDigest, domain.LocaleEnglish, time.Hour)
		invitationResults <- err
	}()
	go func() {
		defer invitationWait.Done()
		<-start
		_, err := store.CreateInvitation(ctx, invitationCreator, "Invitation race target", invitationTarget.Display, domain.LocaleEnglish, []string{superRoleID}, invitationSelector, invitationDigest, time.Hour)
		invitationResults <- err
	}()
	close(start)
	invitationWait.Wait()
	close(invitationResults)
	invitationSuccesses := 0
	invitationConflicts := 0
	for err := range invitationResults {
		switch {
		case err == nil:
			invitationSuccesses++
		case errors.Is(err, application.ErrEmailAlreadyRegistered):
			invitationConflicts++
		default:
			t.Fatalf("invitation/email-change race error = %v", err)
		}
	}
	if invitationSuccesses != 1 || invitationConflicts != 1 {
		t.Fatalf("invitation/email-change race = %d successes, %d conflicts; want one each", invitationSuccesses, invitationConflicts)
	}

	// An existing invitation and acceptance also share the canonical lock with
	// a competing invitation creator. Acceptance either wins and creates the
	// user, or the second creator observes the pending invitation and loses.
	acceptTarget, _ := domain.NewEmail("invitation-accept-race@example.com")
	acceptSelector, acceptDigest := personalEmailMaterial(t, key, 0x91)
	if _, err := store.CreateInvitation(ctx, invitationCreator, "Accept race target", acceptTarget.Display, domain.LocaleEnglish, []string{superRoleID}, acceptSelector, acceptDigest, time.Hour); err != nil {
		t.Fatal(err)
	}
	newSelector, newDigest := personalEmailMaterial(t, key, 0x92)
	acceptResults := make(chan error, 2)
	start = make(chan struct{})
	var acceptWait sync.WaitGroup
	acceptWait.Add(2)
	go func() {
		defer acceptWait.Done()
		<-start
		acceptResults <- store.CompleteInvitation(ctx, acceptSelector, acceptDigest, "hash")
	}()
	go func() {
		defer acceptWait.Done()
		<-start
		_, err := store.CreateInvitation(ctx, invitationCreator, "Accept race replacement", acceptTarget.Display, domain.LocaleEnglish, []string{superRoleID}, newSelector, newDigest, time.Hour)
		acceptResults <- err
	}()
	close(start)
	acceptWait.Wait()
	close(acceptResults)
	acceptSuccesses := 0
	acceptConflicts := 0
	for err := range acceptResults {
		switch {
		case err == nil:
			acceptSuccesses++
		case errors.Is(err, application.ErrInvitationPending), errors.Is(err, application.ErrEmailAlreadyRegistered), errors.Is(err, application.ErrInvitationInvalid):
			acceptConflicts++
		default:
			t.Fatalf("invitation acceptance race error = %v", err)
		}
	}
	if acceptSuccesses != 1 || acceptConflicts != 1 {
		t.Fatalf("invitation acceptance race = %d successes, %d conflicts; want one each", acceptSuccesses, acceptConflicts)
	}
	var acceptedUsers int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_users WHERE email_canonical = $1`, acceptTarget.Canonical).Scan(&acceptedUsers); err != nil {
		t.Fatal(err)
	}
	if acceptedUsers != 1 {
		t.Fatalf("invitation acceptance race users = %d, want one", acceptedUsers)
	}
}

func insertPersonalStoreUser(t *testing.T, ctx context.Context, db *sql.DB, id, name, email string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_users (id, name, email, email_canonical, password_hash) VALUES ($1::uuid, $2, $3, lower($3), 'hash')`, id, name, email); err != nil {
		t.Fatal(err)
	}
}

func personalEmailMaterial(t *testing.T, key []byte, fill byte) ([]byte, []byte) {
	t.Helper()
	selector := bytes.Repeat([]byte{fill}, domain.EmailChangeSelectorBytes)
	_, digest, err := domain.NewEmailChangeMaterial(key, selector)
	if err != nil {
		t.Fatal(err)
	}
	return selector, digest
}
