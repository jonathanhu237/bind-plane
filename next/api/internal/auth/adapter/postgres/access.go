package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

func (s *Store) FindPrincipalByID(ctx context.Context, id string) (domain.Principal, error) {
	var principal domain.Principal
	var authVersion int64
	err := s.db.QueryRowContext(ctx, `SELECT id::text, name, email, created_at, auth_version, COALESCE(locale, ''), EXISTS(SELECT 1 FROM auth_user_avatars a WHERE a.user_id = auth_users.id), COALESCE((SELECT version FROM auth_user_avatars a WHERE a.user_id = auth_users.id), 0) FROM auth_users WHERE id = $1::uuid AND disabled_at IS NULL`, id).
		Scan(&principal.User.ID, &principal.User.Name, &principal.User.Email, &principal.User.CreatedAt, &authVersion, &principal.User.Locale, &principal.User.HasAvatar, &principal.User.AvatarVersion)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Principal{}, application.ErrAccountNotFound
		}
		return domain.Principal{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id::text, COALESCE(r.system_key, ''), r.name, r.description, r.revision,
		       r.created_at, r.updated_at, COALESCE(rp.permission_key, '')
		FROM auth_user_roles AS ur
		JOIN auth_roles AS r ON r.id = ur.role_id
		LEFT JOIN auth_role_permissions AS rp ON rp.role_id = r.id
		WHERE ur.user_id = $1::uuid
		ORDER BY (r.system_key IS NULL), r.name_canonical, r.id, rp.permission_key`, id)
	if err != nil {
		return domain.Principal{}, err
	}
	defer rows.Close()
	roles := make(map[string]*domain.Role)
	for rows.Next() {
		var role domain.Role
		var permission string
		if err := rows.Scan(&role.ID, &role.SystemKey, &role.Name, &role.Description, &role.Revision, &role.CreatedAt, &role.UpdatedAt, &permission); err != nil {
			return domain.Principal{}, err
		}
		current := roles[role.ID]
		if current == nil {
			current = &role
			roles[role.ID] = current
		}
		if role.SystemKey == "super_admin" {
			principal.SuperAdmin = true
		}
		if permission != "" {
			current.Permissions = append(current.Permissions, domain.PermissionKey(permission))
		}
	}
	if err := rows.Err(); err != nil {
		return domain.Principal{}, err
	}
	for _, role := range roles {
		sort.Slice(role.Permissions, func(i, j int) bool { return role.Permissions[i] < role.Permissions[j] })
		principal.Roles = append(principal.Roles, *role)
		principal.Permissions = append(principal.Permissions, role.Permissions...)
	}
	sort.Slice(principal.Roles, func(i, j int) bool { return principal.Roles[i].Name < principal.Roles[j].Name })
	// The application expands the built-in role from the live permission
	// catalog. A persisted summary would therefore be incomplete whenever the
	// principal also has a custom role; omit it so the application can derive
	// authority exclusively from the normalized assignments.
	if principal.SuperAdmin {
		principal.Permissions = nil
	} else {
		principal.Permissions = uniquePermissionKeys(principal.Permissions)
	}
	_ = authVersion // auth_version is checked by Authentication before this projection.
	return principal, nil
}

func uniquePermissionKeys(keys []domain.PermissionKey) []domain.PermissionKey {
	seen := make(map[domain.PermissionKey]struct{}, len(keys))
	result := make([]domain.PermissionKey, 0, len(keys))
	for _, key := range keys {
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			result = append(result, key)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func (s *Store) ListRoles(ctx context.Context) ([]domain.Role, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id::text, COALESCE(r.system_key, ''), r.name, r.description, r.revision,
		       r.created_at, r.updated_at, COALESCE(ur.assignment_count, 0), COALESCE(rp.permission_key, '')
		FROM auth_roles AS r
		LEFT JOIN (
			SELECT role_id, count(*) AS assignment_count
			FROM (
				SELECT role_id FROM auth_user_roles
				UNION ALL
				SELECT role_id FROM auth_invitation_roles
			) AS role_references
			GROUP BY role_id
		) AS ur ON ur.role_id = r.id
		LEFT JOIN auth_role_permissions AS rp ON rp.role_id = r.id
		ORDER BY (r.system_key IS NULL), r.name_canonical, r.id, rp.permission_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := map[string]*domain.Role{}
	for rows.Next() {
		var role domain.Role
		var permission string
		if err := rows.Scan(&role.ID, &role.SystemKey, &role.Name, &role.Description, &role.Revision, &role.CreatedAt, &role.UpdatedAt, &role.AssignmentCount, &permission); err != nil {
			return nil, err
		}
		current := roles[role.ID]
		if current == nil {
			current = &role
			roles[role.ID] = current
		}
		if permission != "" {
			current.Permissions = append(current.Permissions, domain.PermissionKey(permission))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]domain.Role, 0, len(roles))
	for _, role := range roles {
		role.Permissions = uniquePermissionKeys(role.Permissions)
		result = append(result, *role)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SystemKey != "" && result[j].SystemKey == "" {
			return true
		}
		if result[i].SystemKey == "" && result[j].SystemKey != "" {
			return false
		}
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func (s *Store) ListRoleOptions(ctx context.Context) ([]application.RoleOption, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id::text, name
		FROM auth_roles
		ORDER BY (system_key IS NULL), name_canonical, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	options := make([]application.RoleOption, 0)
	for rows.Next() {
		var option application.RoleOption
		if err := rows.Scan(&option.ID, &option.Name); err != nil {
			return nil, err
		}
		options = append(options, option)
	}
	return options, rows.Err()
}

func (s *Store) FindRole(ctx context.Context, id string) (domain.Role, error) {
	var role domain.Role
	var systemKey sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT r.id::text, r.system_key, r.name, r.description, r.revision, r.created_at, r.updated_at,
		       (SELECT count(*) FROM auth_user_roles WHERE role_id = r.id) + (SELECT count(*) FROM auth_invitation_roles WHERE role_id = r.id)
		FROM auth_roles AS r WHERE r.id = $1::uuid`, id).
		Scan(&role.ID, &systemKey, &role.Name, &role.Description, &role.Revision, &role.CreatedAt, &role.UpdatedAt, &role.AssignmentCount)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Role{}, application.ErrRoleNotFound
		}
		return domain.Role{}, err
	}
	role.SystemKey = systemKey.String
	rows, err := s.db.QueryContext(ctx, `SELECT permission_key FROM auth_role_permissions WHERE role_id = $1::uuid ORDER BY permission_key`, id)
	if err != nil {
		return domain.Role{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return domain.Role{}, err
		}
		role.Permissions = append(role.Permissions, domain.PermissionKey(key))
	}
	if err := rows.Err(); err != nil {
		return domain.Role{}, err
	}
	return role, nil
}

func (s *Store) CreateRole(ctx context.Context, name, description string, permissions []domain.PermissionKey) (domain.Role, error) {
	return s.createRole(ctx, "", name, description, permissions)
}

func (s *Store) CreateRoleWithinScope(ctx context.Context, actorID, name, description string, permissions []domain.PermissionKey) (domain.Role, error) {
	return s.createRole(ctx, actorID, name, description, permissions)
}

func (s *Store) createRole(ctx context.Context, actorID, name, description string, permissions []domain.PermissionKey) (domain.Role, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Role{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if actorID != "" {
		actorSuper, actorPermissions, scopeErr := lockActorForRoleMutation(ctx, tx, actorID)
		if scopeErr != nil {
			return domain.Role{}, scopeErr
		}
		if !actorSuper && (!actorPermissions[domain.PermissionRolesRead] || !actorPermissions[domain.PermissionRolesWrite]) {
			return domain.Role{}, application.ErrForbidden
		}
		if !actorSuper && !permissionSetContains(actorPermissions, permissions) {
			return domain.Role{}, application.ErrPermissionScope
		}
	}
	var role domain.Role
	canonical := domain.CanonicalRoleName(name)
	err = tx.QueryRowContext(ctx, `
		INSERT INTO auth_roles (name, name_canonical, description)
		VALUES ($1, $2, $3)
		RETURNING id::text, name, description, revision, created_at, updated_at`, name, canonical, description).
		Scan(&role.ID, &role.Name, &role.Description, &role.Revision, &role.CreatedAt, &role.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Role{}, application.ErrRoleAlreadyExists
		}
		return domain.Role{}, err
	}
	if err := insertRolePermissions(ctx, tx, role.ID, permissions); err != nil {
		return domain.Role{}, err
	}
	role.Permissions = append([]domain.PermissionKey(nil), permissions...)
	if err := tx.Commit(); err != nil {
		return domain.Role{}, err
	}
	return role, nil
}

