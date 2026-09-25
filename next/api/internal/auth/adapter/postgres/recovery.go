package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

func (s *Store) FindPasswordResetTarget(ctx context.Context, selector, verifierDigest []byte) (domain.User, error) {
	var user domain.User
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id::text, u.name, u.email, u.created_at
		FROM auth_password_resets AS r
		JOIN auth_users AS u ON u.id = r.user_id
		WHERE r.selector = $1 AND r.verifier_digest = $2 AND r.expires_at > clock_timestamp() AND u.disabled_at IS NULL`, selector, verifierDigest).
		Scan(&user.ID, &user.Name, &user.Email, &user.CreatedAt)
	if err == sql.ErrNoRows {
		return domain.User{}, application.ErrInvalidPasswordResetToken
	}
	return user, err
}

func (s *Store) RequestPasswordReset(ctx context.Context, canonical string, selector, verifierDigest []byte, ttl time.Duration, locale domain.Locale) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var userID string
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM auth_users WHERE email_canonical = $1 AND disabled_at IS NULL FOR UPDATE`, canonical).Scan(&userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return tx.Commit()
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE auth_mail_outbox
		SET canceled_at = clock_timestamp(), finished_at = clock_timestamp(), last_error_code = 'superseded', lease_token = NULL, lease_expires_at = NULL
		WHERE user_id = $1::uuid AND kind = 'password_reset'
		  AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL
		  AND material_ciphertext IS NULL`, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO auth_password_resets (user_id, selector, verifier_digest, expires_at)
		VALUES ($1::uuid, $2, $3, clock_timestamp() + ($4 * INTERVAL '1 second'))
		ON CONFLICT (user_id) DO UPDATE
		SET selector = EXCLUDED.selector,
		    verifier_digest = EXCLUDED.verifier_digest,
		    expires_at = EXCLUDED.expires_at,
		    created_at = EXCLUDED.created_at`, userID, selector, verifierDigest, ttl.Seconds()); err != nil {
		return err
	}
	var name, email, systemName string
	var createdAt, expiresAt time.Time
	if err := tx.QueryRowContext(ctx, `
		SELECT u.name, u.email, pr.created_at, pr.expires_at
		FROM auth_password_resets AS pr JOIN auth_users AS u ON u.id = pr.user_id
		WHERE pr.user_id = $1::uuid AND u.disabled_at IS NULL`, userID).Scan(&name, &email, &createdAt, &expiresAt); err != nil {
		return err
	}
	systemName, err = mailTaskSystemName(ctx, tx)
	if err != nil {
		return err
	}
	material, err := s.sealMailTaskMaterial(application.MailTaskMaterial{Version: 1, Kind: application.MailPasswordReset, Name: name, Email: email, Locale: locale, SystemName: systemName, CreatedAt: createdAt, ExpiresAt: expiresAt, ResetSelector: selector, VerifierDigest: verifierDigest})
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO auth_mail_outbox (kind, user_id, reset_selector, recipient_email, recipient_name, locale, system_name, material_ciphertext, expires_at, created_at)
		VALUES ('password_reset', $1::uuid, $2, $3, $4, $5, $6, $7, $8, $9)`, userID, selector, email, name, string(locale), systemName, nullableBytes(material), expiresAt, createdAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) PreflightPasswordReset(ctx context.Context, selector, verifierDigest []byte) error {
	var stored []byte
	var valid bool
	err := s.db.QueryRowContext(ctx, `SELECT verifier_digest, expires_at > clock_timestamp() FROM auth_password_resets WHERE selector = $1`, selector).Scan(&stored, &valid)
	if err != nil {
		if err == sql.ErrNoRows {
			return application.ErrInvalidPasswordResetToken
		}
		return err
	}
	if !valid || !equalDigest(stored, verifierDigest) {
		return application.ErrInvalidPasswordResetToken
	}
	return nil
}

func (s *Store) CompletePasswordReset(ctx context.Context, selector, verifierDigest []byte, passwordHash string, locale domain.Locale, notificationTTL time.Duration) (time.Time, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var userID string
	// Match the account-before-reset lock order used by issuance and deactivation.
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM auth_users WHERE id=(SELECT user_id FROM auth_password_resets WHERE selector=$1) AND disabled_at IS NULL FOR UPDATE`, selector).Scan(&userID); err != nil {
		if err == sql.ErrNoRows {
			return time.Time{}, application.ErrInvalidPasswordResetToken
		}
		return time.Time{}, err
	}
	var stored []byte
	var valid bool
	if err := tx.QueryRowContext(ctx, `
		SELECT r.user_id::text, r.verifier_digest, r.expires_at > clock_timestamp()
		FROM auth_password_resets AS r
		JOIN auth_users AS u ON u.id = r.user_id
		WHERE r.selector = $1 AND u.disabled_at IS NULL
		FOR UPDATE OF r, u`, selector).Scan(&userID, &stored, &valid); err != nil {
		if err == sql.ErrNoRows {
			return time.Time{}, application.ErrInvalidPasswordResetToken
		}
		return time.Time{}, err
	}
	if !valid || !equalDigest(stored, verifierDigest) {
		return time.Time{}, application.ErrInvalidPasswordResetToken
	}
	// Consume the reset generation without allowing a worker to claim another
	// active copy between authority consumption and the update below.
	if _, err := tx.ExecContext(ctx, `
		UPDATE auth_mail_outbox
		SET canceled_at = clock_timestamp(), finished_at = clock_timestamp(), last_error_code = 'superseded', lease_token = NULL, lease_expires_at = NULL
		WHERE user_id = $1::uuid AND kind = 'password_reset'
		  AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL
		  AND material_ciphertext IS NULL`, userID); err != nil {
		return time.Time{}, err
	}

	var changedAt time.Time
	if err := tx.QueryRowContext(ctx, `
		UPDATE auth_users
		SET password_hash = $2, auth_version = auth_version + 1
		WHERE id = $1::uuid AND auth_version < 9223372036854775807
		RETURNING clock_timestamp()`, userID, passwordHash).Scan(&changedAt); err != nil {
		if err == sql.ErrNoRows {
			return time.Time{}, application.ErrDependencyUnavailable
		}
		return time.Time{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_password_resets WHERE user_id = $1::uuid`, userID); err != nil {
		return time.Time{}, err
	}
	var recipientEmail, recipientName, systemName string
	if err := tx.QueryRowContext(ctx, `SELECT email, name FROM auth_users WHERE id = $1::uuid`, userID).Scan(&recipientEmail, &recipientName); err != nil {
		return time.Time{}, err
	}
	systemName, err = mailTaskSystemName(ctx, tx)
	if err != nil {
		return time.Time{}, err
	}
	expiresAt := changedAt.Add(notificationTTL)
	material, err := s.sealMailTaskMaterial(application.MailTaskMaterial{Version: 1, Kind: application.MailPasswordChanged, Name: recipientName, Email: recipientEmail, Locale: locale, SystemName: systemName, CreatedAt: changedAt, ExpiresAt: expiresAt})
	if err != nil {
		return time.Time{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO auth_mail_outbox (kind, user_id, recipient_email, recipient_name, locale, system_name, material_ciphertext, expires_at, created_at)
		VALUES ('password_changed', $1::uuid, $2, $3, $4, $5, $6, $7, $8)`, userID, recipientEmail, recipientName, string(locale), systemName, nullableBytes(material), expiresAt, changedAt); err != nil {
		return time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return time.Time{}, err
	}
	return changedAt, nil
}
