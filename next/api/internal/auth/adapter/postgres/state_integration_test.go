package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	"github.com/jonathanhu237/bind-plane/next/api/internal/config"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestStateIntegrationSessionsPersistTouchWithoutTouchAndCleanup(t *testing.T) {
	db, ctx := openStateIntegrationDB(t)
	defer db.Close()
	if err := resetAuthState(ctx, db); err != nil {
		t.Fatal(err)
	}
	const userID = "019535d9-3df7-79fb-b466-fa907fa17f90"
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_users (id, name, email, email_canonical, password_hash, auth_version) VALUES ($1::uuid, 'State User', 'state@example.com', 'state@example.com', 'hash', 4)`, userID); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		SessionIdleTimeout:          time.Hour,
		SessionAbsoluteTimeout:      2 * time.Hour,
		LoginGlobalCapacity:         10,
		LoginGlobalRefillInterval:   time.Hour,
		LoginEmailCapacity:          10,
		LoginEmailRefillInterval:    time.Hour,
		PasswordResetGlobalCapacity: 10,
		PasswordResetGlobalRefill:   time.Hour,
		PasswordResetEmailCapacity:  10,
		PasswordResetEmailRefill:    time.Hour,
	}
	store := NewStore(db, cfg)
	credential := integrationCredential('s')
	if err := store.CreateVersioned(ctx, credential, userID, 4); err != nil {
		t.Fatalf("CreateVersioned() error = %v", err)
	}
	var storedDigest []byte
	if err := db.QueryRowContext(ctx, `SELECT token_digest FROM auth_sessions WHERE user_id = $1::uuid`, userID).Scan(&storedDigest); err != nil {
		t.Fatal(err)
	}
	expectedDigest := sha256.Sum256(rawCredential(t, credential))
	if string(storedDigest) == credential || len(storedDigest) != sha256.Size {
		t.Fatalf("stored session credential leaked or has wrong length: %x", storedDigest)
	}
	if string(storedDigest) != string(expectedDigest[:]) {
		t.Fatalf("stored digest = %x, want sha256(raw credential) = %x", storedDigest, expectedDigest)
	}
	if got, version, err := store.ResolveVersioned(ctx, credential); err != nil || got != userID || version != 4 {
		t.Fatalf("ResolveVersioned() = %q, %d, %v", got, version, err)
	}
	var before time.Time
	if err := db.QueryRowContext(ctx, `SELECT last_seen_at FROM auth_sessions WHERE token_digest = $1`, storedDigest).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ResolveVersioned(ctx, credential); err != nil {
		t.Fatal(err)
	}
	var afterRead time.Time
	if err := db.QueryRowContext(ctx, `SELECT last_seen_at FROM auth_sessions WHERE token_digest = $1`, storedDigest).Scan(&afterRead); err != nil {
		t.Fatal(err)
	}
	if !afterRead.Equal(before) {
		t.Fatalf("read-only resolution changed last_seen_at from %s to %s", before, afterRead)
	}
	time.Sleep(5 * time.Millisecond)
	if got, version, err := store.ResolveAndTouchVersioned(ctx, credential); err != nil || got != userID || version != 4 {
		t.Fatalf("ResolveAndTouchVersioned() = %q, %d, %v", got, version, err)
	}
	var afterTouch time.Time
	if err := db.QueryRowContext(ctx, `SELECT last_seen_at FROM auth_sessions WHERE token_digest = $1`, storedDigest).Scan(&afterTouch); err != nil {
		t.Fatal(err)
	}
	if !afterTouch.After(afterRead) {
		t.Fatalf("touch last_seen_at = %s, want after %s", afterTouch, afterRead)
	}
	freshStore := NewStore(db, cfg)
	if got, version, err := freshStore.ResolveVersioned(ctx, credential); err != nil || got != userID || version != 4 {
		t.Fatalf("resolution after store reconstruction = %q, %d, %v", got, version, err)
	}
	activities, err := freshStore.ValidSessions(ctx)
	if err != nil || len(activities) != 1 || activities[0].UserID != userID || activities[0].AuthVersion != 4 {
		t.Fatalf("ValidSessions() = %+v, %v", activities, err)
	}
	shortConfig := cfg
	shortConfig.SessionIdleTimeout = time.Hour
	shortConfig.SessionAbsoluteTimeout = 250 * time.Millisecond
	absoluteStore := NewStore(db, shortConfig)
	absoluteCredential := integrationCredential('a')
	if err := absoluteStore.CreateVersioned(ctx, absoluteCredential, userID, 4); err != nil {
		t.Fatal(err)
	}
	if got, _, err := absoluteStore.ResolveVersioned(ctx, absoluteCredential); err != nil || got != userID {
		t.Fatalf("absolute session before deadline = %q, %v", got, err)
	}
	time.Sleep(350 * time.Millisecond)
	if got, _, err := absoluteStore.ResolveVersioned(ctx, absoluteCredential); err != nil || got != "" {
		t.Fatalf("absolute session after deadline = %q, %v", got, err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE auth_sessions SET created_at = clock_timestamp() - interval '3 seconds', last_seen_at = clock_timestamp() - interval '2 seconds', idle_expires_at = clock_timestamp() - interval '1 second' WHERE token_digest = $1`, storedDigest); err != nil {
		t.Fatal(err)
	}
	if got, version, err := freshStore.ResolveVersioned(ctx, credential); err != nil || got != "" || version != 0 {
		t.Fatalf("expired ResolveVersioned() = %q, %d, %v", got, version, err)
	}
	if count, err := freshStore.CleanupExpiredSessions(ctx, 1); err != nil || count != 1 {
		t.Fatalf("CleanupExpiredSessions() = %d, %v", count, err)
	}
	if _, _, err := freshStore.ResolveVersioned(ctx, credential); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_sessions (token_digest, user_id, auth_version, created_at, last_seen_at, idle_expires_at, absolute_expires_at) VALUES ($1, $2::uuid, 4, clock_timestamp(), clock_timestamp(), clock_timestamp() + interval '1 hour', clock_timestamp() + interval '1 hour')`, sessionDigest(integrationCredential('e')), userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE auth_users SET auth_version = 5 WHERE id = $1::uuid`, userID); err != nil {
		t.Fatal(err)
	}
	auth := application.NewAuthentication(store, nil, store, store, application.CryptoRandom(), domain.DefaultPermissionCatalog())
	if _, err := auth.Current(ctx, integrationCredential('e')); !errors.Is(err, application.ErrUnauthenticated) {
		t.Fatalf("stale auth-version session = %v, want unauthenticated", err)
	}

	raceCredential := integrationCredential('r')
	if err := store.CreateVersioned(ctx, raceCredential, userID, 5); err != nil {
		t.Fatal(err)
	}
	raceStart := make(chan struct{})
	raceErrors := make(chan error, 2)
	var raceWait sync.WaitGroup
	raceWait.Add(2)
	go func() {
		defer raceWait.Done()
		<-raceStart
		_, err := store.ResolveAndTouch(ctx, raceCredential)
		raceErrors <- err
	}()
	go func() {
		defer raceWait.Done()
		<-raceStart
		raceErrors <- store.Delete(ctx, raceCredential)
	}()
	close(raceStart)
	raceWait.Wait()
	close(raceErrors)
	for err := range raceErrors {
		if err != nil {
			t.Fatalf("delete/touch race error = %v", err)
		}
	}
	if got, _, err := store.ResolveVersioned(ctx, raceCredential); err != nil || got != "" {
		t.Fatalf("session after delete/touch race = %q, %v", got, err)
	}
}