func insertRolePermissions(ctx context.Context, tx *sql.Tx, roleID string, permissions []domain.PermissionKey) error {
	for _, permission := range permissions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO auth_role_permissions (role_id, permission_key) VALUES ($1::uuid, $2)`, roleID, string(permission)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ReplaceRole(ctx context.Context, id string, revision int64, name, description string, permissions []domain.PermissionKey) (domain.Role, error) {
	return s.replaceRole(ctx, "", id, revision, name, description, permissions)
}

func (s *Store) ReplaceRoleWithinScope(ctx context.Context, actorID, id string, revision int64, name, description string, permissions []domain.PermissionKey) (domain.Role, error) {
	return s.replaceRole(ctx, actorID, id, revision, name, description, permissions)
}

func (s *Store) replaceRole(ctx context.Context, actorID, id string, revision int64, name, description string, permissions []domain.PermissionKey) (domain.Role, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Role{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var actorSuper bool
	var actorPermissions map[domain.PermissionKey]bool
	if actorID != "" {
		actorSuper, actorPermissions, err = lockActorForRoleMutation(ctx, tx, actorID, id)
		if err != nil {
			return domain.Role{}, err
		}
	}
	var systemKey sql.NullString
	var oldRevision int64
	roleQuery := `SELECT system_key, revision FROM auth_roles WHERE id = $1::uuid`
	if actorID == "" {
		roleQuery += " FOR UPDATE"
	}
	if err := tx.QueryRowContext(ctx, roleQuery, id).Scan(&systemKey, &oldRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Role{}, application.ErrRoleNotFound
		}
		return domain.Role{}, err
	}
	if systemKey.Valid {
		return domain.Role{}, application.ErrImmutableRole
	}
	if revision != oldRevision {
		return domain.Role{}, application.ErrStaleRevision
	}
	var oldPermissions []domain.PermissionKey
	rows, err := tx.QueryContext(ctx, `SELECT permission_key FROM auth_role_permissions WHERE role_id = $1::uuid ORDER BY permission_key`, id)
	if err != nil {
		return domain.Role{}, err
	}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return domain.Role{}, err
		}
		oldPermissions = append(oldPermissions, domain.PermissionKey(key))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return domain.Role{}, err
	}
	rows.Close()
	if actorID != "" {
		if !actorSuper && (!actorPermissions[domain.PermissionRolesRead] || !actorPermissions[domain.PermissionRolesWrite]) {
			return domain.Role{}, application.ErrForbidden
		}
		if !actorSuper && (!permissionSetContains(actorPermissions, oldPermissions) || !permissionSetContains(actorPermissions, permissions)) {
			return domain.Role{}, application.ErrPermissionScope
		}
	}
	changed := !samePermissionSet(oldPermissions, permissions)
	canonical := domain.CanonicalRoleName(name)
	var role domain.Role
	if err := tx.QueryRowContext(ctx, `UPDATE auth_roles SET name = $2, name_canonical = $3, description = $4, revision = revision + 1, updated_at = clock_timestamp() WHERE id = $1::uuid RETURNING id::text, name, description, revision, created_at, updated_at`, id, name, canonical, description).
		Scan(&role.ID, &role.Name, &role.Description, &role.Revision, &role.CreatedAt, &role.UpdatedAt); err != nil {
		if isUniqueViolation(err) {
			return domain.Role{}, application.ErrRoleAlreadyExists
		}
		return domain.Role{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_role_permissions WHERE role_id = $1::uuid`, id); err != nil {
		return domain.Role{}, err
	}
	if err := insertRolePermissions(ctx, tx, id, permissions); err != nil {
		return domain.Role{}, err
	}
	if changed {
		if _, err := tx.ExecContext(ctx, `UPDATE auth_users SET auth_version = auth_version + 1 WHERE id IN (SELECT user_id FROM auth_user_roles WHERE role_id = $1::uuid)`, id); err != nil {
			return domain.Role{}, err
		}
	}
	role.Permissions = append([]domain.PermissionKey(nil), permissions...)
	if err := tx.Commit(); err != nil {
		return domain.Role{}, err
	}
	return role, nil
}

func samePermissionSet(left, right []domain.PermissionKey) bool {
	return strings.Join(permissionStrings(uniquePermissionKeys(left)), "\x00") == strings.Join(permissionStrings(uniquePermissionKeys(right)), "\x00")
}

func permissionStrings(keys []domain.PermissionKey) []string {
	result := make([]string, len(keys))
	for i, key := range keys {
		result[i] = string(key)
	}
	return result
}

func (s *Store) DeleteRole(ctx context.Context, id string) error {
	_, err := s.deleteRole(ctx, "", id)
	return err
}

func (s *Store) DeleteRoleWithinScope(ctx context.Context, actorID, id string) error {
	_, err := s.deleteRole(ctx, actorID, id)
	return err
}

func (s *Store) DeleteRoleWithSnapshot(ctx context.Context, actorID, id string) (domain.Role, error) {
	return s.deleteRole(ctx, actorID, id)
}

func (s *Store) deleteRole(ctx context.Context, actorID, id string) (domain.Role, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Role{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var actorSuper bool
	var actorPermissions map[domain.PermissionKey]bool
	if actorID != "" {
		actorSuper, actorPermissions, err = lockActorForRoleMutation(ctx, tx, actorID, id)
		if err != nil {
			return domain.Role{}, err
		}
	}
	var role domain.Role
	var systemKey sql.NullString
	roleQuery := `SELECT id::text, system_key, name, description, revision, created_at, updated_at FROM auth_roles WHERE id = $1::uuid`
	if actorID == "" {
		roleQuery += " FOR UPDATE"
	}
	if err := tx.QueryRowContext(ctx, roleQuery, id).Scan(&role.ID, &systemKey, &role.Name, &role.Description, &role.Revision, &role.CreatedAt, &role.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Role{}, application.ErrRoleNotFound
		}
		return domain.Role{}, err
	}
	role.SystemKey = systemKey.String
	if systemKey.Valid {
		return domain.Role{}, application.ErrImmutableRole
	}
	if actorID != "" {
		if !actorSuper && (!actorPermissions[domain.PermissionRolesRead] || !actorPermissions[domain.PermissionRolesWrite]) {
			return domain.Role{}, application.ErrForbidden
		}
		_, permissions, permissionErr := rolePermissionsTx(ctx, tx, id)
		if permissionErr != nil {
			return domain.Role{}, permissionErr
		}
		role.Permissions = append([]domain.PermissionKey(nil), permissions...)
		if !actorSuper && !permissionSetContains(actorPermissions, permissions) {
			return domain.Role{}, application.ErrPermissionScope
		}
	} else {
		_, permissions, permissionErr := rolePermissionsTx(ctx, tx, id)
		if permissionErr != nil {
			return domain.Role{}, permissionErr
		}
		role.Permissions = append([]domain.PermissionKey(nil), permissions...)
	}
	var references int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM auth_user_roles WHERE role_id = $1::uuid) + (SELECT count(*) FROM auth_invitation_roles WHERE role_id = $1::uuid)`, id).Scan(&references); err != nil {
		return domain.Role{}, err
	}
	if references > 0 {
		return domain.Role{}, application.ErrRoleInUse
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_roles WHERE id = $1::uuid`, id); err != nil {
		return domain.Role{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Role{}, err
	}
	return role, nil
}

func (s *Store) ListUsers(ctx context.Context, cursor string, limit int) (application.UserPage, error) {
	return s.listUsersWithOptions(ctx, application.AccessListOptions{Cursor: cursor, Limit: limit, Sort: "createdAt", Direction: "desc"}, true)
}

// FindAccessUser is the immutable business snapshot seam used by operation
// history. It reads the user and all currently assigned role descriptors in a
// single statement before a mutation; credentials are never selected.
func (s *Store) FindAccessUser(ctx context.Context, id string) (domain.AccessUser, error) {
	page, err := scanUsers(ctx, s.db, `
		SELECT u.id::text, u.name, u.email, u.created_at, u.auth_version, (u.disabled_at IS NOT NULL), COALESCE(u.locale, ''),
		       EXISTS(SELECT 1 FROM auth_user_avatars a WHERE a.user_id = u.id), COALESCE((SELECT version FROM auth_user_avatars a WHERE a.user_id = u.id), 0),
		       COALESCE(r.id::text, ''), COALESCE(r.system_key, ''), COALESCE(r.name, ''), COALESCE(r.description, ''), COALESCE(r.revision, 0),
		       COALESCE(rp.permission_key, '')
		FROM auth_users AS u
		LEFT JOIN auth_user_roles AS ur ON ur.user_id = u.id
		LEFT JOIN auth_roles AS r ON r.id = ur.role_id
		LEFT JOIN auth_role_permissions AS rp ON rp.role_id = r.id
		WHERE u.id = $1::uuid
		ORDER BY (r.system_key IS NULL), r.name_canonical, r.id, rp.permission_key`, id)
	if err != nil {
		return domain.AccessUser{}, err
	}
	if len(page.Items) == 0 {
		return domain.AccessUser{}, application.ErrUserNotFound
	}
	return page.Items[0], nil
}

