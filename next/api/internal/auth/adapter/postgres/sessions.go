package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
)

const sessionDigestBytes = sha256.Size

func (s *Store) Create(ctx context.Context, sessionID, userID string) error {
	return s.CreateVersioned(ctx, sessionID, userID, 1)
}

func (s *Store) CreateVersioned(ctx context.Context, sessionID, userID string, authVersion int64) error {
	if authVersion <= 0 {
		return fmt.Errorf("invalid authentication version")
	}
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	idleMillis := durationMillis(s.idleTimeout)
	absoluteMillis := durationMillis(s.absoluteTimeout)
	result, err := s.db.ExecContext(operationCtx, `
		WITH db_clock AS (SELECT clock_timestamp() AS now),
        account AS MATERIALIZED (SELECT id FROM auth_users WHERE id=$2::uuid AND disabled_at IS NULL AND auth_version=$3::bigint FOR SHARE)
		INSERT INTO auth_sessions (
			token_digest, user_id, auth_version, created_at, last_seen_at,
			idle_expires_at, absolute_expires_at
		)
		SELECT $1::bytea, $2::uuid, $3::bigint, now, now,
			LEAST(
				now + ($4::bigint * INTERVAL '1 millisecond'),
				now + ($5::bigint * INTERVAL '1 millisecond')
			),
			now + ($5::bigint * INTERVAL '1 millisecond')
		FROM db_clock CROSS JOIN account
		ON CONFLICT (token_digest) DO NOTHING`, sessionDigest(sessionID), userID, authVersion, idleMillis, absoluteMillis)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected != 1 {
		return application.ErrInvalidCredentials
	}
	return nil
}

func (s *Store) ResolveAndTouch(ctx context.Context, sessionID string) (string, error) {
	userID, _, err := s.ResolveAndTouchVersioned(ctx, sessionID)
	return userID, err
}

func (s *Store) ResolveAndTouchVersioned(ctx context.Context, sessionID string) (string, int64, error) {
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(operationCtx, nil)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var userID string
	var authVersion int64
	var lastSeenAt time.Time
	var idleExpiresAt time.Time
	var absoluteExpiresAt time.Time
	// All lifecycle paths lock the account before dependent sessions.
	var disabled bool
	err = tx.QueryRowContext(operationCtx, `SELECT disabled_at IS NOT NULL FROM auth_users WHERE id=(SELECT user_id FROM auth_sessions WHERE token_digest=$1) FOR UPDATE`, sessionDigest(sessionID)).Scan(&disabled)
	if errorsIsNoRows(err) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	var disabledRevoked bool
	err = tx.QueryRowContext(operationCtx, `
		SELECT sessions.user_id::text, sessions.auth_version, sessions.last_seen_at, sessions.idle_expires_at, sessions.absolute_expires_at, sessions.disabled_revoked
		FROM auth_sessions AS sessions
		WHERE sessions.token_digest = $1::bytea
		FOR UPDATE`, sessionDigest(sessionID)).Scan(&userID, &authVersion, &lastSeenAt, &idleExpiresAt, &absoluteExpiresAt, &disabledRevoked)
	if errorsIsNoRows(err) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}

	// The row lock must be acquired before sampling the clock. A queued
	// resolver therefore validates against the current row and current DB time
	// after any logout, cleanup, or earlier touch that was ahead of it.
	var now time.Time
	if err := tx.QueryRowContext(operationCtx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return "", 0, err
	}
	if !now.Before(idleExpiresAt) || !now.Before(absoluteExpiresAt) {
		return "", 0, nil
	}

	if disabled && disabledRevoked {
		return "", 0, application.ErrAccountDisabled
	}
	if disabled || disabledRevoked {
		return "", 0, nil
	}

	// Keep activity monotonic if the wall clock is adjusted backwards while
	// retaining the exact database timestamp when it moves forwards.
	renewalTime := now
	if renewalTime.Before(lastSeenAt) {
		renewalTime = lastSeenAt
	}
	idleExpiry := renewalTime.Add(time.Duration(durationMillis(s.idleTimeout)) * time.Millisecond)
	if idleExpiry.After(absoluteExpiresAt) {
		idleExpiry = absoluteExpiresAt
	}
	result, err := tx.ExecContext(operationCtx, `
		UPDATE auth_sessions
		SET last_seen_at = $2::timestamptz, idle_expires_at = $3::timestamptz
		WHERE token_digest = $1::bytea`, sessionDigest(sessionID), renewalTime, idleExpiry)
	if err != nil {
		return "", 0, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return "", 0, err
	} else if affected != 1 {
		return "", 0, fmt.Errorf("session disappeared during touch")
	}
	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	return userID, authVersion, nil
}