func TestStateIntegrationTouchUsesDatabaseTimeAfterRowLock(t *testing.T) {
	db, ctx := openStateIntegrationDB(t)
	defer db.Close()
	if err := resetAuthState(ctx, db); err != nil {
		t.Fatal(err)
	}
	const userID = "019535d9-3df7-79fb-b466-fa907fa17f91"
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_users (id, name, email, email_canonical, password_hash) VALUES ($1::uuid, 'Touch User', 'touch@example.com', 'touch@example.com', 'hash')`, userID); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		SessionIdleTimeout:          time.Hour,
		SessionAbsoluteTimeout:      2 * time.Hour,
		LoginGlobalCapacity:         10,
		LoginGlobalRefillInterval:   time.Hour,
		LoginEmailCapacity:          10,
		LoginEmailRefillInterval:    time.Hour,
		PasswordResetGlobalCapacity: 10,
		PasswordResetGlobalRefill:   time.Hour,
		PasswordResetEmailCapacity:  10,
		PasswordResetEmailRefill:    time.Hour,
	}
	for _, test := range []struct {
		name          string
		credential    string
		idleTimeout   time.Duration
		absoluteLimit time.Duration
	}{
		{
			name:          "idle deadline",
			credential:    integrationCredential('i'),
			idleTimeout:   100 * time.Millisecond,
			absoluteLimit: time.Hour,
		},
		{
			name:          "absolute deadline",
			credential:    integrationCredential('j'),
			idleTimeout:   time.Hour,
			absoluteLimit: 100 * time.Millisecond,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			caseConfig := cfg
			caseConfig.SessionIdleTimeout = test.idleTimeout
			caseConfig.SessionAbsoluteTimeout = test.absoluteLimit
			store := NewStore(db, caseConfig)
			if err := store.CreateVersioned(ctx, test.credential, userID, 1); err != nil {
				t.Fatal(err)
			}

			holder, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = holder.Rollback() }()
			var lockedUserID string
			if err := holder.QueryRowContext(ctx, `
				SELECT user_id::text
				FROM auth_sessions
				WHERE token_digest = $1::bytea
				FOR UPDATE`, sessionDigest(test.credential)).Scan(&lockedUserID); err != nil {
				t.Fatal(err)
			}
			if lockedUserID != userID {
				t.Fatalf("locked session user = %q, want %q", lockedUserID, userID)
			}

			touchCtx, touchCancel := context.WithTimeout(ctx, 5*time.Second)
			defer touchCancel()
			touchResult := make(chan struct {
				userID      string
				authVersion int64
				err         error
			}, 1)
			go func() {
				resolvedUserID, authVersion, resolveErr := store.ResolveAndTouchVersioned(touchCtx, test.credential)
				touchResult <- struct {
					userID      string
					authVersion int64
					err         error
				}{resolvedUserID, authVersion, resolveErr}
			}()
			waitCtx, waitCancel := context.WithTimeout(ctx, 2*time.Second)
			waitErr := waitForDatabaseLockWait(waitCtx, db, "auth_sessions")
			waitCancel()
			if waitErr != nil {
				t.Fatalf("touch did not wait for session row lock: %v", waitErr)
			}
			if _, err := holder.ExecContext(ctx, `SELECT pg_sleep(0.25)`); err != nil {
				t.Fatal(err)
			}
			if err := holder.Commit(); err != nil {
				t.Fatal(err)
			}
			result := <-touchResult
			if result.err != nil {
				t.Fatalf("ResolveAndTouchVersioned() error = %v", result.err)
			}
			if result.userID != "" || result.authVersion != 0 {
				t.Fatalf("expired session revived after row-lock wait: user=%q version=%d", result.userID, result.authVersion)
			}
		})
	}
}

func TestStateIntegrationTouchPreservesQueuedActivityTimestamp(t *testing.T) {
	db, ctx := openStateIntegrationDB(t)
	defer db.Close()
	if err := resetAuthState(ctx, db); err != nil {
		t.Fatal(err)
	}
	const userID = "019535d9-3df7-79fb-b466-fa907fa17f92"
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_users (id, name, email, email_canonical, password_hash) VALUES ($1::uuid, 'Monotonic User', 'monotonic@example.com', 'monotonic@example.com', 'hash')`, userID); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		SessionIdleTimeout:          time.Hour,
		SessionAbsoluteTimeout:      2 * time.Hour,
		LoginGlobalCapacity:         10,
		LoginGlobalRefillInterval:   time.Hour,
		LoginEmailCapacity:          10,
		LoginEmailRefillInterval:    time.Hour,
		PasswordResetGlobalCapacity: 10,
		PasswordResetGlobalRefill:   time.Hour,
		PasswordResetEmailCapacity:  10,
		PasswordResetEmailRefill:    time.Hour,
	}
	store := NewStore(db, cfg)
	credential := integrationCredential('m')
	if err := store.CreateVersioned(ctx, credential, userID, 1); err != nil {
		t.Fatal(err)
	}

	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback() }()
	var lockedUserID string
	if err := holder.QueryRowContext(ctx, `
		SELECT user_id::text
		FROM auth_sessions
		WHERE token_digest = $1::bytea
		FOR UPDATE`, sessionDigest(credential)).Scan(&lockedUserID); err != nil {
		t.Fatal(err)
	}
	if lockedUserID != userID {
		t.Fatalf("locked session user = %q, want %q", lockedUserID, userID)
	}

	touchCtx, touchCancel := context.WithTimeout(ctx, 5*time.Second)
	defer touchCancel()
	touchResult := make(chan struct {
		userID      string
		authVersion int64
		err         error
	}, 1)
	go func() {
		resolvedUserID, authVersion, resolveErr := store.ResolveAndTouchVersioned(touchCtx, credential)
		touchResult <- struct {
			userID      string
			authVersion int64
			err         error
		}{resolvedUserID, authVersion, resolveErr}
	}()
	waitCtx, waitCancel := context.WithTimeout(ctx, 2*time.Second)
	waitErr := waitForDatabaseLockWait(waitCtx, db, "auth_sessions")
	waitCancel()
	if waitErr != nil {
		t.Fatalf("touch did not wait for session row lock: %v", waitErr)
	}
	var expectedLastSeen time.Time
	if err := holder.QueryRowContext(ctx, `
		WITH db_clock AS (SELECT clock_timestamp() AS now)
		UPDATE auth_sessions
		SET last_seen_at = db_clock.now + interval '5 seconds',
			idle_expires_at = db_clock.now + interval '6 seconds',
			absolute_expires_at = db_clock.now + interval '1 hour'
		FROM db_clock
		WHERE token_digest = $1::bytea
		RETURNING last_seen_at`, sessionDigest(credential)).Scan(&expectedLastSeen); err != nil {
		t.Fatal(err)
	}
	if err := holder.Commit(); err != nil {
		t.Fatal(err)
	}
	result := <-touchResult
	if result.err != nil || result.userID != userID || result.authVersion != 1 {
		t.Fatalf("ResolveAndTouchVersioned() = %q, %d, %v", result.userID, result.authVersion, result.err)
	}
	var actualLastSeen time.Time
	if err := db.QueryRowContext(ctx, `SELECT last_seen_at FROM auth_sessions WHERE token_digest = $1::bytea`, sessionDigest(credential)).Scan(&actualLastSeen); err != nil {
		t.Fatal(err)
	}
	if actualLastSeen.Before(expectedLastSeen) {
		t.Fatalf("queued touch regressed last_seen_at from %s to %s", expectedLastSeen, actualLastSeen)
	}
}