func (s *Store) ListUsersWithOptions(ctx context.Context, options application.AccessListOptions) (application.UserPage, error) {
	return s.listUsersWithOptions(ctx, options, false)
}

func (s *Store) listUsersWithOptions(ctx context.Context, options application.AccessListOptions, forceLegacy bool) (application.UserPage, error) {
	options = storeListOptions(options, false)
	cursor, legacy, err := storeCursor(options, false)
	if err != nil {
		return application.UserPage{}, err
	}
	legacy = legacy || (forceLegacy && (options.Cursor == "" || domain.IsCanonicalUUID(options.Cursor)))
	args := []any{likePattern(options.Query)}
	where := []string{"($1 = '' OR lower(u.name) LIKE '%' || $1 || '%' ESCAPE '\\' OR lower(u.email) LIKE '%' || $1 || '%' ESCAPE '\\')"}
	if options.Status == "disabled" {
		where = append(where, "u.disabled_at IS NOT NULL")
	} else if options.Status == "active" {
		where = append(where, "u.disabled_at IS NULL")
	}
	if options.RoleID != "" {
		args = append(args, options.RoleID)
		where = append(where, "EXISTS (SELECT 1 FROM auth_user_roles AS ur_filter WHERE ur_filter.user_id = u.id AND ur_filter.role_id = $"+strconv.Itoa(len(args))+"::uuid)")
	}
	if cursor.ID != "" {
		if legacy {
			args = append(args, cursor.ID)
			where = append(where, "u.id > $"+strconv.Itoa(len(args))+"::uuid")
		} else {
			boundary, boundaryArgs, boundaryErr := cursorBoundary("u", cursor, options.Sort, options.Direction, len(args))
			if boundaryErr != nil {
				return application.UserPage{}, boundaryErr
			}
			where = append(where, boundary)
			args = append(args, boundaryArgs...)
		}
	}
	args = append(args, options.Limit+1)
	order := accessOrder("u", options.Sort, options.Direction)
	if legacy {
		// UUID cursors were issued by the previous ID-ascending endpoint. Keep
		// that sequence self-consistent for clients finishing an old page.
		order = "u.id ASC"
	}
	query := fmt.Sprintf(`
		WITH selected_users AS (
			SELECT u.id, u.name, u.email, u.email_canonical, u.created_at, u.auth_version, u.disabled_at, u.locale
			FROM auth_users AS u
			WHERE %s
			ORDER BY %s
			LIMIT $%d
		)
		SELECT u.id::text, u.name, u.email, u.created_at, u.auth_version, (u.disabled_at IS NOT NULL), COALESCE(u.locale, ''),
		       EXISTS(SELECT 1 FROM auth_user_avatars a WHERE a.user_id = u.id), COALESCE((SELECT version FROM auth_user_avatars a WHERE a.user_id = u.id), 0),
		       COALESCE(r.id::text, ''), COALESCE(r.system_key, ''), COALESCE(r.name, ''), COALESCE(r.description, ''), COALESCE(r.revision, 0),
		       COALESCE(rp.permission_key, '')
		FROM selected_users AS u
		LEFT JOIN auth_user_roles AS ur ON ur.user_id = u.id
		LEFT JOIN auth_roles AS r ON r.id = ur.role_id
		LEFT JOIN auth_role_permissions AS rp ON rp.role_id = r.id
		ORDER BY %s, (r.system_key IS NULL), r.name_canonical, r.id, rp.permission_key`, strings.Join(where, " AND "), order, len(args), order)
	page, err := scanUsers(ctx, s.db, query, args...)
	if err != nil {
		return application.UserPage{}, err
	}
	if len(page.Items) > options.Limit {
		last := page.Items[options.Limit-1]
		if legacy {
			page.NextCursor = last.User.ID
		} else {
			page.NextCursor, err = encodeUserCursor(last, options)
			if err != nil {
				return application.UserPage{}, err
			}
		}
		page.Items = page.Items[:options.Limit]
	}
	return page, nil
}

func scanUsers(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, query string, args ...any) (application.UserPage, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return application.UserPage{}, err
	}
	defer rows.Close()
	users := map[string]*domain.AccessUser{}
	order := []string{}
	for rows.Next() {
		var user domain.AccessUser
		var role domain.Role
		var permission string
		if err := rows.Scan(&user.User.ID, &user.User.Name, &user.User.Email, &user.User.CreatedAt, &user.AuthVersion, &user.User.Disabled, &user.User.Locale, &user.User.HasAvatar, &user.User.AvatarVersion, &role.ID, &role.SystemKey, &role.Name, &role.Description, &role.Revision, &permission); err != nil {
			return application.UserPage{}, err
		}
		current := users[user.User.ID]
		if current == nil {
			current = &user
			users[user.User.ID] = current
			order = append(order, user.User.ID)
		}
		if role.ID == "" {
			continue
		}
		if len(current.Roles) == 0 || current.Roles[len(current.Roles)-1].ID != role.ID {
			current.Roles = append(current.Roles, role)
		}
		if permission != "" {
			index := len(current.Roles) - 1
			current.Roles[index].Permissions = append(current.Roles[index].Permissions, domain.PermissionKey(permission))
		}
	}
	if err := rows.Err(); err != nil {
		return application.UserPage{}, err
	}
	page := application.UserPage{}
	for _, id := range order {
		page.Items = append(page.Items, *users[id])
	}
	return page, nil
}

func (s *Store) DeactivateUserWithRevision(ctx context.Context, actorID, userID string, revision int64) (domain.AccessUser, error) {
	return s.mutateUserLifecycle(ctx, actorID, userID, revision, "deactivate")
}

func (s *Store) ReactivateUserWithRevision(ctx context.Context, actorID, userID string, revision int64) (domain.AccessUser, error) {
	return s.mutateUserLifecycle(ctx, actorID, userID, revision, "reactivate")
}

func (s *Store) DeleteUserWithRevision(ctx context.Context, actorID, userID string, revision int64) error {
	_, err := s.mutateUserLifecycle(ctx, actorID, userID, revision, "delete")
	return err
}

func (s *Store) ReplaceUserRoles(ctx context.Context, userID string, authVersion int64, roleIDs []string) (domain.AccessUser, error) {
	return s.replaceUserRoles(ctx, "", userID, authVersion, roleIDs)
}

// ReplaceUserRolesWithinScope is the transaction-aware assignment path used by
// the application service. It rechecks the actor's current authority and the
// permissions of every changed role while all relevant rows are locked.
func (s *Store) ReplaceUserRolesWithinScope(ctx context.Context, actorID, userID string, authVersion int64, roleIDs []string) (domain.AccessUser, error) {
	return s.replaceUserRoles(ctx, actorID, userID, authVersion, roleIDs)
}

