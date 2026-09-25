package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

func (s *Store) UpdateUserLocale(ctx context.Context, userID string, locale domain.Locale) (domain.User, error) {
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	var user domain.User
	err := s.db.QueryRowContext(operationCtx, `
		UPDATE auth_users SET locale = $2
		WHERE id = $1::uuid AND disabled_at IS NULL
		RETURNING id::text, name, email, created_at, locale,
			EXISTS(SELECT 1 FROM auth_user_avatars a WHERE a.user_id = auth_users.id),
			COALESCE((SELECT version FROM auth_user_avatars a WHERE a.user_id = auth_users.id), 0)`, userID, locale).Scan(
		&user.ID, &user.Name, &user.Email, &user.CreatedAt, &user.Locale, &user.HasAvatar, &user.AvatarVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, application.ErrAccountNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	return user, nil
}

func (s *Store) UpdateUserName(ctx context.Context, userID string, name domain.Name) (domain.User, error) {
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	var user domain.User
	err := s.db.QueryRowContext(operationCtx, `
		UPDATE auth_users
		SET name = $2
		WHERE id = $1::uuid AND disabled_at IS NULL
		RETURNING id::text, name, email, created_at, COALESCE(locale, ''),
			EXISTS(SELECT 1 FROM auth_user_avatars a WHERE a.user_id = auth_users.id),
			COALESCE((SELECT version FROM auth_user_avatars a WHERE a.user_id = auth_users.id), 0)`, userID, string(name)).Scan(
		&user.ID, &user.Name, &user.Email, &user.CreatedAt, &user.Locale, &user.HasAvatar, &user.AvatarVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, application.ErrAccountNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	return user, nil
}

func (s *Store) GetUserAvatar(ctx context.Context, userID string) (domain.Avatar, error) {
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	var avatar domain.Avatar
	err := s.db.QueryRowContext(operationCtx, `SELECT media_type, image_bytes, version, updated_at FROM auth_user_avatars WHERE user_id = $1::uuid`, userID).Scan(&avatar.MediaType, &avatar.Bytes, &avatar.Version, &avatar.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Avatar{}, application.ErrAvatarNotFound
	}
	if err != nil {
		return domain.Avatar{}, err
	}
	return avatar, nil
}

func (s *Store) SaveUserAvatar(ctx context.Context, userID string, data []byte, mediaType string) (domain.Avatar, error) {
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	var avatar domain.Avatar
	err := s.db.QueryRowContext(operationCtx, `
		INSERT INTO auth_user_avatars (user_id, media_type, image_bytes, version, updated_at)
		SELECT id, $2, $3, 1, clock_timestamp()
		FROM auth_users WHERE id = $1::uuid AND disabled_at IS NULL
		ON CONFLICT (user_id) DO UPDATE SET
			media_type = EXCLUDED.media_type,
			image_bytes = EXCLUDED.image_bytes,
			version = auth_user_avatars.version + 1,
			updated_at = clock_timestamp()
		RETURNING media_type, image_bytes, version, updated_at`, userID, mediaType, data).Scan(&avatar.MediaType, &avatar.Bytes, &avatar.Version, &avatar.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Avatar{}, application.ErrAccountNotFound
	}
	if err != nil {
		return domain.Avatar{}, err
	}
	return avatar, nil
}

func (s *Store) DeleteUserAvatar(ctx context.Context, userID string) error {
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	_, err := s.db.ExecContext(operationCtx, `DELETE FROM auth_user_avatars WHERE user_id = $1::uuid`, userID)
	return err
}

func (s *Store) ChangePassword(ctx context.Context, userID string, expectedAuthVersion int64, passwordHash string, locale domain.Locale, notificationTTL time.Duration) (domain.User, time.Time, error) {
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(operationCtx, nil)
	if err != nil {
		return domain.User{}, time.Time{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var currentVersion int64
	var disabled bool
	if err := tx.QueryRowContext(operationCtx, `SELECT auth_version, disabled_at IS NOT NULL FROM auth_users WHERE id = $1::uuid FOR UPDATE`, userID).Scan(&currentVersion, &disabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, time.Time{}, application.ErrAccountNotFound
		}
		return domain.User{}, time.Time{}, err
	}
	if disabled {
		return domain.User{}, time.Time{}, application.ErrAccountDisabled
	}
	if currentVersion != expectedAuthVersion {
		return domain.User{}, time.Time{}, application.ErrStaleRevision
	}
	// The account row is locked above; invalidate legacy rows that lack a
	// durable material snapshot. Materialized tasks remain independent history.
	if _, err := tx.ExecContext(operationCtx, `UPDATE auth_mail_outbox SET canceled_at = clock_timestamp(), finished_at = clock_timestamp(), lease_token = NULL, lease_expires_at = NULL, last_error_code = 'superseded' WHERE user_id = $1::uuid AND kind = 'password_reset' AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL AND material_ciphertext IS NULL`, userID); err != nil {
		return domain.User{}, time.Time{}, err
	}
	var user domain.User
	var changedAt time.Time
	if err := tx.QueryRowContext(operationCtx, `
		UPDATE auth_users
		SET password_hash = $2, auth_version = auth_version + 1
		WHERE id = $1::uuid AND auth_version < 9223372036854775807
		RETURNING id::text, name, email, clock_timestamp(), created_at, COALESCE(locale, ''),
			EXISTS(SELECT 1 FROM auth_user_avatars a WHERE a.user_id = auth_users.id),
			COALESCE((SELECT version FROM auth_user_avatars a WHERE a.user_id = auth_users.id), 0)`, userID, passwordHash).Scan(
		&user.ID, &user.Name, &user.Email, &changedAt, &user.CreatedAt, &user.Locale, &user.HasAvatar, &user.AvatarVersion); err != nil {
		return domain.User{}, time.Time{}, err
	}
	if _, err := tx.ExecContext(operationCtx, `DELETE FROM auth_password_resets WHERE user_id = $1::uuid`, userID); err != nil {
		return domain.User{}, time.Time{}, err
	}
	if notificationTTL <= 0 {
		notificationTTL = 24 * time.Hour
	}
	if !locale.Valid() {
		locale = domain.LocaleEnglish
	}
	systemName, err := mailTaskSystemName(operationCtx, tx)
	if err != nil {
		return domain.User{}, time.Time{}, err
	}
	expiresAt := changedAt.Add(notificationTTL)
	material, err := s.sealMailTaskMaterial(application.MailTaskMaterial{Version: 1, Kind: application.MailPasswordChanged, Name: user.Name, Email: user.Email, Locale: locale, SystemName: systemName, CreatedAt: changedAt, ExpiresAt: expiresAt})
	if err != nil {
		return domain.User{}, time.Time{}, err
	}
	if _, err := tx.ExecContext(operationCtx, `
		INSERT INTO auth_mail_outbox (kind, user_id, recipient_email, recipient_name, locale, system_name, material_ciphertext, expires_at, created_at)
		VALUES ('password_changed', $1::uuid, $2, $3, $4, $5, $6, $7, $8)`, userID, user.Email, user.Name, locale, systemName, nullableBytes(material), expiresAt, changedAt); err != nil {
		return domain.User{}, time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.User{}, time.Time{}, err
	}
	return user, changedAt, nil
}

func (s *Store) GetEmailChangeRequest(ctx context.Context, userID string) (domain.EmailChangeRequest, error) {
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	var request domain.EmailChangeRequest
	err := s.db.QueryRowContext(operationCtx, `
		SELECT id::text, user_id::text, old_email, new_email, expires_at, resend_after,
			attempts_remaining, revision, created_at, selector, verifier_digest
		FROM auth_email_change_requests WHERE user_id = $1::uuid`, userID).Scan(
		&request.ID, &request.UserID, &request.OldEmail, &request.NewEmail, &request.ExpiresAt,
		&request.ResendAfter, &request.AttemptsRemaining, &request.Revision, &request.CreatedAt, &request.Selector, &request.VerifierDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.EmailChangeRequest{}, application.ErrEmailChangeNotFound
	}
	if err != nil {
		return domain.EmailChangeRequest{}, err
	}
	return request, nil
}

func (s *Store) RequestEmailChange(ctx context.Context, userID string, newEmail domain.Email, selector, digest []byte, ttl time.Duration, locale domain.Locale) (domain.EmailChangeRequest, error) {
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(operationCtx, nil)
	if err != nil {
		return domain.EmailChangeRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockEmailChangeTarget(operationCtx, tx, newEmail.Canonical); err != nil {
		return domain.EmailChangeRequest{}, err
	}
	var currentName, currentEmail, currentCanonical string
	var disabled bool
	if err := tx.QueryRowContext(operationCtx, `SELECT name, email, email_canonical, disabled_at IS NOT NULL FROM auth_users WHERE id = $1::uuid FOR UPDATE`, userID).Scan(&currentName, &currentEmail, &currentCanonical, &disabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.EmailChangeRequest{}, application.ErrAccountNotFound
		}
		return domain.EmailChangeRequest{}, err
	}
	if disabled {
		return domain.EmailChangeRequest{}, application.ErrAccountDisabled
	}
	if currentCanonical == newEmail.Canonical {
		return domain.EmailChangeRequest{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "newEmail", Code: "same_email"}}}
	}
	var occupied bool
	if err := tx.QueryRowContext(operationCtx, `SELECT EXISTS(SELECT 1 FROM auth_users WHERE email_canonical = $1 AND id <> $2::uuid) OR EXISTS(SELECT 1 FROM auth_user_invitations WHERE email_canonical = $1)`, newEmail.Canonical, userID).Scan(&occupied); err != nil {
		return domain.EmailChangeRequest{}, err
	}
	if occupied {
		return domain.EmailChangeRequest{}, application.ErrEmailAlreadyRegistered
	}
	if ttl <= 0 {
		ttl = application.EmailChangeValidity
	}
	if !locale.Valid() {
		locale = domain.LocaleEnglish
	}
	var previousRevision int64
	var previousResendAfter, previousCreatedAt time.Time
	var hasPrevious bool
	if err := tx.QueryRowContext(operationCtx, `SELECT revision, resend_after, created_at FROM auth_email_change_requests WHERE user_id = $1::uuid FOR UPDATE`, userID).Scan(&previousRevision, &previousResendAfter, &previousCreatedAt); errors.Is(err, sql.ErrNoRows) {
		previousRevision = 0
	} else if err != nil {
		return domain.EmailChangeRequest{}, err
	} else {
		hasPrevious = true
		// Request and resend share one per-user authority. The row lock makes
		// this cooldown check atomic with replacement, including an exhausted
		// request and a changed destination address. A caller must wait for the
		// original sixty-second window before issuing another code.
		var cooldownActive bool
		if err := tx.QueryRowContext(operationCtx, `SELECT $1 > clock_timestamp()`, previousResendAfter).Scan(&cooldownActive); err != nil {
			return domain.EmailChangeRequest{}, err
		}
		if cooldownActive {
			return domain.EmailChangeRequest{}, application.ErrEmailChangeResendTooSoon
		}
	}
	// Invalidate only legacy rows lacking a durable snapshot. Materialized
	// tasks remain independently retryable historical messages.
	if _, err := tx.ExecContext(operationCtx, `UPDATE auth_mail_outbox SET canceled_at = clock_timestamp(), finished_at = clock_timestamp(), last_error_code = 'superseded', lease_token = NULL, lease_expires_at = NULL WHERE user_id = $1::uuid AND kind = 'email_change_code' AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL AND material_ciphertext IS NULL`, userID); err != nil {
		return domain.EmailChangeRequest{}, err
	}
	if _, err := tx.ExecContext(operationCtx, `DELETE FROM auth_email_change_requests WHERE user_id = $1::uuid`, userID); err != nil {
		return domain.EmailChangeRequest{}, err
	}
	var request domain.EmailChangeRequest
	var createdAt any
	if hasPrevious {
		createdAt = previousCreatedAt
	}
	if err := tx.QueryRowContext(operationCtx, `
		WITH timestamps AS MATERIALIZED (SELECT statement_timestamp() AS now)
		INSERT INTO auth_email_change_requests (user_id, old_email, new_email, new_email_canonical, selector, verifier_digest, expires_at, resend_after, attempts_remaining, revision, created_at, updated_at)
		SELECT $1::uuid, $2, $3, $4, $5, $6, timestamps.now + ($7 * INTERVAL '1 second'), timestamps.now + interval '60 seconds', 5, $8, COALESCE($9::timestamptz, timestamps.now), timestamps.now
		FROM timestamps
		RETURNING id::text, user_id::text, old_email, new_email, expires_at, resend_after, attempts_remaining, revision, created_at, selector, verifier_digest`,
		userID, currentEmail, newEmail.Display, newEmail.Canonical, selector, digest, ttl.Seconds(), previousRevision+1, createdAt).Scan(
		&request.ID, &request.UserID, &request.OldEmail, &request.NewEmail, &request.ExpiresAt,
		&request.ResendAfter, &request.AttemptsRemaining, &request.Revision, &request.CreatedAt, &request.Selector, &request.VerifierDigest); err != nil {
		return domain.EmailChangeRequest{}, err
	}
	systemName, err := mailTaskSystemName(operationCtx, tx)
	if err != nil {
		return domain.EmailChangeRequest{}, err
	}
	// A replacement keeps the authority's original creation timestamp for
	// cooldown/audit semantics, but the newly queued delivery is a new task and
	// must receive its own creation timestamp for ordering and retention.
	taskCreatedAt := request.CreatedAt
	if hasPrevious {
		if err := tx.QueryRowContext(operationCtx, `SELECT clock_timestamp()`).Scan(&taskCreatedAt); err != nil {
			return domain.EmailChangeRequest{}, err
		}
	}
	material, err := s.sealMailTaskMaterial(application.MailTaskMaterial{Version: 1, Kind: application.MailEmailChangeCode, Name: currentName, Email: newEmail.Display, Locale: locale, SystemName: systemName, CreatedAt: taskCreatedAt, ExpiresAt: request.ExpiresAt, EmailSelector: selector, VerifierDigest: digest})
	if err != nil {
		return domain.EmailChangeRequest{}, err
	}
	if _, err := tx.ExecContext(operationCtx, `
		INSERT INTO auth_mail_outbox (kind, user_id, recipient_email, recipient_name, email_change_request_id, email_change_selector, locale, system_name, material_ciphertext, expires_at, created_at)
		VALUES ('email_change_code', $1::uuid, $2, $3, $4::uuid, $5, $6, $7, $8, $9, $10)`, userID, newEmail.Display, currentName, request.ID, selector, locale, systemName, nullableBytes(material), request.ExpiresAt, taskCreatedAt); err != nil {
		return domain.EmailChangeRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.EmailChangeRequest{}, err
	}
	return request, nil
}

func (s *Store) ResendEmailChange(ctx context.Context, userID string, selector, digest []byte, ttl time.Duration, locale domain.Locale) (domain.EmailChangeRequest, error) {
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(operationCtx, nil)
	if err != nil {
		return domain.EmailChangeRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var request domain.EmailChangeRequest
	var canonical string
	var disabled bool
	var currentName string
	if err := tx.QueryRowContext(operationCtx, `SELECT name, disabled_at IS NOT NULL FROM auth_users WHERE id = $1::uuid FOR UPDATE`, userID).Scan(&currentName, &disabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.EmailChangeRequest{}, application.ErrAccountNotFound
		}
		return domain.EmailChangeRequest{}, err
	}
	if disabled {
		return domain.EmailChangeRequest{}, application.ErrAccountDisabled
	}
	if err := tx.QueryRowContext(operationCtx, `
		SELECT id::text, user_id::text, old_email, new_email, expires_at,
			resend_after, attempts_remaining, revision, created_at, new_email_canonical,
			selector, verifier_digest
		FROM auth_email_change_requests
		WHERE user_id = $1::uuid FOR UPDATE`, userID).Scan(
		&request.ID, &request.UserID, &request.OldEmail, &request.NewEmail, &request.ExpiresAt,
		&request.ResendAfter, &request.AttemptsRemaining, &request.Revision, &request.CreatedAt, &canonical, &request.Selector, &request.VerifierDigest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.EmailChangeRequest{}, application.ErrEmailChangeNotFound
		}
		return domain.EmailChangeRequest{}, err
	}
	var expiresValid, resendReady bool
	if err := tx.QueryRowContext(operationCtx, `SELECT expires_at > clock_timestamp(), resend_after <= clock_timestamp() FROM auth_email_change_requests WHERE user_id = $1::uuid`, userID).Scan(&expiresValid, &resendReady); err != nil {
		return domain.EmailChangeRequest{}, err
	}
	if !expiresValid {
		return domain.EmailChangeRequest{}, application.ErrEmailChangeExpired
	}
	if request.AttemptsRemaining <= 0 {
		return domain.EmailChangeRequest{}, application.ErrEmailChangeAttemptsExceeded
	}
	if !resendReady {
		return domain.EmailChangeRequest{}, application.ErrEmailChangeResendTooSoon
	}
	if ttl <= 0 {
		ttl = application.EmailChangeValidity
	}
	if !locale.Valid() {
		locale = domain.LocaleEnglish
	}
	// Invalidate only legacy rows before replacing the authority. Materialized
	// tasks retain their original code and can be retried without reissuing it.
	if _, err := tx.ExecContext(operationCtx, `UPDATE auth_mail_outbox SET canceled_at = clock_timestamp(), finished_at = clock_timestamp(), lease_token = NULL, lease_expires_at = NULL, last_error_code = 'superseded' WHERE user_id = $1::uuid AND kind = 'email_change_code' AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL AND material_ciphertext IS NULL`, userID); err != nil {
		return domain.EmailChangeRequest{}, err
	}
	if err := tx.QueryRowContext(operationCtx, `
		UPDATE auth_email_change_requests
		SET selector = $2, verifier_digest = $3, expires_at = clock_timestamp() + ($4 * INTERVAL '1 second'),
			resend_after = clock_timestamp() + interval '60 seconds', attempts_remaining = 5, revision = revision + 1, updated_at = clock_timestamp()
		WHERE user_id = $1::uuid
		RETURNING id::text, user_id::text, old_email, new_email, expires_at, resend_after, attempts_remaining, revision, created_at, selector, verifier_digest`, userID, selector, digest, ttl.Seconds()).Scan(
		&request.ID, &request.UserID, &request.OldEmail, &request.NewEmail, &request.ExpiresAt,
		&request.ResendAfter, &request.AttemptsRemaining, &request.Revision, &request.CreatedAt, &request.Selector, &request.VerifierDigest); err != nil {
		return domain.EmailChangeRequest{}, err
	}
	systemName, err := mailTaskSystemName(operationCtx, tx)
	if err != nil {
		return domain.EmailChangeRequest{}, err
	}
	// A resend is a new delivery task even though the authority request keeps
	// its original creation timestamp for audit and expiry semantics.
	var taskCreatedAt time.Time
	if err := tx.QueryRowContext(operationCtx, `SELECT clock_timestamp()`).Scan(&taskCreatedAt); err != nil {
		return domain.EmailChangeRequest{}, err
	}
	material, err := s.sealMailTaskMaterial(application.MailTaskMaterial{Version: 1, Kind: application.MailEmailChangeCode, Name: currentName, Email: request.NewEmail, Locale: locale, SystemName: systemName, CreatedAt: taskCreatedAt, ExpiresAt: request.ExpiresAt, EmailSelector: selector, VerifierDigest: digest})
	if err != nil {
		return domain.EmailChangeRequest{}, err
	}
	if _, err := tx.ExecContext(operationCtx, `
		INSERT INTO auth_mail_outbox (kind, user_id, recipient_email, recipient_name, email_change_request_id, email_change_selector, locale, system_name, material_ciphertext, expires_at, created_at)
		VALUES ('email_change_code', $1::uuid, $2, $3, $4::uuid, $5, $6, $7, $8, $9, $10)`, userID, request.NewEmail, currentName, request.ID, selector, locale, systemName, nullableBytes(material), request.ExpiresAt, taskCreatedAt); err != nil {
		return domain.EmailChangeRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.EmailChangeRequest{}, err
	}
	return request, nil
}

