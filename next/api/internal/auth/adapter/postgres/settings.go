package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
)

func (s *Store) GetEmailSettings(ctx context.Context) (application.EmailSettingsRecord, error) {
	var record application.EmailSettingsRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT smtp_host, smtp_port, smtp_security, smtp_username,
		       smtp_password_ciphertext, from_address, from_name, default_locale,
		       auto_retry_count, retention_days, revision, updated_at
		FROM auth_email_settings WHERE singleton = true`).Scan(
		&record.Host, &record.Port, &record.Security, &record.Username,
		&record.PasswordCiphertext, &record.FromAddress, &record.FromName,
		&record.DefaultLocale, &record.AutoRetryCount, &record.RetentionDays, &record.Revision, &record.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return application.EmailSettingsRecord{}, application.ErrMailNotConfigured
	}
	return record, err
}

func (s *Store) SaveEmailSettings(ctx context.Context, expectedRevision int64, record application.EmailSettingsRecord) (application.EmailSettingsRecord, error) {
	var saved application.EmailSettingsRecord
	if expectedRevision == 0 {
		err := s.db.QueryRowContext(ctx, `
			INSERT INTO auth_email_settings
				(singleton, smtp_host, smtp_port, smtp_security, smtp_username, smtp_password_ciphertext, from_address, from_name, default_locale, auto_retry_count, retention_days, revision)
			VALUES (true, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 1)
			ON CONFLICT (singleton) DO NOTHING
			RETURNING smtp_host, smtp_port, smtp_security, smtp_username, smtp_password_ciphertext, from_address, from_name, default_locale, auto_retry_count, retention_days, revision, updated_at`,
			record.Host, record.Port, record.Security, record.Username, record.PasswordCiphertext, record.FromAddress, record.FromName, string(record.DefaultLocale), record.AutoRetryCount, record.RetentionDays).Scan(
			&saved.Host, &saved.Port, &saved.Security, &saved.Username, &saved.PasswordCiphertext, &saved.FromAddress, &saved.FromName, &saved.DefaultLocale, &saved.AutoRetryCount, &saved.RetentionDays, &saved.Revision, &saved.UpdatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return application.EmailSettingsRecord{}, application.ErrStaleRevision
		}
		if err != nil {
			return application.EmailSettingsRecord{}, err
		}
		return saved, nil
	}
	err := s.db.QueryRowContext(ctx, `
		UPDATE auth_email_settings
		SET smtp_host = $1, smtp_port = $2, smtp_security = $3, smtp_username = $4,
		    smtp_password_ciphertext = $5, from_address = $6, from_name = $7,
		    default_locale = $8, auto_retry_count = $9, retention_days = $10,
		    revision = revision + 1, updated_at = clock_timestamp()
		WHERE singleton = true AND revision = $11
		RETURNING smtp_host, smtp_port, smtp_security, smtp_username, smtp_password_ciphertext, from_address, from_name, default_locale, auto_retry_count, retention_days, revision, updated_at`,
		record.Host, record.Port, record.Security, record.Username, record.PasswordCiphertext, record.FromAddress, record.FromName, string(record.DefaultLocale), record.AutoRetryCount, record.RetentionDays, expectedRevision).Scan(
		&saved.Host, &saved.Port, &saved.Security, &saved.Username, &saved.PasswordCiphertext, &saved.FromAddress, &saved.FromName, &saved.DefaultLocale, &saved.AutoRetryCount, &saved.RetentionDays, &saved.Revision, &saved.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return application.EmailSettingsRecord{}, application.ErrStaleRevision
	}
	return saved, err
}

var _ application.EmailSettingsStore = (*Store)(nil)