func (s *Store) replaceUserRoles(ctx context.Context, actorID, userID string, authVersion int64, roleIDs []string) (domain.AccessUser, error) {
	if len(roleIDs) == 0 {
		return domain.AccessUser{}, application.ErrInvalidRoleSet
	}
	seen := make(map[string]struct{}, len(roleIDs))
	for _, roleID := range roleIDs {
		if _, exists := seen[roleID]; exists {
			return domain.AccessUser{}, application.ErrInvalidRoleSet
		}
		seen[roleID] = struct{}{}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.AccessUser{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var superRoleID string
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM auth_roles WHERE system_key = 'super_admin'`).Scan(&superRoleID); err != nil {
		return domain.AccessUser{}, err
	}

	// Role updates, invitation acceptance, and assignment replacement all lock
	// role rows in one deterministic UUID order before locking owning users.
	// Include the current actor and target assignments so a removal is checked
	// against the role permissions that are actually being detached.
	lockedRoleIDs := make([]string, 0, len(roleIDs)+1)
	lockedRoleIDs = append(lockedRoleIDs, roleIDs...)
	lockedRoleIDs = append(lockedRoleIDs, superRoleID)
	if actorID != "" {
		for _, id := range []string{actorID, userID} {
			ids, listErr := roleIDsForUserTx(ctx, tx, id)
			if listErr != nil {
				return domain.AccessUser{}, listErr
			}
			lockedRoleIDs = append(lockedRoleIDs, ids...)
		}
	}
	sort.Strings(lockedRoleIDs)
	for index, roleID := range lockedRoleIDs {
		if index > 0 && roleID == lockedRoleIDs[index-1] {
			continue
		}
		var lockedID string
		if err := tx.QueryRowContext(ctx, `SELECT id::text FROM auth_roles WHERE id = $1::uuid FOR UPDATE`, roleID).Scan(&lockedID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.AccessUser{}, application.ErrRoleNotFound
			}
			return domain.AccessUser{}, err
		}
	}
	if actorID != "" {
		if err := lockUsersTx(ctx, tx, actorID, userID); err != nil {
			return domain.AccessUser{}, err
		}
	}
	var currentVersion int64
	var targetDisabled bool
	if err := tx.QueryRowContext(ctx, `SELECT auth_version, disabled_at IS NOT NULL FROM auth_users WHERE id = $1::uuid FOR UPDATE`, userID).Scan(&currentVersion, &targetDisabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.AccessUser{}, application.ErrUserNotFound
		}
		return domain.AccessUser{}, err
	}
	if currentVersion != authVersion {
		return domain.AccessUser{}, application.ErrStaleRevision
	}

	var oldRoles []string
	oldRoles, err = roleIDsForUserTx(ctx, tx, userID)
	if err != nil {
		return domain.AccessUser{}, err
	}
	sort.Strings(oldRoles)
	newRoles := append([]string(nil), roleIDs...)
	sort.Strings(newRoles)
	changed := strings.Join(oldRoles, "\x00") != strings.Join(newRoles, "\x00")

	currentSuper := containsString(oldRoles, superRoleID)
	newHasSuper := containsString(roleIDs, superRoleID)
	if actorID != "" {
		actorSuper, actorPermissions, actorErr := effectivePermissionsTx(ctx, tx, actorID, superRoleID)
		if actorErr != nil {
			return domain.AccessUser{}, actorErr
		}
		if !actorSuper && (!actorPermissions[domain.PermissionUsersWrite] || !actorPermissions[domain.PermissionRolesRead]) {
			return domain.AccessUser{}, application.ErrForbidden
		}
		if !actorSuper {
			for _, roleID := range changedRoleIDs(oldRoles, newRoles) {
				systemKey, permissions, roleErr := rolePermissionsTx(ctx, tx, roleID)
				if roleErr != nil {
					return domain.AccessUser{}, roleErr
				}
				if systemKey != "" {
					return domain.AccessUser{}, application.ErrPermissionScope
				}
				for _, permission := range permissions {
					if !actorPermissions[permission] {
						return domain.AccessUser{}, application.ErrPermissionScope
					}
				}
			}
		}
	}
	if currentSuper && !newHasSuper && !targetDisabled {
		var holders int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM auth_user_roles ur JOIN auth_users u ON u.id=ur.user_id WHERE ur.role_id = $1::uuid AND u.disabled_at IS NULL`, superRoleID).Scan(&holders); err != nil {
			return domain.AccessUser{}, err
		}
		if holders <= 1 {
			return domain.AccessUser{}, application.ErrLastSuperAdmin
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_user_roles WHERE user_id = $1::uuid`, userID); err != nil {
		return domain.AccessUser{}, err
	}
	for _, roleID := range roleIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO auth_user_roles (user_id, role_id) VALUES ($1::uuid, $2::uuid)`, userID, roleID); err != nil {
			return domain.AccessUser{}, err
		}
	}
	if changed {
		if _, err := tx.ExecContext(ctx, `UPDATE auth_users SET auth_version = auth_version + 1 WHERE id = $1::uuid`, userID); err != nil {
			return domain.AccessUser{}, err
		}
	}
	var user domain.AccessUser
	if err := tx.QueryRowContext(ctx, `SELECT id::text, name, email, created_at, auth_version, disabled_at IS NOT NULL, COALESCE(locale, ''), EXISTS(SELECT 1 FROM auth_user_avatars a WHERE a.user_id = auth_users.id), COALESCE((SELECT version FROM auth_user_avatars a WHERE a.user_id = auth_users.id), 0) FROM auth_users WHERE id = $1::uuid`, userID).Scan(&user.User.ID, &user.User.Name, &user.User.Email, &user.User.CreatedAt, &user.AuthVersion, &user.User.Disabled, &user.User.Locale, &user.User.HasAvatar, &user.User.AvatarVersion); err != nil {
		return domain.AccessUser{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT r.id::text, COALESCE(r.system_key, ''), r.name, r.description, r.revision, COALESCE(rp.permission_key, '') FROM auth_user_roles ur JOIN auth_roles r ON r.id = ur.role_id LEFT JOIN auth_role_permissions rp ON rp.role_id = r.id WHERE ur.user_id = $1::uuid ORDER BY (r.system_key IS NULL), r.name_canonical, r.id, rp.permission_key`, userID)
	if err != nil {
		return domain.AccessUser{}, err
	}
	for rows.Next() {
		var role domain.Role
		var permission string
		if err := rows.Scan(&role.ID, &role.SystemKey, &role.Name, &role.Description, &role.Revision, &permission); err != nil {
			rows.Close()
			return domain.AccessUser{}, err
		}
		if len(user.Roles) == 0 || user.Roles[len(user.Roles)-1].ID != role.ID {
			user.Roles = append(user.Roles, role)
		}
		if permission != "" {
			index := len(user.Roles) - 1
			user.Roles[index].Permissions = append(user.Roles[index].Permissions, domain.PermissionKey(permission))
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return domain.AccessUser{}, err
	}
	rows.Close()
	if err := tx.Commit(); err != nil {
		return domain.AccessUser{}, err
	}
	return user, nil
}

func roleIDsForUserTx(ctx context.Context, tx *sql.Tx, userID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT role_id::text FROM auth_user_roles WHERE user_id = $1::uuid`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var roleID string
		if err := rows.Scan(&roleID); err != nil {
			return nil, err
		}
		result = append(result, roleID)
	}
	return result, rows.Err()
}

func lockUsersTx(ctx context.Context, tx *sql.Tx, userIDs ...string) error {
	ordered := append([]string(nil), userIDs...)
	sort.Strings(ordered)
	for index, userID := range ordered {
		if index > 0 && userID == ordered[index-1] {
			continue
		}
		var lockedID string
		if err := tx.QueryRowContext(ctx, `SELECT id::text FROM auth_users WHERE id = $1::uuid FOR UPDATE`, userID).Scan(&lockedID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return application.ErrUserNotFound
			}
			return err
		}
	}
	return nil
}

