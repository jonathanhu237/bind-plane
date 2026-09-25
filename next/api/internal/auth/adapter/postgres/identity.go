package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
)

func (s *Store) GetSystemIdentity(ctx context.Context) (application.SystemIdentityRecord, error) {
	var record application.SystemIdentityRecord
	var mediaType sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT system_name, icon_media_type, icon_bytes, revision, updated_at
		FROM auth_system_identity WHERE singleton = true`).Scan(
		&record.SystemName, &mediaType, &record.IconBytes, &record.Revision, &record.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SystemIdentityRecord{}, application.ErrSystemIdentityNotConfigured
	}
	if mediaType.Valid {
		record.IconMediaType = mediaType.String
	}
	return record, err
}

func (s *Store) SaveSystemIdentity(ctx context.Context, expectedRevision int64, record application.SystemIdentityRecord) (application.SystemIdentityRecord, error) {
	var saved application.SystemIdentityRecord
	var mediaType sql.NullString
	if expectedRevision == 0 {
		err := s.db.QueryRowContext(ctx, `
			INSERT INTO auth_system_identity
				(singleton, system_name, icon_media_type, icon_bytes, revision)
			VALUES (true, $1, $2, $3, 1)
			ON CONFLICT (singleton) DO NOTHING
			RETURNING system_name, icon_media_type, icon_bytes, revision, updated_at`,
			record.SystemName, record.IconMediaType, record.IconBytes).Scan(
			&saved.SystemName, &mediaType, &saved.IconBytes, &saved.Revision, &saved.UpdatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return application.SystemIdentityRecord{}, application.ErrStaleRevision
		}
		if err != nil {
			return application.SystemIdentityRecord{}, err
		}
		if mediaType.Valid {
			saved.IconMediaType = mediaType.String
		}
		return saved, nil
	}
	err := s.db.QueryRowContext(ctx, `
		UPDATE auth_system_identity
		SET system_name = $1, icon_media_type = $2, icon_bytes = $3,
			revision = revision + 1, updated_at = clock_timestamp()
		WHERE singleton = true AND revision = $4
		RETURNING system_name, icon_media_type, icon_bytes, revision, updated_at`,
		record.SystemName, record.IconMediaType, record.IconBytes, expectedRevision).Scan(
		&saved.SystemName, &mediaType, &saved.IconBytes, &saved.Revision, &saved.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SystemIdentityRecord{}, application.ErrStaleRevision
	}
	if mediaType.Valid {
		saved.IconMediaType = mediaType.String
	}
	return saved, err
}

var _ application.SystemIdentityStore = (*Store)(nil)