func (s *Store) CompleteEmailChange(ctx context.Context, userID, requestID string, presentedDigest []byte, locale domain.Locale, notificationTTL time.Duration) (domain.User, time.Time, error) {
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	// Read only the destination before taking locks. All mutating paths then
	// acquire the destination advisory lock before user/request rows.
	var target string
	if err := s.db.QueryRowContext(operationCtx, `SELECT new_email_canonical FROM auth_email_change_requests WHERE id = $1::uuid AND user_id = $2::uuid`, requestID, userID).Scan(&target); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, time.Time{}, application.ErrInvalidEmailChange
		}
		return domain.User{}, time.Time{}, err
	}
	tx, err := s.db.BeginTx(operationCtx, nil)
	if err != nil {
		return domain.User{}, time.Time{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockEmailChangeTarget(operationCtx, tx, target); err != nil {
		return domain.User{}, time.Time{}, err
	}
	var request domain.EmailChangeRequest
	var canonical, currentEmail string
	var disabled bool
	if err := tx.QueryRowContext(operationCtx, `SELECT email, disabled_at IS NOT NULL FROM auth_users WHERE id = $1::uuid FOR UPDATE`, userID).Scan(&currentEmail, &disabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, time.Time{}, application.ErrAccountNotFound
		}
		return domain.User{}, time.Time{}, err
	}
	if disabled {
		return domain.User{}, time.Time{}, application.ErrAccountDisabled
	}
	if err := tx.QueryRowContext(operationCtx, `
		SELECT id::text, user_id::text, old_email, new_email, new_email_canonical,
			expires_at, resend_after, attempts_remaining, revision, created_at, selector, verifier_digest
		FROM auth_email_change_requests
		WHERE id = $1::uuid AND user_id = $2::uuid FOR UPDATE`, requestID, userID).Scan(
		&request.ID, &request.UserID, &request.OldEmail, &request.NewEmail, &canonical, &request.ExpiresAt,
		&request.ResendAfter, &request.AttemptsRemaining, &request.Revision, &request.CreatedAt, &request.Selector, &request.VerifierDigest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, time.Time{}, application.ErrInvalidEmailChange
		}
		return domain.User{}, time.Time{}, err
	}
	var validTime bool
	if err := tx.QueryRowContext(operationCtx, `SELECT expires_at > clock_timestamp() FROM auth_email_change_requests WHERE id = $1::uuid`, requestID).Scan(&validTime); err != nil {
		return domain.User{}, time.Time{}, err
	}
	if !validTime {
		return domain.User{}, time.Time{}, application.ErrEmailChangeExpired
	}
	if request.AttemptsRemaining <= 0 {
		return domain.User{}, time.Time{}, application.ErrEmailChangeAttemptsExceeded
	}
	if !equalDigest(request.VerifierDigest, presentedDigest) {
		var remaining int
		if err := tx.QueryRowContext(operationCtx, `UPDATE auth_email_change_requests SET attempts_remaining = attempts_remaining - 1, updated_at = clock_timestamp() WHERE id = $1::uuid RETURNING attempts_remaining`, requestID).Scan(&remaining); err != nil {
			return domain.User{}, time.Time{}, err
		}
		if remaining <= 0 {
			_, _ = tx.ExecContext(operationCtx, `UPDATE auth_mail_outbox SET canceled_at = clock_timestamp(), finished_at = clock_timestamp(), lease_token = NULL, lease_expires_at = NULL, last_error_code = 'invalid_email_change' WHERE email_change_request_id = $1::uuid AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL AND material_ciphertext IS NULL`, requestID)
		}
		if err := tx.Commit(); err != nil {
			return domain.User{}, time.Time{}, err
		}
		if remaining <= 0 {
			return domain.User{}, time.Time{}, application.ErrEmailChangeAttemptsExceeded
		}
		return domain.User{}, time.Time{}, application.ErrInvalidEmailChangeCode
	}
	if currentEmail != request.OldEmail {
		return domain.User{}, time.Time{}, application.ErrInvalidEmailChange
	}
	var occupied bool
	if err := tx.QueryRowContext(operationCtx, `SELECT EXISTS(SELECT 1 FROM auth_users WHERE email_canonical = $1 AND id <> $2::uuid) OR EXISTS(SELECT 1 FROM auth_user_invitations WHERE email_canonical = $1)`, canonical, userID).Scan(&occupied); err != nil {
		return domain.User{}, time.Time{}, err
	}
	if occupied {
		return domain.User{}, time.Time{}, application.ErrEmailAlreadyRegistered
	}
	// Stop only legacy deliveries before consuming the authority. Materialized
	// task rows preserve the original notification independently.
	if _, err := tx.ExecContext(operationCtx, `UPDATE auth_mail_outbox SET canceled_at = clock_timestamp(), finished_at = clock_timestamp(), lease_token = NULL, lease_expires_at = NULL, last_error_code = 'superseded' WHERE email_change_request_id = $1::uuid AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL AND material_ciphertext IS NULL`, requestID); err != nil {
		return domain.User{}, time.Time{}, err
	}
	if _, err := tx.ExecContext(operationCtx, `UPDATE auth_mail_outbox SET canceled_at = clock_timestamp(), finished_at = clock_timestamp(), lease_token = NULL, lease_expires_at = NULL, last_error_code = 'superseded' WHERE user_id = $1::uuid AND kind = 'password_reset' AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL AND material_ciphertext IS NULL`, userID); err != nil {
		return domain.User{}, time.Time{}, err
	}
	var user domain.User
	var changedAt time.Time
	if err := tx.QueryRowContext(operationCtx, `
		UPDATE auth_users
		SET email = $2, email_canonical = $3, auth_version = auth_version + 1
		WHERE id = $1::uuid AND auth_version < 9223372036854775807
		RETURNING id::text, name, email, clock_timestamp(), created_at, COALESCE(locale, ''),
			EXISTS(SELECT 1 FROM auth_user_avatars a WHERE a.user_id = auth_users.id),
			COALESCE((SELECT version FROM auth_user_avatars a WHERE a.user_id = auth_users.id), 0)`, userID, request.NewEmail, canonical).Scan(
		&user.ID, &user.Name, &user.Email, &changedAt, &user.CreatedAt, &user.Locale, &user.HasAvatar, &user.AvatarVersion); err != nil {
		return domain.User{}, time.Time{}, err
	}
	if _, err := tx.ExecContext(operationCtx, `DELETE FROM auth_email_change_requests WHERE id = $1::uuid`, requestID); err != nil {
		return domain.User{}, time.Time{}, err
	}
	if _, err := tx.ExecContext(operationCtx, `DELETE FROM auth_password_resets WHERE user_id = $1::uuid`, userID); err != nil {
		return domain.User{}, time.Time{}, err
	}
	if notificationTTL <= 0 {
		notificationTTL = 24 * time.Hour
	}
	if !locale.Valid() {
		locale = domain.LocaleEnglish
	}
	systemName, err := mailTaskSystemName(operationCtx, tx)
	if err != nil {
		return domain.User{}, time.Time{}, err
	}
	expiresAt := changedAt.Add(notificationTTL)
	material, err := s.sealMailTaskMaterial(application.MailTaskMaterial{Version: 1, Kind: application.MailEmailChanged, Name: user.Name, Email: request.OldEmail, Locale: locale, SystemName: systemName, CreatedAt: changedAt, ExpiresAt: expiresAt})
	if err != nil {
		return domain.User{}, time.Time{}, err
	}
	if _, err := tx.ExecContext(operationCtx, `
		INSERT INTO auth_mail_outbox (kind, user_id, recipient_email, recipient_name, locale, system_name, material_ciphertext, expires_at, created_at)
		VALUES ('email_changed', $1::uuid, $2, $3, $4, $5, $6, $7, $8)`, userID, request.OldEmail, user.Name, locale, systemName, nullableBytes(material), expiresAt, changedAt); err != nil {
		return domain.User{}, time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.User{}, time.Time{}, err
	}
	return user, changedAt, nil
}

func lockEmailChangeTarget(ctx context.Context, tx *sql.Tx, canonical string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, canonical)
	return err
}

var (
	_ application.PersonalProfileStore = (*Store)(nil)
	_ application.PasswordChangeStore  = (*Store)(nil)
	_ application.EmailChangeStore     = (*Store)(nil)
)
