package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

const outboxMaintenanceBatchSize = 100

func (s *Store) ClaimMail(ctx context.Context, leaseToken string, leaseDuration time.Duration) (*application.MailJob, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var job application.MailJob
	var kind, locale string
	var userID, invitationID, emailChangeRequestID sql.NullString
	var recipientName, recipientEmail, systemName string
	var selector, emailChangeSelector, digest, encryptedMaterial []byte
	var attempts, round, roundAttempts int
	err = tx.QueryRowContext(ctx, `
		SELECT o.id::text, o.kind, o.user_id::text, o.invitation_id::text,
		       COALESCE(o.recipient_name, u.name, i.name, ''),
		       COALESCE(o.recipient_email, u.email, i.email, ''),
		       COALESCE(o.system_name, 'Temvia'), o.locale,
		       CASE WHEN o.material_ciphertext IS NULL THEN o.reset_selector END,
		       CASE WHEN o.material_ciphertext IS NULL THEN o.email_change_selector END,
		       CASE WHEN o.material_ciphertext IS NULL THEN COALESCE(r.verifier_digest, e.verifier_digest, i.verifier_digest) END,
		       o.email_change_request_id::text, o.material_ciphertext,
		       o.attempt_count + 1, o.round_number, o.round_attempt_count + 1,
		       o.created_at, o.expires_at
		FROM auth_mail_outbox AS o
		LEFT JOIN auth_users AS u ON u.id = o.user_id
		LEFT JOIN auth_user_invitations AS i ON i.id = o.invitation_id
		LEFT JOIN auth_password_resets AS r
		  ON r.user_id = o.user_id AND r.selector = o.reset_selector
		LEFT JOIN auth_email_change_requests AS e
		  ON e.id = o.email_change_request_id AND e.selector = o.email_change_selector
		WHERE o.sent_at IS NULL AND o.canceled_at IS NULL AND o.dead_at IS NULL
		  AND o.available_at <= clock_timestamp()
		  AND (o.lease_expires_at IS NULL OR o.lease_expires_at <= clock_timestamp())
		  AND (COALESCE(octet_length(o.material_ciphertext), 0) > 0 OR o.expires_at > clock_timestamp())
		  AND (
			COALESCE(octet_length(o.material_ciphertext), 0) > 0
			OR o.kind = 'password_changed'
			OR (o.kind = 'password_reset' AND r.user_id IS NOT NULL AND r.expires_at > clock_timestamp())
			OR (o.kind = 'user_invitation' AND i.id IS NOT NULL AND i.expires_at > clock_timestamp())
			OR (o.kind = 'email_change_code' AND e.id IS NOT NULL AND e.expires_at > clock_timestamp())
			OR o.kind = 'email_changed'
		  )
		ORDER BY o.created_at, o.id
		FOR UPDATE OF o SKIP LOCKED
		LIMIT 1`,
	).Scan(&job.ID, &kind, &userID, &invitationID, &recipientName, &recipientEmail, &systemName, &locale,
		&selector, &emailChangeSelector, &digest, &emailChangeRequestID, &encryptedMaterial,
		&attempts, &round, &roundAttempts, &job.CreatedAt, &job.ExpiresAt)
	if err != nil {
		if err == sql.ErrNoRows {
			if err := tx.Commit(); err != nil {
				return nil, err
			}
			return nil, nil
		}
		return nil, err
	}
	// A worker may have crashed or lost its context after claiming the
	// previous attempt. The lease boundary guarantees that at most the latest
	// attempt in this round can be missing from history; close that attempt
	// before advancing the counter so recovery never leaves counters ahead of
	// the durable attempt projection. A late result can upsert the same
	// immutable identity with its more precise outcome.
	previousRoundAttempts := roundAttempts - 1
	if previousRoundAttempts > 0 {
		var recordedAttempt int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt_number), 0) FROM auth_mail_task_attempts WHERE task_id = $1::uuid AND round_number = $2`, job.ID, round).Scan(&recordedAttempt); err != nil {
			return nil, err
		}
		if recordedAttempt < previousRoundAttempts {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO auth_mail_task_attempts (task_id, round_number, attempt_number, outcome, error_code)
				SELECT $1::uuid, $2, attempt_number, 'failed', 'dependency'
				FROM generate_series($3::integer, $4::integer) AS attempt_number
				ON CONFLICT (task_id, round_number, attempt_number) DO NOTHING`, job.ID, round, recordedAttempt+1, previousRoundAttempts); err != nil {
				return nil, err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE auth_mail_outbox
		SET lease_token = $2::uuid,
		    lease_expires_at = clock_timestamp() + ($3 * INTERVAL '1 second'),
		    attempt_count = attempt_count + 1,
		    round_attempt_count = round_attempt_count + 1
		WHERE id = $1::uuid`, job.ID, leaseToken, leaseDuration.Seconds()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	job.Kind = application.MailKind(kind)
	if userID.Valid {
		job.UserID = userID.String
	}
	if invitationID.Valid {
		job.InvitationID = invitationID.String
	}
	if emailChangeRequestID.Valid {
		job.EmailChangeRequestID = emailChangeRequestID.String
	}
	job.Name = recipientName
	job.Email = recipientEmail
	job.SystemName = systemName
	job.Locale = domain.Locale(locale)
	job.EncryptedMaterial = append([]byte(nil), encryptedMaterial...)
	// Materialized messages are rendered from the authenticated worker-only
	// projection. Do not copy selectors or verifier digests into the claimed
	// job when the encrypted snapshot is available; legacy material-less rows
	// still receive the fields needed by the compatibility composer.
	if len(encryptedMaterial) == 0 {
		if job.Kind == application.MailEmailChangeCode {
			job.EmailChangeSelector = append([]byte(nil), emailChangeSelector...)
		} else {
			job.ResetSelector = append([]byte(nil), selector...)
		}
		job.VerifierDigest = append([]byte(nil), digest...)
		job.InvitationSelector = append([]byte(nil), selector...)
		job.InvitationVerifierDigest = append([]byte(nil), digest...)
	}
	job.Attempts = attempts
	job.Round = round
	job.RoundAttempts = roundAttempts
	job.LeaseToken = leaseToken
	return &job, nil
}

func (s *Store) MarkMailSent(ctx context.Context, id, leaseToken string) (bool, error) {
	return s.finishMailAttempt(ctx, id, leaseToken, application.MailTaskOutcomeSent, "", true)
}

func (s *Store) RetryMail(ctx context.Context, id, leaseToken string, delay time.Duration, errorCode string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var round, attempt int
	err = tx.QueryRowContext(ctx, `
		UPDATE auth_mail_outbox
		SET available_at = clock_timestamp() + ($3 * INTERVAL '1 second'),
		    lease_token = NULL, lease_expires_at = NULL, last_error_code = $4
		WHERE id = $1::uuid AND lease_token = $2::uuid
		  AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL
		RETURNING round_number, round_attempt_count`, id, leaseToken, delay.Seconds(), errorCode).Scan(&round, &attempt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := upsertMailAttemptTx(ctx, tx, id, round, attempt, application.MailTaskOutcomeFailed, errorCode); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) DeadLetterMail(ctx context.Context, id, leaseToken, errorCode string) (bool, error) {
	return s.finishMailAttemptWithCode(ctx, id, leaseToken, application.MailTaskOutcomeFailed, errorCode, "dead_at")
}

func (s *Store) DiscardMail(ctx context.Context, id, leaseToken, errorCode string) (bool, error) {
	return s.finishMailAttemptWithCode(ctx, id, leaseToken, application.MailTaskOutcomeFailed, errorCode, "canceled_at")
}

func (s *Store) finishMailAttempt(ctx context.Context, id, leaseToken, outcome, errorCode string, terminal bool) (bool, error) {
	if !terminal {
		return false, nil
	}
	return s.finishMailAttemptWithCode(ctx, id, leaseToken, outcome, errorCode, "sent_at")
}

func (s *Store) finishMailAttemptWithCode(ctx context.Context, id, leaseToken, outcome, errorCode, terminalColumn string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var round, attempt int
	setTerminal := ""
	if terminalColumn == "sent_at" && outcome == application.MailTaskOutcomeSent {
		setTerminal = ", sent_at = clock_timestamp(), finished_at = clock_timestamp(), last_error_code = NULL"
	} else if terminalColumn != "" {
		if terminalColumn != "dead_at" && terminalColumn != "canceled_at" {
			return false, application.ErrDependencyUnavailable
		}
		setTerminal = ", " + terminalColumn + " = clock_timestamp(), finished_at = clock_timestamp(), last_error_code = $3"
	}
	query := `UPDATE auth_mail_outbox SET lease_token = NULL, lease_expires_at = NULL` + setTerminal + ` WHERE id = $1::uuid AND lease_token = $2::uuid AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL RETURNING round_number, round_attempt_count`
	args := []any{id, leaseToken}
	if terminalColumn != "sent_at" {
		args = append(args, errorCode)
	}
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&round, &attempt); err == sql.ErrNoRows {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := upsertMailAttemptTx(ctx, tx, id, round, attempt, outcome, errorCode); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func upsertMailAttemptTx(ctx context.Context, tx *sql.Tx, taskID string, round, attempt int, outcome, errorCode string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO auth_mail_task_attempts (task_id, round_number, attempt_number, outcome, error_code)
		VALUES ($1::uuid, $2, $3, $4, NULLIF($5, ''))
		ON CONFLICT (task_id, round_number, attempt_number) DO UPDATE
		SET outcome = EXCLUDED.outcome, error_code = EXCLUDED.error_code, occurred_at = EXCLUDED.occurred_at`, taskID, round, attempt, outcome, errorCode)
	return err
}