func lockActorForRoleMutation(ctx context.Context, tx *sql.Tx, actorID string, additionalRoleIDs ...string) (bool, map[domain.PermissionKey]bool, error) {
	var superRoleID string
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM auth_roles WHERE system_key = 'super_admin'`).Scan(&superRoleID); err != nil {
		return false, nil, err
	}
	roleIDs, err := roleIDsForUserTx(ctx, tx, actorID)
	if err != nil {
		return false, nil, err
	}
	roleIDs = append(roleIDs, additionalRoleIDs...)
	roleIDs = append(roleIDs, superRoleID)
	if err := lockRoles(ctx, tx, roleIDs); err != nil {
		return false, nil, err
	}
	if err := lockUsersTx(ctx, tx, actorID); err != nil {
		return false, nil, err
	}
	return effectivePermissionsTx(ctx, tx, actorID, superRoleID)
}

func permissionSetContains(granted map[domain.PermissionKey]bool, required []domain.PermissionKey) bool {
	for _, permission := range required {
		if !granted[permission] {
			return false
		}
	}
	return true
}

func effectivePermissionsTx(ctx context.Context, tx *sql.Tx, userID, superRoleID string) (bool, map[domain.PermissionKey]bool, error) {
	var enabled bool
	if err := tx.QueryRowContext(ctx, `SELECT disabled_at IS NULL FROM auth_users WHERE id=$1::uuid`, userID).Scan(&enabled); err != nil {
		return false, nil, err
	}
	if !enabled {
		return false, nil, application.ErrForbidden
	}
	var superAdmin bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM auth_user_roles WHERE user_id = $1::uuid AND role_id = $2::uuid)`, userID, superRoleID).Scan(&superAdmin); err != nil {
		return false, nil, err
	}
	permissions := make(map[domain.PermissionKey]bool)
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT rp.permission_key FROM auth_user_roles ur JOIN auth_role_permissions rp ON rp.role_id = ur.role_id WHERE ur.user_id = $1::uuid`, userID)
	if err != nil {
		return false, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return false, nil, err
		}
		permissions[domain.PermissionKey(permission)] = true
	}
	return superAdmin, permissions, rows.Err()
}

func rolePermissionsTx(ctx context.Context, tx *sql.Tx, roleID string) (string, []domain.PermissionKey, error) {
	var systemKey sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT system_key FROM auth_roles WHERE id = $1::uuid`, roleID).Scan(&systemKey); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, application.ErrRoleNotFound
		}
		return "", nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT permission_key FROM auth_role_permissions WHERE role_id = $1::uuid`, roleID)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	var permissions []domain.PermissionKey
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return "", nil, err
		}
		permissions = append(permissions, domain.PermissionKey(permission))
	}
	return systemKey.String, permissions, rows.Err()
}

func changedRoleIDs(oldRoles, newRoles []string) []string {
	oldSet := make(map[string]struct{}, len(oldRoles))
	newSet := make(map[string]struct{}, len(newRoles))
	for _, roleID := range oldRoles {
		oldSet[roleID] = struct{}{}
	}
	for _, roleID := range newRoles {
		newSet[roleID] = struct{}{}
	}
	changed := make([]string, 0, len(oldSet)+len(newSet))
	for roleID := range oldSet {
		if _, exists := newSet[roleID]; !exists {
			changed = append(changed, roleID)
		}
	}
	for roleID := range newSet {
		if _, exists := oldSet[roleID]; !exists {
			changed = append(changed, roleID)
		}
	}
	sort.Strings(changed)
	return changed
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *Store) CreateInvitation(ctx context.Context, createdBy, name, email string, locale domain.Locale, roleIDs []string, selector, digest []byte, ttl time.Duration) (domain.Invitation, error) {
	return s.createInvitation(ctx, "", createdBy, name, email, locale, roleIDs, selector, digest, ttl)
}

func (s *Store) CreateInvitationWithinScope(ctx context.Context, actorID, name, email string, locale domain.Locale, roleIDs []string, selector, digest []byte, ttl time.Duration) (domain.Invitation, error) {
	return s.createInvitation(ctx, actorID, actorID, name, email, locale, roleIDs, selector, digest, ttl)
}

func (s *Store) createInvitation(ctx context.Context, actorID, createdBy, name, email string, locale domain.Locale, roleIDs []string, selector, digest []byte, ttl time.Duration) (domain.Invitation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Invitation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	// The active-account and pending-invitation tables have independent unique
	// indexes. Serialize this email across both tables so a concurrent
	// invitation and activation cannot commit an active account alongside a
	// pending invitation for the same canonical address.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(lower($1), 0))`, email); err != nil {
		return domain.Invitation{}, err
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM auth_users WHERE email_canonical = lower($1))`, email).Scan(&active); err != nil {
		return domain.Invitation{}, err
	}
	if active {
		return domain.Invitation{}, application.ErrEmailAlreadyRegistered
	}
	var pending bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM auth_user_invitations WHERE email_canonical = lower($1))`, email).Scan(&pending); err != nil {
		return domain.Invitation{}, err
	}
	if pending {
		return domain.Invitation{}, application.ErrInvitationPending
	}
	if actorID != "" {
		actorSuper, actorPermissions, scopeErr := lockActorForRoleMutation(ctx, tx, actorID, roleIDs...)
		if scopeErr != nil {
			return domain.Invitation{}, scopeErr
		}
		if !actorSuper && (!actorPermissions[domain.PermissionInvitationsWrite] || !actorPermissions[domain.PermissionRolesRead]) {
			return domain.Invitation{}, application.ErrForbidden
		}
		if !actorSuper {
			for _, roleID := range roleIDs {
				systemKey, permissions, roleErr := rolePermissionsTx(ctx, tx, roleID)
				if roleErr != nil {
					return domain.Invitation{}, roleErr
				}
				if systemKey != "" || !permissionSetContains(actorPermissions, permissions) {
					return domain.Invitation{}, application.ErrInvitationRoleForbidden
				}
			}
		}
	} else if err := lockRoles(ctx, tx, roleIDs); err != nil {
		return domain.Invitation{}, err
	}
	var invitation domain.Invitation
	err = tx.QueryRowContext(ctx, `
		INSERT INTO auth_user_invitations (name, email, email_canonical, selector, verifier_digest, locale, expires_at, created_by, created_by_name, created_by_email, created_by_user_key)
		SELECT $1, $2, lower($2), $3, $4, $5, clock_timestamp() + ($6 * INTERVAL '1 second'), u.id, u.name, u.email, u.id::text FROM auth_users u WHERE u.id=$7::uuid
		RETURNING id::text, name, email, locale, expires_at, created_at, revision, COALESCE(created_by::text, created_by_user_key, ''), created_by_name, created_by_email, created_by IS NULL`, name, email, selector, digest, string(locale), ttl.Seconds(), createdBy).
		Scan(&invitation.ID, &invitation.Name, &invitation.Email, &invitation.Locale, &invitation.ExpiresAt, &invitation.CreatedAt, &invitation.Revision, &invitation.CreatedBy, &invitation.CreatedByName, &invitation.CreatedByEmail, &invitation.CreatorDeleted)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Invitation{}, application.ErrInvitationPending
		}
		return domain.Invitation{}, err
	}
	for _, roleID := range roleIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO auth_invitation_roles (invitation_id, role_id) VALUES ($1::uuid, $2::uuid)`, invitation.ID, roleID); err != nil {
			return domain.Invitation{}, err
		}
	}
	systemName, err := mailTaskSystemName(ctx, tx)
	if err != nil {
		return domain.Invitation{}, err
	}
	material, err := s.sealMailTaskMaterial(application.MailTaskMaterial{Version: 1, Kind: application.MailUserInvitation, Name: invitation.Name, Email: invitation.Email, Locale: locale, SystemName: systemName, CreatedAt: invitation.CreatedAt, ExpiresAt: invitation.ExpiresAt, ResetSelector: selector, VerifierDigest: digest})
	if err != nil {
		return domain.Invitation{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO auth_mail_outbox (kind, invitation_id, reset_selector, recipient_email, recipient_name, locale, system_name, material_ciphertext, expires_at, created_at) VALUES ('user_invitation', $1::uuid, $2, $3, $4, $5, $6, $7, $8, $9)`, invitation.ID, selector, invitation.Email, invitation.Name, string(locale), systemName, nullableBytes(material), invitation.ExpiresAt, invitation.CreatedAt); err != nil {
		return domain.Invitation{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Invitation{}, err
	}
	return s.populateInvitationRoles(ctx, invitation)
}

// lockRoles serializes operations which may create or remove a role reference.
// All callers acquire role locks in UUID order before locking their owning row,
// which keeps role deletion, assignment replacement, and invitation creation
// on one lock order.
func lockRoles(ctx context.Context, tx *sql.Tx, roleIDs []string) error {
	ordered := append([]string(nil), roleIDs...)
	sort.Strings(ordered)
	for _, roleID := range ordered {
		var lockedID string
		if err := tx.QueryRowContext(ctx, `SELECT id::text FROM auth_roles WHERE id = $1::uuid FOR UPDATE`, roleID).Scan(&lockedID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return application.ErrRoleNotFound
			}
			return err
		}
	}
	return nil
}

func (s *Store) ListInvitations(ctx context.Context, cursor string, limit int) (application.InvitationPage, error) {
	return s.listInvitationsWithOptions(ctx, application.AccessListOptions{Cursor: cursor, Limit: limit, Sort: "createdAt", Direction: "desc"}, true)
}

func (s *Store) FindInvitation(ctx context.Context, id string) (domain.Invitation, error) {
	var invitation domain.Invitation
	if err := s.db.QueryRowContext(ctx, `
		SELECT id::text, name, email, locale, expires_at, created_at, revision, COALESCE(created_by::text, created_by_user_key, ''), created_by_name, created_by_email, created_by IS NULL
		FROM auth_user_invitations
		WHERE id = $1::uuid`, id).
		Scan(&invitation.ID, &invitation.Name, &invitation.Email, &invitation.Locale, &invitation.ExpiresAt, &invitation.CreatedAt, &invitation.Revision, &invitation.CreatedBy, &invitation.CreatedByName, &invitation.CreatedByEmail, &invitation.CreatorDeleted); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Invitation{}, application.ErrInvitationNotFound
		}
		return domain.Invitation{}, err
	}
	return s.populateInvitationRoles(ctx, invitation)
}

func (s *Store) ListInvitationsWithOptions(ctx context.Context, options application.AccessListOptions) (application.InvitationPage, error) {
	return s.listInvitationsWithOptions(ctx, options, false)
}