func TestStateIntegrationLimiterSerializesCreationWithResetAndCleanup(t *testing.T) {
	db, ctx := openStateIntegrationDB(t)
	defer db.Close()
	if err := resetAuthState(ctx, db); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		SessionIdleTimeout:          time.Hour,
		SessionAbsoluteTimeout:      2 * time.Hour,
		LoginGlobalCapacity:         5,
		LoginGlobalRefillInterval:   time.Hour,
		LoginEmailCapacity:          3,
		LoginEmailRefillInterval:    time.Hour,
		PasswordResetGlobalCapacity: 2,
		PasswordResetGlobalRefill:   time.Hour,
		PasswordResetEmailCapacity:  2,
		PasswordResetEmailRefill:    time.Hour,
	}
	store := NewStore(db, cfg)
	const email = "serialized@example.com"
	for _, test := range []struct {
		name       string
		expired    bool
		interleave func(*testing.T)
	}{
		{
			name: "successful login reset",
			interleave: func(t *testing.T) {
				if err := store.ResetEmail(ctx, email); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:    "expired bucket cleanup",
			expired: true,
			interleave: func(t *testing.T) {
				deleted, err := store.CleanupExpiredRateLimitBuckets(ctx, 100)
				if err != nil {
					t.Fatal(err)
				}
				if deleted != 1 {
					t.Fatalf("CleanupExpiredRateLimitBuckets() deleted %d rows, want the unlocked email row", deleted)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := resetAuthState(ctx, db); err != nil {
				t.Fatal(err)
			}
			if allowed, err := store.Allow(ctx, email); err != nil || !allowed {
				t.Fatalf("initial Allow() = %t, %v", allowed, err)
			}
			if test.expired {
				if _, err := db.ExecContext(ctx, `
					UPDATE auth_rate_limit_buckets
					SET tokens = 0,
						last_refill_at = clock_timestamp() - interval '5 hours',
						expires_at = clock_timestamp() - interval '1 second'`); err != nil {
					t.Fatal(err)
				}
			}

			holder, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = holder.Rollback() }()
			var tokens int64
			if err := holder.QueryRowContext(ctx, `
				SELECT tokens
				FROM auth_rate_limit_buckets
				WHERE namespace = 'login' AND bucket_kind = 'global' AND bucket_digest = $1::bytea
				FOR UPDATE`, globalRateLimitDigest).Scan(&tokens); err != nil {
				t.Fatal(err)
			}

			allowCtx, allowCancel := context.WithTimeout(ctx, 5*time.Second)
			defer allowCancel()
			allowResult := make(chan struct {
				allowed bool
				err     error
			}, 1)
			go func() {
				allowed, allowErr := store.Allow(allowCtx, email)
				allowResult <- struct {
					allowed bool
					err     error
				}{allowed, allowErr}
			}()
			waitCtx, waitCancel := context.WithTimeout(ctx, 2*time.Second)
			waitErr := waitForDatabaseLockWait(waitCtx, db, "auth_rate_limit_buckets")
			waitCancel()
			if waitErr != nil {
				t.Fatalf("Allow() did not wait for the locked global bucket: %v", waitErr)
			}
			test.interleave(t)
			if err := holder.Commit(); err != nil {
				t.Fatal(err)
			}
			result := <-allowResult
			if result.err != nil {
				t.Fatalf("Allow() after interleaving error = %v", result.err)
			}
			var emailRows int
			if err := db.QueryRowContext(ctx, `
				SELECT count(*)
				FROM auth_rate_limit_buckets
				WHERE namespace = 'login' AND bucket_kind = 'email' AND bucket_digest = $1::bytea`, rateLimitDigest(email)).Scan(&emailRows); err != nil {
				t.Fatal(err)
			}
			if emailRows != 1 {
				t.Fatalf("email bucket rows after Allow() = %d, want 1", emailRows)
			}
		})
	}
}

