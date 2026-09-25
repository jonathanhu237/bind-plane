package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// mutateUserLifecycle uses the existing roles-before-users lock order. Locking
// all role definitions also serializes assignment changes and permission edits
// while the actor's authority and the last available administrator are checked.
func (s *Store) mutateUserLifecycle(ctx context.Context, actorID, userID string, revision int64, action string) (domain.AccessUser, error) {
	if !domain.IsCanonicalUUID(actorID) || !domain.IsCanonicalUUID(userID) {
		return domain.AccessUser{}, application.ErrUserNotFound
	}
	if actorID == userID {
		return domain.AccessUser{}, application.ErrSelfUserOperation
	}
	if revision < 0 {
		return domain.AccessUser{}, application.ErrStaleRevision
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.AccessUser{}, err
	}
	defer tx.Rollback()
	// Invitation operations acquire their email lock before role/user locks.
	var email string
	if err = tx.QueryRowContext(ctx, `SELECT email_canonical FROM auth_users WHERE id=$1::uuid`, userID).Scan(&email); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.AccessUser{}, application.ErrUserNotFound
		}
		return domain.AccessUser{}, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, email); err != nil {
		return domain.AccessUser{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text FROM auth_roles ORDER BY id FOR UPDATE`)
	if err != nil {
		return domain.AccessUser{}, err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return domain.AccessUser{}, err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return domain.AccessUser{}, err
	}
	if err = lockUsersTx(ctx, tx, actorID, userID); err != nil {
		return domain.AccessUser{}, err
	}
	var actorEnabled, authorized bool
	if err = tx.QueryRowContext(ctx, `SELECT disabled_at IS NULL, EXISTS(SELECT 1 FROM auth_user_roles ur JOIN auth_roles r ON r.id=ur.role_id LEFT JOIN auth_role_permissions rp ON rp.role_id=r.id WHERE ur.user_id=u.id AND (r.system_key='super_admin' OR rp.permission_key='users.write')) FROM auth_users u WHERE u.id=$1::uuid`, actorID).Scan(&actorEnabled, &authorized); err != nil {
		return domain.AccessUser{}, err
	}
	if !actorEnabled || !authorized {
		return domain.AccessUser{}, application.ErrForbidden
	}
	var user domain.AccessUser
	var super bool
	err = tx.QueryRowContext(ctx, `SELECT u.id::text,u.name,u.email,u.created_at,u.auth_version,u.disabled_at IS NOT NULL,COALESCE(u.locale,''),EXISTS(SELECT 1 FROM auth_user_avatars a WHERE a.user_id=u.id),COALESCE((SELECT version FROM auth_user_avatars a WHERE a.user_id=u.id),0),EXISTS(SELECT 1 FROM auth_user_roles ur JOIN auth_roles r ON r.id=ur.role_id WHERE ur.user_id=u.id AND r.system_key='super_admin') FROM auth_users u WHERE u.id=$1::uuid`, userID).Scan(&user.User.ID, &user.User.Name, &user.User.Email, &user.User.CreatedAt, &user.AuthVersion, &user.User.Disabled, &user.User.Locale, &user.User.HasAvatar, &user.User.AvatarVersion, &super)
	if err != nil {
		return domain.AccessUser{}, err
	}
	if user.AuthVersion != revision {
		return domain.AccessUser{}, application.ErrStaleRevision
	}
	if (action == "deactivate" && user.User.Disabled) || (action == "reactivate" && !user.User.Disabled) {
		return domain.AccessUser{}, application.ErrStaleRevision
	}
	if action != "reactivate" && super && !user.User.Disabled {
		var available int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM auth_users u JOIN auth_user_roles ur ON ur.user_id=u.id JOIN auth_roles r ON r.id=ur.role_id WHERE r.system_key='super_admin' AND u.disabled_at IS NULL`).Scan(&available)
		if err != nil {
			return domain.AccessUser{}, err
		}
		if available <= 1 {
			return domain.AccessUser{}, application.ErrLastSuperAdmin
		}
	}
	switch action {
	case "deactivate":
		_, err = tx.ExecContext(ctx, `UPDATE auth_sessions SET disabled_revoked=true WHERE user_id=$1::uuid AND auth_version=$2 AND idle_expires_at>clock_timestamp() AND absolute_expires_at>clock_timestamp()`, userID, revision)
		if err != nil {
			return domain.AccessUser{}, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE auth_users SET disabled_at=clock_timestamp(),auth_version=auth_version+1 WHERE id=$1::uuid`, userID)
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM auth_password_resets WHERE user_id=$1::uuid`, userID)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE auth_mail_outbox SET canceled_at=clock_timestamp(),finished_at=clock_timestamp(),last_error_code='superseded',lease_token=NULL,lease_expires_at=NULL WHERE user_id=$1::uuid AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL AND material_ciphertext IS NULL`, userID)
		}
	case "reactivate":
		_, err = tx.ExecContext(ctx, `UPDATE auth_users SET disabled_at=NULL,auth_version=auth_version+1 WHERE id=$1::uuid`, userID)
	case "delete":
		_, err = tx.ExecContext(ctx, `INSERT INTO auth_deleted_user_identities(user_id,name,email) VALUES($1,$2,$3)`, userID, user.User.Name, user.User.Email)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE auth_user_invitations SET created_by_name=$2,created_by_email=$3,created_by_user_key=$1,created_by=NULL WHERE created_by=$1::uuid`, userID, user.User.Name, user.User.Email)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE auth_mail_outbox SET canceled_at=clock_timestamp(), finished_at=clock_timestamp(), lease_token=NULL, lease_expires_at=NULL, last_error_code='superseded' WHERE user_id=$1::uuid AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL AND material_ciphertext IS NULL`, userID)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM auth_users WHERE id=$1::uuid`, userID)
		}
	default:
		return domain.AccessUser{}, application.ErrForbidden
	}
	if err != nil {
		return domain.AccessUser{}, err
	}
	if action != "delete" {
		page, readErr := scanUsers(ctx, tx, `SELECT u.id::text,u.name,u.email,u.created_at,u.auth_version,u.disabled_at IS NOT NULL,COALESCE(u.locale,''),
		EXISTS(SELECT 1 FROM auth_user_avatars a WHERE a.user_id=u.id),COALESCE((SELECT version FROM auth_user_avatars a WHERE a.user_id=u.id),0),
		COALESCE(r.id::text,''),COALESCE(r.system_key,''),COALESCE(r.name,''),COALESCE(r.description,''),COALESCE(r.revision,0),COALESCE(rp.permission_key,'')
		FROM auth_users u LEFT JOIN auth_user_roles ur ON ur.user_id=u.id LEFT JOIN auth_roles r ON r.id=ur.role_id LEFT JOIN auth_role_permissions rp ON rp.role_id=r.id WHERE u.id=$1::uuid ORDER BY r.id,rp.permission_key`, userID)
		if readErr != nil {
			return domain.AccessUser{}, readErr
		}
		if len(page.Items) != 1 {
			return domain.AccessUser{}, application.ErrUserNotFound
		}
		user = page.Items[0]
	}
	if err = tx.Commit(); err != nil {
		return domain.AccessUser{}, err
	}
	return user, nil
}