func (s *Store) listInvitationsWithOptions(ctx context.Context, options application.AccessListOptions, forceLegacy bool) (application.InvitationPage, error) {
	options = storeListOptions(options, true)
	cursor, legacy, err := storeCursor(options, true)
	if err != nil {
		return application.InvitationPage{}, err
	}
	legacy = legacy || (forceLegacy && (options.Cursor == "" || domain.IsCanonicalUUID(options.Cursor)))
	args := []any{likePattern(options.Query)}
	where := []string{"($1 = '' OR lower(i.name) LIKE '%' || $1 || '%' ESCAPE '\\' OR lower(i.email) LIKE '%' || $1 || '%' ESCAPE '\\')"}
	if options.RoleID != "" {
		args = append(args, options.RoleID)
		where = append(where, "EXISTS (SELECT 1 FROM auth_invitation_roles AS ir_filter WHERE ir_filter.invitation_id = i.id AND ir_filter.role_id = $"+strconv.Itoa(len(args))+"::uuid)")
	}
	if options.Status == "pending" {
		where = append(where, "i.expires_at > clock_timestamp()")
	} else if options.Status == "expired" {
		where = append(where, "i.expires_at <= clock_timestamp()")
	}
	if cursor.ID != "" {
		if legacy {
			args = append(args, cursor.ID)
			where = append(where, "i.id > $"+strconv.Itoa(len(args))+"::uuid")
		} else {
			boundary, boundaryArgs, boundaryErr := cursorBoundary("i", cursor, options.Sort, options.Direction, len(args))
			if boundaryErr != nil {
				return application.InvitationPage{}, boundaryErr
			}
			where = append(where, boundary)
			args = append(args, boundaryArgs...)
		}
	}
	args = append(args, options.Limit+1)
	order := accessOrder("i", options.Sort, options.Direction)
	if legacy {
		// UUID cursors were issued by the previous ID-ascending endpoint. Keep
		// that sequence self-consistent for clients finishing an old page.
		order = "i.id ASC"
	}
	query := fmt.Sprintf(`
		WITH selected_invitations AS (
			SELECT i.id, i.name, i.email, i.email_canonical, i.locale, i.expires_at, i.created_at, i.revision, i.created_by, i.created_by_user_key, i.created_by_name, i.created_by_email
			FROM auth_user_invitations AS i
			WHERE %s
			ORDER BY %s
			LIMIT $%d
		)
		SELECT i.id::text, i.name, i.email, i.locale, i.expires_at, i.created_at, i.revision, COALESCE(i.created_by::text, i.created_by_user_key, ''),i.created_by_name,i.created_by_email,i.created_by IS NULL,
		       COALESCE(r.id::text, ''), COALESCE(r.system_key, ''), COALESCE(r.name, ''), COALESCE(r.description, ''), COALESCE(r.revision, 0),
		       COALESCE(rp.permission_key, '')
		FROM selected_invitations AS i
		LEFT JOIN auth_invitation_roles AS ir ON ir.invitation_id = i.id
		LEFT JOIN auth_roles AS r ON r.id = ir.role_id
		LEFT JOIN auth_role_permissions AS rp ON rp.role_id = r.id
		ORDER BY %s, (r.system_key IS NULL), r.name_canonical, r.id, rp.permission_key`, strings.Join(where, " AND "), order, len(args), order)
	page, err := scanInvitations(ctx, s.db, query, args...)
	if err != nil {
		return application.InvitationPage{}, err
	}
	if len(page.Items) > options.Limit {
		last := page.Items[options.Limit-1]
		if legacy {
			page.NextCursor = last.ID
		} else {
			page.NextCursor, err = encodeInvitationCursor(last, options)
			if err != nil {
				return application.InvitationPage{}, err
			}
		}
		page.Items = page.Items[:options.Limit]
	}
	return page, nil
}

func scanInvitations(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, query string, args ...any) (application.InvitationPage, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return application.InvitationPage{}, err
	}
	defer rows.Close()
	invitations := map[string]*domain.Invitation{}
	order := []string{}
	for rows.Next() {
		var invitation domain.Invitation
		var role domain.Role
		var permission string
		if err := rows.Scan(&invitation.ID, &invitation.Name, &invitation.Email, &invitation.Locale, &invitation.ExpiresAt, &invitation.CreatedAt, &invitation.Revision, &invitation.CreatedBy, &invitation.CreatedByName, &invitation.CreatedByEmail, &invitation.CreatorDeleted, &role.ID, &role.SystemKey, &role.Name, &role.Description, &role.Revision, &permission); err != nil {
			return application.InvitationPage{}, err
		}
		current := invitations[invitation.ID]
		if current == nil {
			current = &invitation
			invitations[invitation.ID] = current
			order = append(order, invitation.ID)
		}
		if role.ID == "" {
			continue
		}
		if len(current.Roles) == 0 || current.Roles[len(current.Roles)-1].ID != role.ID {
			current.Roles = append(current.Roles, role)
		}
		if permission != "" {
			index := len(current.Roles) - 1
			current.Roles[index].Permissions = append(current.Roles[index].Permissions, domain.PermissionKey(permission))
		}
	}
	if err := rows.Err(); err != nil {
		return application.InvitationPage{}, err
	}
	page := application.InvitationPage{}
	for _, id := range order {
		page.Items = append(page.Items, *invitations[id])
	}
	return page, nil
}

func storeListOptions(options application.AccessListOptions, invitations bool) application.AccessListOptions {
	options.Query = strings.TrimSpace(options.Query)
	if options.Limit <= 0 {
		options.Limit = application.DefaultAccessPageSize
	}
	if options.Sort == "" {
		options.Sort = "createdAt"
	}
	if options.Direction != "asc" && options.Direction != "desc" {
		options.Direction = "desc"
	}
	if !invitations && options.Sort == "expiresAt" {
		options.Sort = "createdAt"
	}
	if options.Sort != "name" && options.Sort != "email" && options.Sort != "roles" && options.Sort != "createdAt" && options.Sort != "expiresAt" {
		options.Sort = "createdAt"
	}
	if invitations && options.Sort == "roles" {
		options.Sort = "createdAt"
	}
	return options
}

func storeCursor(options application.AccessListOptions, invitations bool) (application.AccessCursor, bool, error) {
	if options.Cursor == "" {
		return application.AccessCursor{}, false, nil
	}
	if domain.IsCanonicalUUID(options.Cursor) {
		if options.Query != "" || options.RoleID != "" || options.Status != "" || options.Sort != "createdAt" || options.Direction != "desc" {
			return application.AccessCursor{}, false, application.ErrInvalidCursor
		}
		return application.AccessCursor{Version: 1, ID: options.Cursor, Query: options.Query, Sort: options.Sort, Direction: options.Direction}, true, nil
	}
	cursor, err := application.DecodeAccessCursor(options.Cursor)
	if err != nil || cursor.Query != options.Query || cursor.RoleID != options.RoleID || cursor.Status != options.Status || cursor.Sort != options.Sort || cursor.Direction != options.Direction {
		return application.AccessCursor{}, false, application.ErrInvalidCursor
	}
	if !invitations && cursor.Sort == "expiresAt" {
		return application.AccessCursor{}, false, application.ErrInvalidCursor
	}
	return cursor, false, nil
}

func accessOrder(alias, sortKey, direction string) string {
	expression := alias + ".created_at"
	switch sortKey {
	case "name":
		expression = "lower(" + alias + ".name)"
	case "email":
		expression = alias + ".email_canonical"
	case "roles":
		expression = assignedRoleOrderExpression(alias)
	case "expiresAt":
		expression = alias + ".expires_at"
	default:
		expression = alias + ".created_at"
	}
	if direction != "asc" {
		direction = "desc"
	}
	return expression + " " + direction + ", " + alias + ".id " + direction
}

func cursorBoundary(alias string, cursor application.AccessCursor, sortKey, direction string, argumentOffset int) (string, []any, error) {
	expression := ""
	value := any(cursor.Value)
	switch sortKey {
	case "name":
		expression = "lower(" + alias + ".name)"
	case "email":
		expression = alias + ".email_canonical"
	case "roles":
		expression = assignedRoleOrderExpression(alias)
	case "createdAt":
		expression = alias + ".created_at"
	case "expiresAt":
		expression = alias + ".expires_at"
		parsed, err := time.Parse(time.RFC3339Nano, cursor.Value)
		if err != nil {
			return "", nil, application.ErrInvalidCursor
		}
		value = parsed
	default:
		return "", nil, application.ErrInvalidCursor
	}
	operator := "<"
	if direction == "asc" {
		operator = ">"
	}
	valuePlaceholder := "$" + strconv.Itoa(argumentOffset+1)
	idPlaceholder := "$" + strconv.Itoa(argumentOffset+2)
	return fmt.Sprintf("(%s %s %s OR (%s = %s AND %s %s %s::uuid))", expression, operator, valuePlaceholder, expression, valuePlaceholder, alias+".id", operator, idPlaceholder), []any{value, cursor.ID}, nil
}

func likePattern(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(value))
}

func assignedRoleOrderExpression(alias string) string {
	return fmt.Sprintf(`COALESCE((SELECT string_agg(r_sort.name_canonical, ', ' ORDER BY (r_sort.system_key IS NULL), r_sort.name_canonical, r_sort.id)
		FROM auth_user_roles AS ur_sort
		JOIN auth_roles AS r_sort ON r_sort.id = ur_sort.role_id
		WHERE ur_sort.user_id = %s.id), '')`, alias)
}

func encodeUserCursor(user domain.AccessUser, options application.AccessListOptions) (string, error) {
	value := ""
	switch options.Sort {
	case "name":
		value = strings.ToLower(user.User.Name)
	case "email":
		value = strings.ToLower(user.User.Email)
	case "roles":
		value = userRoleSortKey(user)
	default:
		value = user.User.CreatedAt.Format(time.RFC3339Nano)
	}
	return application.EncodeAccessCursor(application.AccessCursor{Version: 1, Query: options.Query, RoleID: options.RoleID, Status: options.Status, Sort: options.Sort, Direction: options.Direction, Value: value, ID: user.User.ID})
}