func TestStateIntegrationLimiterQuotasAtomicityRefillCleanupAndConcurrency(t *testing.T) {
	db, ctx := openStateIntegrationDB(t)
	defer db.Close()
	if err := resetAuthState(ctx, db); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		SessionIdleTimeout:          time.Hour,
		SessionAbsoluteTimeout:      2 * time.Hour,
		LoginGlobalCapacity:         5,
		LoginGlobalRefillInterval:   time.Hour,
		LoginEmailCapacity:          3,
		LoginEmailRefillInterval:    time.Hour,
		PasswordResetGlobalCapacity: 2,
		PasswordResetGlobalRefill:   time.Hour,
		PasswordResetEmailCapacity:  2,
		PasswordResetEmailRefill:    time.Hour,
	}
	store := NewStore(db, cfg)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	for i := 0; i < 3; i++ {
		allowed, err := store.Allow(ctx, "ada@example.com")
		if err != nil || !allowed {
			t.Fatalf("login email request %d = %t, %v", i+1, allowed, err)
		}
	}
	if allowed, err := store.Allow(ctx, "ada@example.com"); err != nil || allowed {
		t.Fatalf("login email request after email quota = %t, %v", allowed, err)
	}
	for i := 0; i < 2; i++ {
		allowed, err := store.Allow(ctx, "other@example.com")
		if err != nil || !allowed {
			t.Fatalf("shared global request %d = %t, %v", i+1, allowed, err)
		}
	}
	if allowed, err := store.Allow(ctx, "other@example.com"); err != nil || allowed {
		t.Fatalf("login request after global quota = %t, %v", allowed, err)
	}
	if allowed, err := store.AllowPasswordReset(ctx, "ada@example.com"); err != nil || !allowed {
		t.Fatalf("independent password-reset namespace = %t, %v", allowed, err)
	}

	loginDigest := rateLimitDigest("ada@example.com")
	if _, err := db.ExecContext(ctx, `UPDATE auth_rate_limit_buckets SET tokens = 0, last_refill_at = clock_timestamp(), expires_at = clock_timestamp() + interval '1 hour' WHERE namespace = 'login' AND bucket_kind = 'global'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE auth_rate_limit_buckets SET tokens = 2, last_refill_at = clock_timestamp(), expires_at = clock_timestamp() + interval '1 hour' WHERE namespace = 'login' AND bucket_kind = 'email' AND bucket_digest = $1`, loginDigest); err != nil {
		t.Fatal(err)
	}
	if allowed, err := store.Allow(ctx, "ada@example.com"); err != nil || allowed {
		t.Fatalf("atomic dual-bucket denial = %t, %v", allowed, err)
	}
	var emailTokens int64
	if err := db.QueryRowContext(ctx, `SELECT tokens FROM auth_rate_limit_buckets WHERE namespace = 'login' AND bucket_kind = 'email' AND bucket_digest = $1`, loginDigest).Scan(&emailTokens); err != nil {
		t.Fatal(err)
	}
	if emailTokens != 2 {
		t.Fatalf("email bucket after global denial = %d, want 2", emailTokens)
	}
	if err := store.ResetEmail(ctx, "ada@example.com"); err != nil {
		t.Fatal(err)
	}
	var emailRows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_rate_limit_buckets WHERE namespace = 'login' AND bucket_kind = 'email' AND bucket_digest = $1`, loginDigest).Scan(&emailRows); err != nil {
		t.Fatal(err)
	}
	if emailRows != 0 {
		t.Fatalf("ResetEmail left %d rows", emailRows)
	}

	if _, err := db.ExecContext(ctx, `UPDATE auth_rate_limit_buckets SET tokens = 0, last_refill_at = clock_timestamp() - interval '2 hours', expires_at = clock_timestamp() + interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	if allowed, err := store.AllowPasswordReset(ctx, "refill@example.com"); err != nil || !allowed {
		t.Fatalf("refilled password-reset bucket = %t, %v", allowed, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE auth_rate_limit_buckets SET tokens = 0, expires_at = clock_timestamp() + interval '1 hour' WHERE namespace = 'password-reset'`); err != nil {
		t.Fatal(err)
	}
	if deleted, err := store.CleanupExpiredRateLimitBuckets(ctx, 100); err != nil || deleted != 0 {
		t.Fatalf("early limiter cleanup = %d, %v", deleted, err)
	}
	if allowed, err := store.AllowPasswordReset(ctx, "refill@example.com"); err != nil || allowed {
		t.Fatalf("quota restored before expiry = %t, %v", allowed, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE auth_rate_limit_buckets SET last_refill_at = clock_timestamp() - interval '5 hours', expires_at = clock_timestamp() - interval '1 second' WHERE namespace = 'password-reset'`); err != nil {
		t.Fatal(err)
	}
	if deleted, err := store.CleanupExpiredRateLimitBuckets(ctx, 100); err != nil || deleted == 0 {
		t.Fatalf("expired limiter cleanup = %d, %v", deleted, err)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM auth_rate_limit_buckets`); err != nil {
		t.Fatal(err)
	}
	const attempts = 20
	var allowed atomic.Int32
	errorsFromLimiter := make(chan error, attempts)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			ok, err := store.Allow(ctx, fmt.Sprintf("hot-%d@example.com", index))
			if err != nil {
				errorsFromLimiter <- err
				return
			}
			if ok {
				allowed.Add(1)
			}
		}(i)
	}
	close(start)
	wait.Wait()
	close(errorsFromLimiter)
	for err := range errorsFromLimiter {
		t.Fatalf("concurrent limiter error = %v", err)
	}
	if got := allowed.Load(); got != int32(cfg.LoginGlobalCapacity) {
		t.Fatalf("concurrent login allows = %d, want %d", got, cfg.LoginGlobalCapacity)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM auth_rate_limit_buckets`); err != nil {
		t.Fatal(err)
	}
	allowed.Store(0)
	errorsFromLimiter = make(chan error, attempts)
	start = make(chan struct{})
	wait = sync.WaitGroup{}
	for i := 0; i < attempts; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			ok, err := store.Allow(ctx, "first-use-hot@example.com")
			if err != nil {
				errorsFromLimiter <- err
				return
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errorsFromLimiter)
	for err := range errorsFromLimiter {
		t.Fatalf("concurrent first-use limiter error = %v", err)
	}
	if got := allowed.Load(); got != int32(cfg.LoginEmailCapacity) {
		t.Fatalf("concurrent first-use email allows = %d, want %d", got, cfg.LoginEmailCapacity)
	}
}