func (s *Store) Resolve(ctx context.Context, sessionID string) (string, error) {
	userID, _, err := s.ResolveVersioned(ctx, sessionID)
	return userID, err
}

func (s *Store) ResolveVersioned(ctx context.Context, sessionID string) (string, int64, error) {
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	var userID string
	var authVersion int64
	var disabled, disabledRevoked bool
	err := s.db.QueryRowContext(operationCtx, `
		WITH db_clock AS (SELECT clock_timestamp() AS now)
		SELECT sessions.user_id::text, sessions.auth_version, users.disabled_at IS NOT NULL, sessions.disabled_revoked
		FROM auth_sessions AS sessions
		JOIN auth_users AS users ON users.id = sessions.user_id
		CROSS JOIN db_clock
		WHERE sessions.token_digest = $1::bytea
		  AND sessions.idle_expires_at > db_clock.now
		  AND sessions.absolute_expires_at > db_clock.now`, sessionDigest(sessionID)).Scan(&userID, &authVersion, &disabled, &disabledRevoked)
	if errorsIsNoRows(err) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	if disabled && disabledRevoked {
		return "", 0, application.ErrAccountDisabled
	}
	if disabled || disabledRevoked {
		return "", 0, nil
	}
	return userID, authVersion, nil
}

func (s *Store) Delete(ctx context.Context, sessionID string) error {
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	_, err := s.db.ExecContext(operationCtx, `DELETE FROM auth_sessions WHERE token_digest = $1::bytea`, sessionDigest(sessionID))
	return err
}

// ValidSessions reads only currently valid rows. It deliberately does not
// update activity, so online-user polling cannot renew a session.
func (s *Store) ValidSessions(ctx context.Context) ([]application.SessionActivity, error) {
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(operationCtx, `
		WITH db_clock AS (SELECT clock_timestamp() AS now)
		SELECT sessions.user_id::text, sessions.auth_version, sessions.last_seen_at
		FROM auth_sessions AS sessions
		JOIN auth_users AS users ON users.id = sessions.user_id
		CROSS JOIN db_clock
		WHERE users.disabled_at IS NULL AND NOT sessions.disabled_revoked AND users.auth_version=sessions.auth_version
		  AND sessions.idle_expires_at > db_clock.now
		  AND sessions.absolute_expires_at > db_clock.now
		  AND sessions.last_seen_at > TIMESTAMPTZ 'epoch'
		ORDER BY sessions.last_seen_at DESC, sessions.user_id::text`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]application.SessionActivity, 0)
	for rows.Next() {
		var item application.SessionActivity
		if err := rows.Scan(&item.UserID, &item.AuthVersion, &item.LastSeenAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// CleanupExpiredSessions removes at most batchSize rows. Row locking prevents
// a cleanup transaction from deleting a session that a concurrent touch has
// already locked and renewed.
func (s *Store) CleanupExpiredSessions(ctx context.Context, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = 100
	}
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(operationCtx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(operationCtx, `
		WITH db_clock AS (SELECT clock_timestamp() AS now)
		SELECT sessions.ctid::text
		FROM auth_sessions AS sessions
		CROSS JOIN db_clock
		WHERE sessions.idle_expires_at <= db_clock.now
		   OR sessions.absolute_expires_at <= db_clock.now
		ORDER BY sessions.idle_expires_at, sessions.absolute_expires_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED`, batchSize)
	if err != nil {
		return 0, err
	}
	ctids := make([]string, 0, batchSize)
	for rows.Next() {
		var ctid string
		if err := rows.Scan(&ctid); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ctids = append(ctids, ctid)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	var deleted int64
	for _, ctid := range ctids {
		result, err := tx.ExecContext(operationCtx, `DELETE FROM auth_sessions WHERE ctid = $1::tid`, ctid)
		if err != nil {
			return 0, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		deleted += count
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}

func sessionDigest(sessionID string) []byte {
	raw, err := base64.RawURLEncoding.DecodeString(sessionID)
	if err != nil {
		raw = []byte(sessionID)
	}
	digest := sha256.Sum256(raw)
	return digest[:]
}

func durationMillis(value time.Duration) int64 {
	if value <= 0 {
		return 0
	}
	millis := int64(value / time.Millisecond)
	if value%time.Millisecond != 0 {
		millis++
	}
	if millis == 0 {
		return 1
	}
	return millis
}

func errorsIsNoRows(err error) bool { return err == sql.ErrNoRows }

func (s *Store) stateContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	timeout := s.operationTimeout
	if timeout <= 0 {
		timeout = stateOperationTimeout
	}
	return context.WithTimeout(parent, timeout)
}