func userRoleSortKey(user domain.AccessUser) string {
	roles := append([]domain.Role(nil), user.Roles...)
	sort.Slice(roles, func(i, j int) bool {
		if roles[i].IsSystem() != roles[j].IsSystem() {
			return roles[i].IsSystem()
		}
		left := domain.CanonicalRoleName(roles[i].Name)
		right := domain.CanonicalRoleName(roles[j].Name)
		if left != right {
			return left < right
		}
		return roles[i].ID < roles[j].ID
	})
	names := make([]string, 0, len(roles))
	for _, role := range roles {
		names = append(names, domain.CanonicalRoleName(role.Name))
	}
	return strings.Join(names, ", ")
}

func encodeInvitationCursor(invitation domain.Invitation, options application.AccessListOptions) (string, error) {
	value := ""
	switch options.Sort {
	case "name":
		value = strings.ToLower(invitation.Name)
	case "email":
		value = strings.ToLower(invitation.Email)
	case "expiresAt":
		value = invitation.ExpiresAt.Format(time.RFC3339Nano)
	default:
		value = invitation.CreatedAt.Format(time.RFC3339Nano)
	}
	return application.EncodeAccessCursor(application.AccessCursor{Version: 1, Query: options.Query, RoleID: options.RoleID, Status: options.Status, Sort: options.Sort, Direction: options.Direction, Value: value, ID: invitation.ID})
}

func (s *Store) populateInvitationRoles(ctx context.Context, invitation domain.Invitation) (domain.Invitation, error) {
	return populateInvitationRolesFromQuery(ctx, s.db, invitation)
}

type queryContext interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func populateInvitationRolesTx(ctx context.Context, tx *sql.Tx, invitation domain.Invitation) (domain.Invitation, error) {
	return populateInvitationRolesFromQuery(ctx, tx, invitation)
}

func populateInvitationRolesFromQuery(ctx context.Context, queryer queryContext, invitation domain.Invitation) (domain.Invitation, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT r.id::text, COALESCE(r.system_key, ''), r.name, r.description, r.revision, COALESCE(rp.permission_key, '') FROM auth_invitation_roles ir JOIN auth_roles r ON r.id = ir.role_id LEFT JOIN auth_role_permissions rp ON rp.role_id = r.id WHERE ir.invitation_id = $1::uuid ORDER BY (r.system_key IS NULL), r.name_canonical, r.id, rp.permission_key`, invitation.ID)
	if err != nil {
		return domain.Invitation{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var role domain.Role
		var permission string
		if err := rows.Scan(&role.ID, &role.SystemKey, &role.Name, &role.Description, &role.Revision, &permission); err != nil {
			return domain.Invitation{}, err
		}
		if len(invitation.Roles) == 0 || invitation.Roles[len(invitation.Roles)-1].ID != role.ID {
			invitation.Roles = append(invitation.Roles, role)
		}
		if permission != "" {
			index := len(invitation.Roles) - 1
			invitation.Roles[index].Permissions = append(invitation.Roles[index].Permissions, domain.PermissionKey(permission))
		}
	}
	return invitation, rows.Err()
}

func (s *Store) ResendInvitation(ctx context.Context, id string, locale domain.Locale, selector, digest []byte, ttl time.Duration) (domain.Invitation, error) {
	_, invitation, err := s.resendInvitation(ctx, "", id, locale, selector, digest, ttl)
	return invitation, err
}

func (s *Store) ResendInvitationWithinScope(ctx context.Context, actorID, id string, locale domain.Locale, selector, digest []byte, ttl time.Duration) (domain.Invitation, error) {
	_, invitation, err := s.resendInvitation(ctx, actorID, id, locale, selector, digest, ttl)
	return invitation, err
}

func (s *Store) ResendInvitationWithSnapshot(ctx context.Context, actorID, id string, locale domain.Locale, selector, digest []byte, ttl time.Duration) (domain.Invitation, domain.Invitation, error) {
	return s.resendInvitation(ctx, actorID, id, locale, selector, digest, ttl)
}

func (s *Store) resendInvitation(ctx context.Context, actorID, id string, locale domain.Locale, selector, digest []byte, ttl time.Duration) (domain.Invitation, domain.Invitation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Invitation{}, domain.Invitation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	invitation, roleIDs, actorSuper, actorPermissions, err := lockInvitationMutationScope(ctx, tx, actorID, id)
	if err != nil {
		return domain.Invitation{}, domain.Invitation{}, err
	}
	before, err := populateInvitationRolesTx(ctx, tx, invitation)
	if err != nil {
		return domain.Invitation{}, domain.Invitation{}, err
	}
	if actorID != "" && !actorSuper {
		if !actorPermissions[domain.PermissionInvitationsWrite] || !actorPermissions[domain.PermissionRolesRead] {
			return domain.Invitation{}, domain.Invitation{}, application.ErrForbidden
		}
		if err := authorizeInvitationRoleIDsTx(ctx, tx, actorPermissions, roleIDs); err != nil {
			return domain.Invitation{}, domain.Invitation{}, err
		}
	}
	// Keep materialized historical tasks; only legacy rows need supersession.
	if _, err := tx.ExecContext(ctx, `UPDATE auth_mail_outbox SET canceled_at = clock_timestamp(), finished_at = clock_timestamp(), lease_token = NULL, lease_expires_at = NULL, last_error_code = 'superseded' WHERE invitation_id = $1::uuid AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL AND material_ciphertext IS NULL`, id); err != nil {
		return domain.Invitation{}, domain.Invitation{}, err
	}
	if err := tx.QueryRowContext(ctx, `UPDATE auth_user_invitations SET selector = $2, verifier_digest = $3, locale = $4, expires_at = clock_timestamp() + ($5 * INTERVAL '1 second'), revision = revision + 1, updated_at = clock_timestamp() WHERE id = $1::uuid RETURNING expires_at, updated_at, revision`, id, selector, digest, string(locale), ttl.Seconds()).Scan(&invitation.ExpiresAt, &invitation.UpdatedAt, &invitation.Revision); err != nil {
		return domain.Invitation{}, domain.Invitation{}, err
	}
	invitation.Locale = locale
	invitation.Roles = before.Roles
	systemName, err := mailTaskSystemName(ctx, tx)
	if err != nil {
		return domain.Invitation{}, domain.Invitation{}, err
	}
	material, err := s.sealMailTaskMaterial(application.MailTaskMaterial{Version: 1, Kind: application.MailUserInvitation, Name: invitation.Name, Email: invitation.Email, Locale: locale, SystemName: systemName, CreatedAt: invitation.UpdatedAt, ExpiresAt: invitation.ExpiresAt, ResetSelector: selector, VerifierDigest: digest})
	if err != nil {
		return domain.Invitation{}, domain.Invitation{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO auth_mail_outbox (kind, invitation_id, reset_selector, recipient_email, recipient_name, locale, system_name, material_ciphertext, expires_at, created_at) VALUES ('user_invitation', $1::uuid, $2, $3, $4, $5, $6, $7, $8, $9)`, id, selector, invitation.Email, invitation.Name, string(locale), systemName, nullableBytes(material), invitation.ExpiresAt, invitation.UpdatedAt); err != nil {
		return domain.Invitation{}, domain.Invitation{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Invitation{}, domain.Invitation{}, err
	}
	return before, invitation, nil
}

func (s *Store) RevokeInvitation(ctx context.Context, id string) error {
	_, err := s.revokeInvitation(ctx, "", id)
	return err
}

func (s *Store) RevokeInvitationWithinScope(ctx context.Context, actorID, id string) error {
	_, err := s.revokeInvitation(ctx, actorID, id)
	return err
}

func (s *Store) RevokeInvitationWithSnapshot(ctx context.Context, actorID, id string) (domain.Invitation, error) {
	return s.revokeInvitation(ctx, actorID, id)
}