func TestStateIntegrationAbuseDimensionsAndRecovery(t *testing.T) {
	db, ctx := openStateIntegrationDB(t)
	defer db.Close()
	if err := resetAuthState(ctx, db); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		SessionIdleTimeout:              time.Hour,
		SessionAbsoluteTimeout:          2 * time.Hour,
		LoginGlobalCapacity:             100,
		LoginGlobalRefillInterval:       time.Hour,
		LoginEmailCapacity:              2,
		LoginEmailRefillInterval:        time.Hour,
		LoginIPCapacity:                 2,
		LoginIPRefillInterval:           time.Hour,
		PasswordResetGlobalCapacity:     100,
		PasswordResetGlobalRefill:       time.Hour,
		PasswordResetEmailCapacity:      2,
		PasswordResetEmailRefill:        time.Hour,
		PasswordResetIPCapacity:         2,
		PasswordResetIPRefill:           time.Hour,
		PasswordResetCompleteIPCapacity: 2,
		PasswordResetCompleteIPRefill:   time.Hour,
		InvitationAcceptIPCapacity:      2,
		InvitationAcceptIPRefill:        time.Hour,
		SetupIPCapacity:                 2,
		SetupIPRefill:                   time.Hour,
		InvitationSendActorCapacity:     2,
		InvitationSendActorRefill:       time.Hour,
		InvitationSendRecipientCapacity: 1,
		InvitationSendRecipientRefill:   time.Hour,
		TestEmailGlobalCapacity:         100,
		TestEmailGlobalRefill:           time.Hour,
		TestEmailActorCapacity:          2,
		TestEmailActorRefill:            time.Hour,
		TestEmailRecipientCapacity:      1,
		TestEmailRecipientRefill:        time.Hour,
	}
	store := NewStore(db, cfg)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	if allowed, err := store.AllowLogin(ctx, "::ffff:198.51.100.1", "first@example.com"); err != nil || !allowed {
		t.Fatalf("first mapped IPv4 login = %t, %v", allowed, err)
	}
	if allowed, err := store.AllowLogin(ctx, "198.51.100.1", "first@example.com"); err != nil || !allowed {
		t.Fatalf("equivalent IPv4 login = %t, %v", allowed, err)
	}
	if allowed, err := store.AllowLogin(ctx, "198.51.100.1", "second@example.com"); err != nil || allowed {
		t.Fatalf("same-source login after IP quota = %t, %v", allowed, err)
	}
	var deniedEmailBuckets int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_rate_limit_buckets WHERE namespace = 'login' AND bucket_kind = 'email' AND bucket_digest = $1::bytea`, rateLimitDigest("second@example.com")).Scan(&deniedEmailBuckets); err != nil {
		t.Fatal(err)
	}
	if deniedEmailBuckets != 0 {
		t.Fatalf("source-denied login created %d business buckets", deniedEmailBuckets)
	}
	if allowed, err := store.AllowLogin(ctx, "198.51.100.2", "first@example.com"); err != nil || allowed {
		t.Fatalf("same-email login from another source after email quota = %t, %v", allowed, err)
	}
	if allowed, err := store.AllowLogin(ctx, "2001:db8::1", "second@example.com"); err != nil || !allowed {
		t.Fatalf("independent IPv6 login = %t, %v", allowed, err)
	}
	if allowed, err := store.AllowPasswordResetFromSource(ctx, "198.51.100.1", "first@example.com"); err != nil || !allowed {
		t.Fatalf("independent password-reset namespace = %t, %v", allowed, err)
	}

	if allowed, err := store.AllowInvitationSend(ctx, "actor-1", "recipient-a@example.com"); err != nil || !allowed {
		t.Fatalf("first invitation send = %t, %v", allowed, err)
	}
	if allowed, err := store.AllowInvitationSend(ctx, "actor-1", "recipient-a@example.com"); err != nil || allowed {
		t.Fatalf("repeated invitation recipient = %t, %v", allowed, err)
	}
	if allowed, err := store.AllowInvitationSend(ctx, "actor-1", "recipient-b@example.com"); err != nil || !allowed {
		t.Fatalf("actor quota after recipient denial = %t, %v", allowed, err)
	}
	if allowed, err := store.AllowInvitationSend(ctx, "actor-1", "recipient-c@example.com"); err != nil || allowed {
		t.Fatalf("invitation actor quota = %t, %v", allowed, err)
	}
	if allowed, err := store.AllowTestEmail(ctx, "actor-1", "recipient-a@example.com"); err != nil || !allowed {
		t.Fatalf("first test email = %t, %v", allowed, err)
	}
	if allowed, err := store.AllowTestEmail(ctx, "actor-1", "recipient-a@example.com"); err != nil || allowed {
		t.Fatalf("repeated test recipient = %t, %v", allowed, err)
	}
	if allowed, err := store.AllowTestEmail(ctx, "actor-1", "recipient-b@example.com"); err != nil || !allowed {
		t.Fatalf("test actor after recipient denial = %t, %v", allowed, err)
	}
	if allowed, err := store.AllowTestEmail(ctx, "actor-1", "recipient-c@example.com"); err != nil || allowed {
		t.Fatalf("test actor quota = %t, %v", allowed, err)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM auth_rate_limit_buckets`); err != nil {
		t.Fatal(err)
	}
	concurrentCfg := cfg
	concurrentCfg.LoginIPCapacity = 4
	concurrentStore := NewStore(db, concurrentCfg)
	const attempts = 24
	var allowed atomic.Int32
	errorsFromLimiter := make(chan error, attempts)
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(attempts)
	for index := 0; index < attempts; index++ {
		go func(index int) {
			defer wait.Done()
			<-start
			ok, err := concurrentStore.AllowLogin(ctx, "198.51.100.200", fmt.Sprintf("concurrent-%d@example.com", index))
			if err != nil {
				errorsFromLimiter <- err
			} else if ok {
				allowed.Add(1)
			}
		}(index)
	}
	close(start)
	wait.Wait()
	close(errorsFromLimiter)
	for err := range errorsFromLimiter {
		t.Fatalf("concurrent source limiter error = %v", err)
	}
	if got := allowed.Load(); got != int32(concurrentCfg.LoginIPCapacity) {
		t.Fatalf("concurrent source allows = %d, want %d", got, concurrentCfg.LoginIPCapacity)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM auth_rate_limit_buckets`); err != nil {
		t.Fatal(err)
	}
	recoveryCfg := cfg
	recoveryCfg.LoginIPCapacity = 1
	recoveryCfg.LoginIPRefillInterval = 500 * time.Millisecond
	recoveryStore := NewStore(db, recoveryCfg)
	if allowed, err := recoveryStore.AllowLogin(ctx, "198.51.100.201", "recovery@example.com"); err != nil || !allowed {
		t.Fatalf("recovery initial allow = %t, %v", allowed, err)
	}
	var before time.Time
	if err := db.QueryRowContext(ctx, `SELECT last_refill_at FROM auth_rate_limit_buckets WHERE namespace = 'login' AND bucket_kind = 'ip' AND bucket_digest = $1::bytea`, rateLimitDigest("198.51.100.201")).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if allowed, err := recoveryStore.AllowLogin(ctx, "198.51.100.201", "recovery@example.com"); err != nil || allowed {
			t.Fatalf("repeated recovery denial = %t, %v", allowed, err)
		}
	}
	var after time.Time
	if err := db.QueryRowContext(ctx, `SELECT last_refill_at FROM auth_rate_limit_buckets WHERE namespace = 'login' AND bucket_kind = 'ip' AND bucket_digest = $1::bytea`, rateLimitDigest("198.51.100.201")).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !after.Equal(before) {
		t.Fatalf("repeated denial moved last_refill_at from %s to %s", before, after)
	}
	time.Sleep(650 * time.Millisecond)
	if allowed, err := recoveryStore.AllowLogin(ctx, "198.51.100.201", "recovery@example.com"); err != nil || !allowed {
		t.Fatalf("recovery after refill interval = %t, %v", allowed, err)
	}
}

func TestStateIntegrationLimiterUsesCurrentRetentionAfterConfigurationChange(t *testing.T) {
	db, ctx := openStateIntegrationDB(t)
	defer db.Close()
	if err := resetAuthState(ctx, db); err != nil {
		t.Fatal(err)
	}
	oldConfig := config.Config{
		SessionIdleTimeout:          time.Hour,
		SessionAbsoluteTimeout:      2 * time.Hour,
		LoginGlobalCapacity:         10,
		LoginGlobalRefillInterval:   time.Hour,
		LoginEmailCapacity:          10,
		LoginEmailRefillInterval:    time.Hour,
		LoginIPCapacity:             1,
		LoginIPRefillInterval:       100 * time.Millisecond,
		PasswordResetGlobalCapacity: 10,
		PasswordResetGlobalRefill:   time.Hour,
		PasswordResetEmailCapacity:  10,
		PasswordResetEmailRefill:    time.Hour,
	}
	oldStore := NewStore(db, oldConfig)
	const source = "198.51.100.210"
	if allowed, err := oldStore.AllowLogin(ctx, source, "change@example.com"); err != nil || !allowed {
		t.Fatalf("initial configuration allow = %t, %v", allowed, err)
	}
	time.Sleep(250 * time.Millisecond)
	newConfig := oldConfig
	newConfig.LoginIPRefillInterval = time.Hour
	newStore := NewStore(db, newConfig)
	if deleted, err := newStore.CleanupExpiredRateLimitBuckets(ctx, 100); err != nil || deleted != 0 {
		t.Fatalf("cleanup after lengthening interval = %d, %v", deleted, err)
	}
	if allowed, err := newStore.AllowLogin(ctx, source, "change@example.com"); err != nil || allowed {
		t.Fatalf("state after lengthening interval = %t, %v", allowed, err)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM auth_rate_limit_buckets`); err != nil {
		t.Fatal(err)
	}
	largeConfig := oldConfig
	largeConfig.LoginIPCapacity = 5
	largeConfig.LoginIPRefillInterval = time.Hour
	largeStore := NewStore(db, largeConfig)
	for i := 0; i < 4; i++ {
		if allowed, err := largeStore.AllowLogin(ctx, source, fmt.Sprintf("clip-%d@example.com", i)); err != nil || !allowed {
			t.Fatalf("large-capacity allow %d = %t, %v", i, allowed, err)
		}
	}
	clippedConfig := largeConfig
	clippedConfig.LoginIPCapacity = 1
	clippedStore := NewStore(db, clippedConfig)
	if allowed, err := clippedStore.AllowLogin(ctx, source, "clip-next@example.com"); err != nil || !allowed {
		t.Fatalf("capacity-clipped allow = %t, %v", allowed, err)
	}
	if allowed, err := clippedStore.AllowLogin(ctx, source, "clip-final@example.com"); err != nil || allowed {
		t.Fatalf("capacity-clipped second allow = %t, %v", allowed, err)
	}
}

func openStateIntegrationDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	dsn := integrationDSN(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(32)
	db.SetMaxIdleConns(16)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := newTestStore(db).CheckSchema(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("CheckSchema() error = %v", err)
	}
	return db, ctx
}

func integrationCredential(fill byte) string {
	return base64.RawURLEncoding.EncodeToString(bytesOf(fill, 32))
}

func bytesOf(fill byte, length int) []byte {
	value := make([]byte, length)
	for i := range value {
		value[i] = fill
	}
	return value
}

func rawCredential(t *testing.T, credential string) []byte {
	t.Helper()
	decoded, err := base64.RawURLEncoding.DecodeString(credential)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func waitForDatabaseLockWait(ctx context.Context, db *sql.DB, queryFragment string) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		err := db.QueryRowContext(ctx, `
			SELECT count(*)
			FROM pg_stat_activity
			WHERE pid <> pg_backend_pid()
			  AND wait_event_type = 'Lock'
			  AND query LIKE $1`, "%"+queryFragment+"%").Scan(&waiting)
		if err != nil {
			return err
		}
		if waiting > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