// RecordMailAttempt persists a late result without changing task state. It is
// intentionally independent of the lease token: once a worker loses its
// lease, a newer worker may own the task, but the old attempt's immutable
// history is still safe to complete. A deleted task fails the foreign-key
// insert and cannot be resurrected.
func (s *Store) RecordMailAttempt(ctx context.Context, taskID string, round, attempt int, outcome, errorCode string) error {
	if s == nil || s.db == nil {
		return application.ErrDependencyUnavailable
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_mail_task_attempts (task_id, round_number, attempt_number, outcome, error_code)
		VALUES ($1::uuid, $2, $3, $4, NULLIF($5, ''))
		ON CONFLICT (task_id, round_number, attempt_number) DO UPDATE
		SET outcome = EXCLUDED.outcome, error_code = EXCLUDED.error_code, occurred_at = EXCLUDED.occurred_at`, taskID, round, attempt, outcome, errorCode)
	return err
}

func (s *Store) SweepMail(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Sweep can terminally discard an expired or superseded task before the
	// dispatcher gets another ClaimMail opportunity. Close any missing latest
	// lease attempt first; the insert is idempotent and is also safe when a
	// late worker result races this transaction.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO auth_mail_task_attempts (task_id, round_number, attempt_number, outcome, error_code)
		SELECT o.id, o.round_number, o.round_attempt_count, 'failed',
		       CASE
		         WHEN COALESCE(octet_length(o.material_ciphertext), 0) = 0 AND o.expires_at <= clock_timestamp() THEN 'expired'
		         WHEN COALESCE(octet_length(o.material_ciphertext), 0) = 0 AND o.kind = 'password_reset' AND NOT EXISTS (
				SELECT 1 FROM auth_password_resets AS r
				WHERE r.user_id = o.user_id AND r.selector = o.reset_selector
				  AND r.expires_at > clock_timestamp()
			 ) THEN 'superseded'
		         ELSE 'dependency'
		       END
		FROM auth_mail_outbox AS o
		WHERE o.round_attempt_count > 0
		  AND o.sent_at IS NULL AND o.canceled_at IS NULL AND o.dead_at IS NULL
		  AND (o.lease_token IS NULL OR o.lease_expires_at <= clock_timestamp())
		  AND NOT EXISTS (
			SELECT 1 FROM auth_mail_task_attempts AS a
			WHERE a.task_id = o.id AND a.round_number = o.round_number AND a.attempt_number = o.round_attempt_count
		  )
		ON CONFLICT (task_id, round_number, attempt_number) DO NOTHING`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		WITH expired AS (
			SELECT o.id
			FROM auth_mail_outbox AS o
			WHERE o.sent_at IS NULL AND o.canceled_at IS NULL AND o.dead_at IS NULL
			  AND (o.lease_token IS NULL OR o.lease_expires_at <= clock_timestamp())
			  AND COALESCE(octet_length(o.material_ciphertext), 0) = 0
			  AND o.expires_at <= clock_timestamp()
			ORDER BY o.expires_at, o.id
			FOR UPDATE OF o SKIP LOCKED
			LIMIT $1
		)
		UPDATE auth_mail_outbox AS o
		SET canceled_at = clock_timestamp(), finished_at = clock_timestamp(), lease_token = NULL, lease_expires_at = NULL, last_error_code = 'expired'
		WHERE o.id IN (SELECT id FROM expired)`, outboxMaintenanceBatchSize); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		WITH superseded AS (
			SELECT o.id
			FROM auth_mail_outbox AS o
			WHERE o.kind = 'password_reset'
			  AND COALESCE(octet_length(o.material_ciphertext), 0) = 0
			  AND o.sent_at IS NULL AND o.canceled_at IS NULL AND o.dead_at IS NULL
			  AND (o.lease_token IS NULL OR o.lease_expires_at <= clock_timestamp())
			  AND NOT EXISTS (
				SELECT 1 FROM auth_password_resets AS r
				WHERE r.user_id = o.user_id AND r.selector = o.reset_selector
				  AND r.expires_at > clock_timestamp()
			  )
			ORDER BY o.created_at, o.id
			FOR UPDATE OF o SKIP LOCKED
			LIMIT $1
		)
		UPDATE auth_mail_outbox AS o
		SET canceled_at = clock_timestamp(), finished_at = clock_timestamp(), lease_token = NULL, lease_expires_at = NULL, last_error_code = 'superseded'
		WHERE o.id IN (SELECT id FROM superseded)`, outboxMaintenanceBatchSize); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		WITH expired AS (
			SELECT r.user_id
			FROM auth_password_resets AS r
			WHERE r.expires_at <= clock_timestamp()
			ORDER BY r.expires_at, r.user_id
			FOR UPDATE OF r SKIP LOCKED
			LIMIT $1
		)
		DELETE FROM auth_password_resets AS r
		WHERE r.user_id IN (SELECT user_id FROM expired)`, outboxMaintenanceBatchSize); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		WITH expired AS (
			SELECT r.id
			FROM auth_email_change_requests AS r
			WHERE r.expires_at <= clock_timestamp()
			ORDER BY r.expires_at, r.id
			FOR UPDATE OF r SKIP LOCKED
			LIMIT $1
		)
		DELETE FROM auth_email_change_requests AS r
		WHERE r.id IN (SELECT id FROM expired)`, outboxMaintenanceBatchSize); err != nil {
		return err
	}
	return tx.Commit()
}

var _ application.MailAttemptRecorder = (*Store)(nil)

func (s *Store) CleanupMail(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		WITH settings AS (
			SELECT COALESCE((SELECT retention_days FROM auth_email_settings WHERE singleton = true), 30) AS retention_days
		), doomed AS (
			SELECT o.id
			FROM auth_mail_outbox AS o CROSS JOIN settings AS s
			WHERE COALESCE(o.finished_at, o.sent_at, o.canceled_at, o.dead_at) IS NOT NULL
			  AND (o.lease_token IS NULL OR o.lease_expires_at <= clock_timestamp())
			  AND COALESCE(o.finished_at, o.sent_at, o.canceled_at, o.dead_at) < clock_timestamp() - make_interval(days => s.retention_days)
			ORDER BY COALESCE(o.finished_at, o.sent_at, o.canceled_at, o.dead_at), o.id
			FOR UPDATE OF o SKIP LOCKED
			LIMIT $1
		)
		DELETE FROM auth_mail_outbox AS o
		WHERE o.id IN (SELECT id FROM doomed)`, outboxMaintenanceBatchSize)
	return err
}