func (s *Store) revokeInvitation(ctx context.Context, actorID, id string) (domain.Invitation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Invitation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	invit, roleIDs, actorSuper, actorPermissions, err := lockInvitationMutationScope(ctx, tx, actorID, id)
	if err != nil {
		return domain.Invitation{}, err
	}
	before, err := populateInvitationRolesTx(ctx, tx, invit)
	if err != nil {
		return domain.Invitation{}, err
	}
	if actorID != "" && !actorSuper {
		if !actorPermissions[domain.PermissionInvitationsWrite] || !actorPermissions[domain.PermissionRolesRead] {
			return domain.Invitation{}, application.ErrForbidden
		}
		if err := authorizeInvitationRoleIDsTx(ctx, tx, actorPermissions, roleIDs); err != nil {
			return domain.Invitation{}, err
		}
	}
	// Revoke the legacy queue row only; durable material is independent of the
	// invitation authority and remains available for management retry.
	if _, err := tx.ExecContext(ctx, `UPDATE auth_mail_outbox SET canceled_at = clock_timestamp(), finished_at = clock_timestamp(), lease_token = NULL, lease_expires_at = NULL, last_error_code = 'superseded' WHERE invitation_id = $1::uuid AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL AND material_ciphertext IS NULL`, id); err != nil {
		return domain.Invitation{}, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM auth_user_invitations WHERE id = $1::uuid`, id)
	if err != nil {
		return domain.Invitation{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return domain.Invitation{}, err
	}
	if count == 0 {
		return domain.Invitation{}, application.ErrInvitationNotFound
	}
	if err := tx.Commit(); err != nil {
		return domain.Invitation{}, err
	}
	return before, nil
}

// lockInvitationMutationScope establishes the same lock order used by role
// and assignment mutations: the email advisory lock, selected role rows, the
// actor's role and user rows, then the invitation row. Re-reading the
// invitation after the locks makes the authorization decision reflect the
// state that will be changed by this transaction.
func lockInvitationMutationScope(ctx context.Context, tx *sql.Tx, actorID, invitationID string) (domain.Invitation, []string, bool, map[domain.PermissionKey]bool, error) {
	var emailCanonical string
	if err := tx.QueryRowContext(ctx, `SELECT email_canonical FROM auth_user_invitations WHERE id = $1::uuid`, invitationID).Scan(&emailCanonical); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Invitation{}, nil, false, nil, application.ErrInvitationNotFound
		}
		return domain.Invitation{}, nil, false, nil, err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, emailCanonical); err != nil {
		return domain.Invitation{}, nil, false, nil, err
	}
	roleIDs, err := invitationRoleIDsTx(ctx, tx, invitationID)
	if err != nil {
		return domain.Invitation{}, nil, false, nil, err
	}
	var actorSuper bool
	var actorPermissions map[domain.PermissionKey]bool
	if actorID != "" {
		actorSuper, actorPermissions, err = lockActorForRoleMutation(ctx, tx, actorID, roleIDs...)
	} else {
		err = lockRoles(ctx, tx, roleIDs)
	}
	if err != nil {
		return domain.Invitation{}, nil, false, nil, err
	}
	var invitation domain.Invitation
	if err := tx.QueryRowContext(ctx, `SELECT id::text, name, email, locale, expires_at, created_at, revision, COALESCE(created_by::text, created_by_user_key, ''), created_by_name, created_by_email, created_by IS NULL FROM auth_user_invitations WHERE id = $1::uuid FOR UPDATE`, invitationID).Scan(&invitation.ID, &invitation.Name, &invitation.Email, &invitation.Locale, &invitation.ExpiresAt, &invitation.CreatedAt, &invitation.Revision, &invitation.CreatedBy, &invitation.CreatedByName, &invitation.CreatedByEmail, &invitation.CreatorDeleted); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Invitation{}, nil, false, nil, application.ErrInvitationNotFound
		}
		return domain.Invitation{}, nil, false, nil, err
	}
	roleIDs, err = invitationRoleIDsTx(ctx, tx, invitationID)
	if err != nil {
		return domain.Invitation{}, nil, false, nil, err
	}
	return invitation, roleIDs, actorSuper, actorPermissions, nil
}

func invitationRoleIDsTx(ctx context.Context, tx *sql.Tx, invitationID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT role_id::text FROM auth_invitation_roles WHERE invitation_id = $1::uuid`, invitationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roleIDs []string
	for rows.Next() {
		var roleID string
		if err := rows.Scan(&roleID); err != nil {
			return nil, err
		}
		roleIDs = append(roleIDs, roleID)
	}
	return roleIDs, rows.Err()
}

func authorizeInvitationRoleIDsTx(ctx context.Context, tx *sql.Tx, actorPermissions map[domain.PermissionKey]bool, roleIDs []string) error {
	for _, roleID := range roleIDs {
		systemKey, permissions, err := rolePermissionsTx(ctx, tx, roleID)
		if err != nil {
			return err
		}
		if systemKey != "" || !permissionSetContains(actorPermissions, permissions) {
			return application.ErrInvitationNotManageable
		}
	}
	return nil
}

func (s *Store) PreflightInvitation(ctx context.Context, selector, digest []byte) error {
	var stored []byte
	var valid bool
	err := s.db.QueryRowContext(ctx, `SELECT verifier_digest, expires_at > clock_timestamp() FROM auth_user_invitations WHERE selector = $1`, selector).Scan(&stored, &valid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrInvitationInvalid
		}
		return err
	}
	if !valid || !equalDigest(stored, digest) {
		return application.ErrInvitationInvalid
	}
	return nil
}

func (s *Store) FindInvitationTarget(ctx context.Context, selector, digest []byte) (domain.Invitation, error) {
	var invitation domain.Invitation
	err := s.db.QueryRowContext(ctx, `
		SELECT id::text, name, email, locale, expires_at, created_at, updated_at, revision, COALESCE(created_by::text,created_by_user_key,''),created_by_name,created_by_email,created_by IS NULL
		FROM auth_user_invitations
		WHERE selector = $1 AND verifier_digest = $2 AND expires_at > clock_timestamp()`, selector, digest).
		Scan(&invitation.ID, &invitation.Name, &invitation.Email, &invitation.Locale, &invitation.ExpiresAt, &invitation.CreatedAt, &invitation.UpdatedAt, &invitation.Revision, &invitation.CreatedBy, &invitation.CreatedByName, &invitation.CreatedByEmail, &invitation.CreatorDeleted)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Invitation{}, application.ErrInvitationInvalid
	}
	return invitation, err
}

func (s *Store) CompleteInvitation(ctx context.Context, selector, digest []byte, passwordHash string) error {
	return s.CompleteInvitationWithLocale(ctx, selector, digest, passwordHash, domain.LocaleEnglish)
}

func (s *Store) CompleteInvitationWithLocale(ctx context.Context, selector, digest []byte, passwordHash string, locale domain.Locale) error {
	if !locale.Valid() {
		locale = domain.LocaleEnglish
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var invitationID, name, email, canonical string
	var stored []byte
	var valid bool
	if err := tx.QueryRowContext(ctx, `SELECT id::text, name, email, email_canonical FROM auth_user_invitations WHERE selector = $1`, selector).Scan(&invitationID, &name, &email, &canonical); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrInvitationInvalid
		}
		return err
	}
	// Match CreateInvitation's transaction-level email lock before checking
	// the invitation and creating the activated account. This closes the
	// cross-table uniqueness race without storing a second email index.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, canonical); err != nil {
		return err
	}
	roleRows, err := tx.QueryContext(ctx, `SELECT role_id::text FROM auth_invitation_roles WHERE invitation_id = $1::uuid`, invitationID)
	if err != nil {
		return err
	}
	var roleIDs []string
	for roleRows.Next() {
		var roleID string
		if err := roleRows.Scan(&roleID); err != nil {
			roleRows.Close()
			return err
		}
		roleIDs = append(roleIDs, roleID)
	}
	if err := roleRows.Err(); err != nil {
		roleRows.Close()
		return err
	}
	roleRows.Close()
	if err := lockRoles(ctx, tx, roleIDs); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT verifier_digest, expires_at > clock_timestamp() FROM auth_user_invitations WHERE id = $1::uuid FOR UPDATE`, invitationID).Scan(&stored, &valid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrInvitationInvalid
		}
		return err
	}
	if !valid || !equalDigest(stored, digest) {
		return application.ErrInvitationInvalid
	}
	// Consume the invitation generation at the same transaction boundary as
	// the account creation. Materialized task rows remain historical copies.
	if _, err := tx.ExecContext(ctx, `UPDATE auth_mail_outbox SET canceled_at = clock_timestamp(), finished_at = clock_timestamp(), lease_token = NULL, lease_expires_at = NULL, last_error_code = 'superseded' WHERE invitation_id = $1::uuid AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL AND material_ciphertext IS NULL`, invitationID); err != nil {
		return err
	}
	var userID string
	if err := tx.QueryRowContext(ctx, `INSERT INTO auth_users (name, email, email_canonical, password_hash, locale) VALUES ($1, $2, $3, $4, $5) RETURNING id::text`, name, email, canonical, passwordHash, locale).Scan(&userID); err != nil {
		if isUniqueViolation(err) {
			return application.ErrInvitationInvalid
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO auth_user_roles (user_id, role_id) SELECT $1::uuid, role_id FROM auth_invitation_roles WHERE invitation_id = $2::uuid`, userID, invitationID); err != nil {
		return err
	}
	var assigned int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM auth_user_roles WHERE user_id = $1::uuid`, userID).Scan(&assigned); err != nil {
		return err
	}
	if assigned == 0 {
		return application.ErrInvitationInvalid
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_user_invitations WHERE id = $1::uuid`, invitationID); err != nil {
		return err
	}
	return tx.Commit()
}
