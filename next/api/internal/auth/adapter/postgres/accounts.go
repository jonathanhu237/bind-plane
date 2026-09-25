package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

func (s *Store) FindByCanonicalEmail(ctx context.Context, canonical string) (domain.Account, error) {
	var account domain.Account
	err := s.db.QueryRowContext(ctx, `SELECT id::text, name, email, password_hash, auth_version, created_at, COALESCE(locale, ''), EXISTS(SELECT 1 FROM auth_user_avatars a WHERE a.user_id = auth_users.id), COALESCE((SELECT version FROM auth_user_avatars a WHERE a.user_id = auth_users.id), 0) FROM auth_users WHERE email_canonical = $1 AND disabled_at IS NULL`, canonical).Scan(&account.User.ID, &account.User.Name, &account.User.Email, &account.PasswordHash, &account.AuthVersion, &account.User.CreatedAt, &account.User.Locale, &account.User.HasAvatar, &account.User.AvatarVersion)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.Account{}, application.ErrAccountNotFound
		}
		return domain.Account{}, err
	}
	return account, nil
}

func (s *Store) FindPublicAccountByID(ctx context.Context, id string) (domain.Account, error) {
	var account domain.Account
	err := s.db.QueryRowContext(ctx, `SELECT id::text, name, email, password_hash, auth_version, created_at, COALESCE(locale, ''), EXISTS(SELECT 1 FROM auth_user_avatars a WHERE a.user_id = auth_users.id), COALESCE((SELECT version FROM auth_user_avatars a WHERE a.user_id = auth_users.id), 0) FROM auth_users WHERE id = $1::uuid AND disabled_at IS NULL`, id).Scan(&account.User.ID, &account.User.Name, &account.User.Email, &account.PasswordHash, &account.AuthVersion, &account.User.CreatedAt, &account.User.Locale, &account.User.HasAvatar, &account.User.AvatarVersion)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.Account{}, application.ErrAccountNotFound
		}
		return domain.Account{}, err
	}
	return account, nil
}

func (s *Store) InitializeLocale(ctx context.Context, id string, locale domain.Locale) (domain.Account, error) {
	if !locale.Valid() {
		return s.FindPublicAccountByID(ctx, id)
	}
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	var account domain.Account
	err := s.db.QueryRowContext(operationCtx, `
		UPDATE auth_users
		SET locale = $2
		WHERE id = $1::uuid AND disabled_at IS NULL AND locale IS NULL
		RETURNING id::text, name, email, password_hash, auth_version, created_at, locale,
			EXISTS(SELECT 1 FROM auth_user_avatars a WHERE a.user_id = auth_users.id),
			COALESCE((SELECT version FROM auth_user_avatars a WHERE a.user_id = auth_users.id), 0)`, id, locale).Scan(
		&account.User.ID, &account.User.Name, &account.User.Email, &account.PasswordHash, &account.AuthVersion,
		&account.User.CreatedAt, &account.User.Locale, &account.User.HasAvatar, &account.User.AvatarVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return s.FindPublicAccountByID(ctx, id)
	}
	if err != nil {
		return domain.Account{}, err
	}
	return account, nil
}

func (s *Store) FindPublicByID(ctx context.Context, id string) (domain.User, error) {
	var user domain.User
	err := s.db.QueryRowContext(ctx, `SELECT id::text, name, email, created_at, COALESCE(locale, ''), EXISTS(SELECT 1 FROM auth_user_avatars a WHERE a.user_id = auth_users.id), COALESCE((SELECT version FROM auth_user_avatars a WHERE a.user_id = auth_users.id), 0) FROM auth_users WHERE id = $1::uuid`, id).Scan(&user.ID, &user.Name, &user.Email, &user.CreatedAt, &user.Locale, &user.HasAvatar, &user.AvatarVersion)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.User{}, application.ErrAccountNotFound
		}
		return domain.User{}, err
	}
	return user, nil
}
