package application

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	"golang.org/x/text/unicode/norm"
)

type RolePage struct {
	Items        []domain.Role
	Catalog      []domain.PermissionDefinition
	Combinations []domain.PermissionCombination
}

type UserPage struct {
	Items      []domain.AccessUser
	NextCursor string
}

type InvitationPage struct {
	Items      []domain.Invitation
	NextCursor string
}

type RoleOption struct {
	ID   string
	Name string
}

type RoleMutationInput struct {
	Name        string
	Description string
	Permissions []domain.PermissionKey
	Revision    int64
}

type AssignmentInput struct {
	RoleIDs     []string
	AuthVersion int64
}

type InvitationInput struct {
	Name    string
	Email   string
	RoleIDs []string
}

// AccessStore contains state-changing operations for the auth capability. A
// concrete PostgreSQL adapter implements this alongside the authentication and
// mail-outbox ports, while the application layer only sees domain values.
type VersionedUserLifecycleStore interface {
	DeactivateUserWithRevision(context.Context, string, string, int64) (domain.AccessUser, error)
	ReactivateUserWithRevision(context.Context, string, string, int64) (domain.AccessUser, error)
	DeleteUserWithRevision(context.Context, string, string, int64) error
}

type AccessStore interface {
	ListRoles(context.Context) ([]domain.Role, error)
	ListRoleOptions(context.Context) ([]RoleOption, error)
	FindRole(context.Context, string) (domain.Role, error)
	CreateRole(context.Context, string, string, []domain.PermissionKey) (domain.Role, error)
	ReplaceRole(context.Context, string, int64, string, string, []domain.PermissionKey) (domain.Role, error)
	DeleteRole(context.Context, string) error
	ListUsers(context.Context, string, int) (UserPage, error)
	ReplaceUserRoles(context.Context, string, int64, []string) (domain.AccessUser, error)
	CreateInvitation(context.Context, string, string, string, domain.Locale, []string, []byte, []byte, time.Duration) (domain.Invitation, error)
	ListInvitations(context.Context, string, int) (InvitationPage, error)
	FindInvitation(context.Context, string) (domain.Invitation, error)
	ResendInvitation(context.Context, string, domain.Locale, []byte, []byte, time.Duration) (domain.Invitation, error)
	RevokeInvitation(context.Context, string) error
	PreflightInvitation(context.Context, []byte, []byte) error
	CompleteInvitation(context.Context, []byte, []byte, string) error
}

// ScopedUserRoleStore performs an assignment replacement while rechecking the
// actor's authority and the changed role set inside the same database
// transaction that writes the assignment. Stores which do not implement this
// stronger seam retain the legacy application-level checks for compatibility.
type ScopedUserRoleStore interface {
	ReplaceUserRolesWithinScope(context.Context, string, string, int64, []string) (domain.AccessUser, error)
}

// ScopedRoleStore performs role mutations with the actor and target role
// authorization checks held through the database commit boundary.
type ScopedRoleStore interface {
	CreateRoleWithinScope(context.Context, string, string, string, []domain.PermissionKey) (domain.Role, error)
	ReplaceRoleWithinScope(context.Context, string, string, int64, string, string, []domain.PermissionKey) (domain.Role, error)
	DeleteRoleWithinScope(context.Context, string, string) error
}

// ScopedInvitationStore rechecks the sender's authority and selected role
// permissions in the invitation transaction before creating any durable mail
// task.
type ScopedInvitationStore interface {
	CreateInvitationWithinScope(context.Context, string, string, string, domain.Locale, []string, []byte, []byte, time.Duration) (domain.Invitation, error)
	ResendInvitationWithinScope(context.Context, string, string, domain.Locale, []byte, []byte, time.Duration) (domain.Invitation, error)
	RevokeInvitationWithinScope(context.Context, string, string) error
}

// QueryableAccessStore is implemented by stores that support the searchable,
// sortable access listings. The legacy list methods remain in AccessStore so
// adapters can be upgraded without breaking simpler test and embedding stores.
type QueryableAccessStore interface {
	ListUsersWithOptions(context.Context, AccessListOptions) (UserPage, error)
	ListInvitationsWithOptions(context.Context, AccessListOptions) (InvitationPage, error)
}

// AccessSnapshotStore is an optional read seam used only to capture the
// target state that an access mutation is about to replace. It is deliberately
// separate from AccessStore so existing embedders and test stores remain
// source-compatible.
type AccessSnapshotStore interface {
	FindAccessUser(context.Context, string) (domain.AccessUser, error)
}

// OperationAuditAccessService exposes safe, permission-checked snapshots for
// the HTTP audit boundary. Implementations must return business fields only;
// credentials and invitation tokens are never part of a snapshot.
type OperationAuditAccessService interface {
	SnapshotRole(context.Context, string, string) (domain.Role, error)
	SnapshotUser(context.Context, string, string) (domain.AccessUser, error)
	SnapshotInvitation(context.Context, string, string) (domain.Invitation, error)
}

// OperationAuditMutationService returns the state observed under the same
// row locks as a mutation. HTTP records the returned snapshot after the
// business transaction commits, so an audit write never joins that business
// transaction while still describing the exact row that changed.
type OperationAuditMutationService interface {
	DeleteRoleWithSnapshot(context.Context, string, string) (domain.Role, error)
	ResendInvitationWithSnapshot(context.Context, string, string) (domain.Invitation, domain.Invitation, error)
	RevokeInvitationWithSnapshot(context.Context, string, string) (domain.Invitation, error)
}

type OperationAuditMutationStore interface {
	DeleteRoleWithSnapshot(context.Context, string, string) (domain.Role, error)
	ResendInvitationWithSnapshot(context.Context, string, string, domain.Locale, []byte, []byte, time.Duration) (domain.Invitation, domain.Invitation, error)
	RevokeInvitationWithSnapshot(context.Context, string, string) (domain.Invitation, error)
}

type AccessManagement struct {
	store         AccessStore
	principals    PrincipalStore
	catalog       domain.PermissionCatalog
	invitationKey []byte
	random        RandomSource
	invitationTTL time.Duration
	mailSettings  MailSettingsProvider
	mailLimiter   InvitationSendLimiter
}

func NewAccessManagement(store AccessStore, principals PrincipalStore, catalog domain.PermissionCatalog) *AccessManagement {
	if len(catalog.Definitions()) == 0 {
		catalog = domain.DefaultPermissionCatalog()
	}
	return &AccessManagement{store: store, principals: principals, catalog: catalog}
}

func NewAccessManagementWithInvitations(store AccessStore, principals PrincipalStore, catalog domain.PermissionCatalog, key []byte, random RandomSource, ttl time.Duration, mailSettings ...MailSettingsProvider) *AccessManagement {
	manager := NewAccessManagement(store, principals, catalog)
	manager.invitationKey = append([]byte(nil), key...)
	manager.random = random
	manager.invitationTTL = ttl
	if len(mailSettings) > 0 {
		manager.mailSettings = mailSettings[0]
	}
	return manager
}

// SetInvitationSendLimiter wires the authenticated invitation-send policy
// without changing the constructor used by existing embedders.
func (m *AccessManagement) SetInvitationSendLimiter(limiter InvitationSendLimiter) {
	if m != nil {
		m.mailLimiter = limiter
	}
}

func (m *AccessManagement) SnapshotRole(ctx context.Context, actorID, roleID string) (domain.Role, error) {
	return m.Role(ctx, actorID, roleID)
}

func (m *AccessManagement) SnapshotUser(ctx context.Context, actorID, userID string) (domain.AccessUser, error) {
	if _, err := m.principal(ctx, actorID); err != nil {
		return domain.AccessUser{}, err
	}
	snapshotStore, ok := m.store.(AccessSnapshotStore)
	if !ok {
		return domain.AccessUser{}, ErrDependencyUnavailable
	}
	user, err := snapshotStore.FindAccessUser(ctx, userID)
	if err != nil {
		return domain.AccessUser{}, normalizeAccessError(err)
	}
	return normalizeAccessUser(m.catalog, user)
}

func (m *AccessManagement) SnapshotInvitation(ctx context.Context, actorID, invitationID string) (domain.Invitation, error) {
	if _, err := m.principal(ctx, actorID); err != nil {
		return domain.Invitation{}, err
	}
	invitation, err := m.store.FindInvitation(ctx, invitationID)
	if err != nil {
		return domain.Invitation{}, normalizeAccessError(err)
	}
	return normalizeInvitation(m.catalog, invitation)
}

func (m *AccessManagement) principal(ctx context.Context, actorID string) (domain.Principal, error) {
	if m.principals == nil || actorID == "" {
		return domain.Principal{}, ErrDependencyUnavailable
	}
	p, err := m.principals.FindPrincipalByID(ctx, actorID)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			return domain.Principal{}, ErrUnauthenticated
		}
		return domain.Principal{}, dependencyError(err)
	}
	if err := ensurePrincipalIdentity(actorID, p); err != nil {
		return domain.Principal{}, err
	}
	return normalizePrincipal(m.catalog, p)
}

func (m *AccessManagement) require(ctx context.Context, actorID string, permission domain.PermissionKey) error {
	if !m.catalog.Has(permission) {
		// Routes must only authorize against the live, code-owned catalog. This
		// keeps an accidentally persisted/unknown grant from becoming an
		// implicit capability.
		return ErrForbidden
	}
	p, err := m.principal(ctx, actorID)
	if err != nil {
		return err
	}
	if !p.Has(permission) {
		return ErrForbidden
	}
	return nil
}

func (m *AccessManagement) requireSuper(ctx context.Context, actorID string) error {
	p, err := m.principal(ctx, actorID)
	if err != nil {
		return err
	}
	if !p.SuperAdmin {
		return ErrForbidden
	}
	return nil
}

func (m *AccessManagement) Roles(ctx context.Context, actorID string) (RolePage, error) {
	if err := m.require(ctx, actorID, domain.PermissionRolesRead); err != nil {
		return RolePage{}, err
	}
	roles, err := m.store.ListRoles(ctx)
	if err != nil {
		return RolePage{}, dependencyError(err)
	}
	for i := range roles {
		roles[i], err = normalizeRole(m.catalog, roles[i])
		if err != nil {
			return RolePage{}, err
		}
	}
	return RolePage{Items: roles, Catalog: m.catalog.Definitions(), Combinations: m.catalog.Combinations()}, nil
}

func (m *AccessManagement) RoleOptions(ctx context.Context, actorID string) ([]RoleOption, error) {
	principal, err := m.principal(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if !principal.SuperAdmin && !principal.Has(domain.PermissionRolesRead) {
		return nil, ErrForbidden
	}
	options, err := m.store.ListRoleOptions(ctx)
	if err != nil {
		return nil, dependencyError(err)
	}
	return options, nil
}

func (m *AccessManagement) Role(ctx context.Context, actorID, roleID string) (domain.Role, error) {
	if err := m.require(ctx, actorID, domain.PermissionRolesRead); err != nil {
		return domain.Role{}, err
	}
	role, err := m.store.FindRole(ctx, roleID)
	if err != nil {
		return domain.Role{}, normalizeAccessError(err)
	}
	role, err = normalizeRole(m.catalog, role)
	if err != nil {
		return domain.Role{}, err
	}
	return role, nil
}

func (m *AccessManagement) CreateRole(ctx context.Context, actorID string, input RoleMutationInput) (domain.Role, error) {
	principal, err := m.principal(ctx, actorID)
	if err != nil {
		return domain.Role{}, err
	}
	if !principal.SuperAdmin && (!principal.Has(domain.PermissionRolesWrite) || !principal.Has(domain.PermissionRolesRead)) {
		return domain.Role{}, ErrForbidden
	}
	name, description, permissions, err := validateRoleInput(m.catalog, input)
	if err != nil {
		return domain.Role{}, err
	}
	if err := m.ensureGrantable(principal, permissions); err != nil {
		return domain.Role{}, err
	}
	var role domain.Role
	if scoped, ok := m.store.(ScopedRoleStore); ok {
		role, err = scoped.CreateRoleWithinScope(ctx, actorID, name, description, permissions)
	} else {
		role, err = m.store.CreateRole(ctx, name, description, permissions)
	}
	if err != nil {
		return domain.Role{}, normalizeAccessError(err)
	}
	return role, nil
}

func (m *AccessManagement) ReplaceRole(ctx context.Context, actorID, roleID string, input RoleMutationInput) (domain.Role, error) {
	principal, err := m.principal(ctx, actorID)
	if err != nil {
		return domain.Role{}, err
	}
	if !principal.SuperAdmin && (!principal.Has(domain.PermissionRolesWrite) || !principal.Has(domain.PermissionRolesRead)) {
		return domain.Role{}, ErrForbidden
	}
	existing, err := m.store.FindRole(ctx, roleID)
	if err != nil {
		return domain.Role{}, normalizeAccessError(err)
	}
	if existing.IsSystem() {
		return domain.Role{}, ErrImmutableRole
	}
	if err := m.ensureGrantable(principal, existing.Permissions); err != nil {
		return domain.Role{}, ErrPermissionScope
	}
	name, description, permissions, err := validateRoleInput(m.catalog, input)
	if err != nil {
		return domain.Role{}, err
	}
	if err := m.ensureGrantable(principal, permissions); err != nil {
		return domain.Role{}, err
	}
	var role domain.Role
	if scoped, ok := m.store.(ScopedRoleStore); ok {
		role, err = scoped.ReplaceRoleWithinScope(ctx, actorID, roleID, input.Revision, name, description, permissions)
	} else {
		role, err = m.store.ReplaceRole(ctx, roleID, input.Revision, name, description, permissions)
	}
	if err != nil {
		return domain.Role{}, normalizeAccessError(err)
	}
	return role, nil
}

func (m *AccessManagement) DeleteRole(ctx context.Context, actorID, roleID string) error {
	principal, err := m.principal(ctx, actorID)
	if err != nil {
		return err
	}
	if !principal.SuperAdmin && (!principal.Has(domain.PermissionRolesWrite) || !principal.Has(domain.PermissionRolesRead)) {
		return ErrForbidden
	}
	role, err := m.store.FindRole(ctx, roleID)
	if err != nil {
		return normalizeAccessError(err)
	}
	if role.IsSystem() {
		return ErrImmutableRole
	}
	if err := m.ensureGrantable(principal, role.Permissions); err != nil {
		return err
	}
	var deleteErr error
	if scoped, ok := m.store.(ScopedRoleStore); ok {
		deleteErr = scoped.DeleteRoleWithinScope(ctx, actorID, roleID)
	} else {
		deleteErr = m.store.DeleteRole(ctx, roleID)
	}
	if deleteErr != nil {
		return normalizeAccessError(deleteErr)
	}
	return nil
}

// DeleteRoleWithSnapshot is the audit-aware delete path. PostgreSQL returns
// the locked row from the same transaction that removes it; compatibility
// stores keep the legacy delete semantics and return the preflight value.
func (m *AccessManagement) DeleteRoleWithSnapshot(ctx context.Context, actorID, roleID string) (domain.Role, error) {
	principal, err := m.principal(ctx, actorID)
	if err != nil {
		return domain.Role{}, err
	}
	if !principal.SuperAdmin && (!principal.Has(domain.PermissionRolesWrite) || !principal.Has(domain.PermissionRolesRead)) {
		return domain.Role{}, ErrForbidden
	}
	role, err := m.store.FindRole(ctx, roleID)
	if err != nil {
		return domain.Role{}, normalizeAccessError(err)
	}
	if role.IsSystem() {
		return domain.Role{}, ErrImmutableRole
	}
	if err := m.ensureGrantable(principal, role.Permissions); err != nil {
		return domain.Role{}, err
	}
	if audited, ok := m.store.(OperationAuditMutationStore); ok {
		before, deleteErr := audited.DeleteRoleWithSnapshot(ctx, actorID, roleID)
		if deleteErr != nil {
			return domain.Role{}, normalizeAccessError(deleteErr)
		}
		return normalizeRole(m.catalog, before)
	}
	if scoped, ok := m.store.(ScopedRoleStore); ok {
		err = scoped.DeleteRoleWithinScope(ctx, actorID, roleID)
	} else {
		err = m.store.DeleteRole(ctx, roleID)
	}
	if err != nil {
		return domain.Role{}, normalizeAccessError(err)
	}
	return normalizeRole(m.catalog, role)
}

func (m *AccessManagement) Users(ctx context.Context, actorID, cursor string, limit int) (UserPage, error) {
	if err := m.require(ctx, actorID, domain.PermissionUsersRead); err != nil {
		return UserPage{}, err
	}
	if err := validatePage(cursor, limit); err != nil {
		return UserPage{}, err
	}
	if limit == 0 {
		limit = DefaultAccessPageSize
	}
	page, err := m.store.ListUsers(ctx, cursor, limit)
	if err != nil {
		return UserPage{}, dependencyError(err)
	}
	return m.normalizeUsersPage(page)
}

// UsersWithOptions returns users using a validated query, sort, direction,
// and opaque cursor. Users keeps the original method for existing callers.
func (m *AccessManagement) UsersWithOptions(ctx context.Context, actorID string, options AccessListOptions) (UserPage, error) {
	if err := m.require(ctx, actorID, domain.PermissionUsersRead); err != nil {
		return UserPage{}, err
	}
	normalized, err := normalizeAccessListOptions(options, false)
	if err != nil {
		return UserPage{}, err
	}
	var page UserPage
	if queryable, ok := m.store.(QueryableAccessStore); ok {
		page, err = queryable.ListUsersWithOptions(ctx, normalized)
	} else {
		page, err = m.store.ListUsers(ctx, normalized.Cursor, normalized.Limit)
	}
	if err != nil {
		if errors.Is(err, ErrInvalidCursor) {
			return UserPage{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "cursor", Code: "invalid_cursor"}}}
		}
		return UserPage{}, dependencyError(err)
	}
	return m.normalizeUsersPage(page)
}

func (m *AccessManagement) normalizeUsersPage(page UserPage) (UserPage, error) {
	for i := range page.Items {
		if len(page.Items[i].Roles) == 0 {
			return UserPage{}, ErrDependencyUnavailable
		}
		for j := range page.Items[i].Roles {
			role, roleErr := normalizeRole(m.catalog, page.Items[i].Roles[j])
			if roleErr != nil {
				return UserPage{}, roleErr
			}
			page.Items[i].Roles[j] = role
		}
	}
	return page, nil
}

func (m *AccessManagement) ReplaceUserRoles(ctx context.Context, actorID, userID string, input AssignmentInput) (domain.AccessUser, error) {
	principal, err := m.principal(ctx, actorID)
	if err != nil {
		return domain.AccessUser{}, err
	}
	if !principal.SuperAdmin && (!principal.Has(domain.PermissionUsersWrite) || !principal.Has(domain.PermissionRolesRead)) {
		return domain.AccessUser{}, ErrForbidden
	}
	if err := validateRoleIDs(input.RoleIDs); err != nil {
		return domain.AccessUser{}, err
	}
	var user domain.AccessUser
	if scoped, ok := m.store.(ScopedUserRoleStore); ok {
		user, err = scoped.ReplaceUserRolesWithinScope(ctx, actorID, userID, input.AuthVersion, input.RoleIDs)
	} else {
		if err := m.authorizeInvitationRoles(ctx, principal, input.RoleIDs); err != nil {
			return domain.AccessUser{}, err
		}
		user, err = m.store.ReplaceUserRoles(ctx, userID, input.AuthVersion, input.RoleIDs)
	}
	if err != nil {
		return domain.AccessUser{}, normalizeAccessError(err)
	}
	return normalizeAccessUser(m.catalog, user)
}

func (m *AccessManagement) CreateInvitation(ctx context.Context, actorID string, input InvitationInput) (domain.Invitation, error) {
	principal, err := m.invitationManager(ctx, actorID)
	if err != nil {
		return domain.Invitation{}, err
	}
	if !principal.SuperAdmin && !principal.Has(domain.PermissionRolesRead) {
		return domain.Invitation{}, ErrForbidden
	}
	if m.random == nil || len(m.invitationKey) != 32 || m.invitationTTL <= 0 {
		return domain.Invitation{}, ErrDependencyUnavailable
	}
	name, err := domain.NewName(input.Name)
	if err != nil {
		return domain.Invitation{}, err
	}
	email, err := domain.NewEmail(input.Email)
	if err != nil {
		return domain.Invitation{}, err
	}
	if err := validateRoleIDs(input.RoleIDs); err != nil {
		return domain.Invitation{}, err
	}
	if err := m.authorizeInvitationRoles(ctx, principal, input.RoleIDs); err != nil {
		return domain.Invitation{}, err
	}
	if err := m.allowInvitationSend(ctx, principal.User.ID, email.Canonical); err != nil {
		return domain.Invitation{}, err
	}
	locale, err := m.defaultMailLocale(ctx)
	if err != nil {
		return domain.Invitation{}, err
	}
	selector := make([]byte, 16)
	if err := m.random.Read(selector); err != nil {
		return domain.Invitation{}, dependencyError(err)
	}
	material, err := domain.NewInvitationMaterial(m.invitationKey, selector)
	if err != nil {
		return domain.Invitation{}, dependencyError(err)
	}
	var invitation domain.Invitation
	if scoped, ok := m.store.(ScopedInvitationStore); ok {
		invitation, err = scoped.CreateInvitationWithinScope(ctx, actorID, string(name), email.Display, locale, input.RoleIDs, material.Selector, material.VerifierDigest, m.invitationTTL)
	} else {
		invitation, err = m.store.CreateInvitation(ctx, actorID, string(name), email.Display, locale, input.RoleIDs, material.Selector, material.VerifierDigest, m.invitationTTL)
	}
	if err != nil {
		return domain.Invitation{}, normalizeAccessError(err)
	}
	return normalizeInvitation(m.catalog, invitation)
}

func (m *AccessManagement) Invitations(ctx context.Context, actorID, cursor string, limit int) (InvitationPage, error) {
	if err := m.require(ctx, actorID, domain.PermissionInvitationsRead); err != nil {
		return InvitationPage{}, err
	}
	if err := validatePage(cursor, limit); err != nil {
		return InvitationPage{}, err
	}
	if limit == 0 {
		limit = DefaultAccessPageSize
	}
	page, err := m.store.ListInvitations(ctx, cursor, limit)
	if err != nil {
		return InvitationPage{}, dependencyError(err)
	}
	return m.normalizeInvitationsPage(page)
}

func (m *AccessManagement) InvitationsWithOptions(ctx context.Context, actorID string, options AccessListOptions) (InvitationPage, error) {
	if err := m.require(ctx, actorID, domain.PermissionInvitationsRead); err != nil {
		return InvitationPage{}, err
	}
	normalized, err := normalizeAccessListOptions(options, true)
	if err != nil {
		return InvitationPage{}, err
	}
	var page InvitationPage
	if queryable, ok := m.store.(QueryableAccessStore); ok {
		page, err = queryable.ListInvitationsWithOptions(ctx, normalized)
	} else {
		page, err = m.store.ListInvitations(ctx, normalized.Cursor, normalized.Limit)
	}
	if err != nil {
		if errors.Is(err, ErrInvalidCursor) {
			return InvitationPage{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "cursor", Code: "invalid_cursor"}}}
		}
		return InvitationPage{}, dependencyError(err)
	}
	return m.normalizeInvitationsPage(page)
}

func (m *AccessManagement) normalizeInvitationsPage(page InvitationPage) (InvitationPage, error) {
	for i := range page.Items {
		if len(page.Items[i].Roles) == 0 {
			return InvitationPage{}, ErrDependencyUnavailable
		}
		for j := range page.Items[i].Roles {
			role, roleErr := normalizeRole(m.catalog, page.Items[i].Roles[j])
			if roleErr != nil {
				return InvitationPage{}, roleErr
			}
			page.Items[i].Roles[j] = role
		}
	}
	return page, nil
}

func (m *AccessManagement) ResendInvitation(ctx context.Context, actorID, invitationID string) (domain.Invitation, error) {
	principal, err := m.invitationManager(ctx, actorID)
	if err != nil {
		return domain.Invitation{}, err
	}
	if err := m.authorizeExistingInvitation(ctx, principal, invitationID); err != nil {
		return domain.Invitation{}, err
	}
	if m.random == nil || len(m.invitationKey) != 32 || m.invitationTTL <= 0 {
		return domain.Invitation{}, ErrDependencyUnavailable
	}
	existingInvitation, err := m.store.FindInvitation(ctx, invitationID)
	if err != nil {
		return domain.Invitation{}, normalizeAccessError(err)
	}
	email, err := domain.NewEmail(existingInvitation.Email)
	if err != nil {
		return domain.Invitation{}, ErrDependencyUnavailable
	}
	if err := m.allowInvitationSend(ctx, principal.User.ID, email.Canonical); err != nil {
		return domain.Invitation{}, err
	}
	locale, err := m.defaultMailLocale(ctx)
	if err != nil {
		return domain.Invitation{}, err
	}
	selector := make([]byte, 16)
	if err := m.random.Read(selector); err != nil {
		return domain.Invitation{}, dependencyError(err)
	}
	material, err := domain.NewInvitationMaterial(m.invitationKey, selector)
	if err != nil {
		return domain.Invitation{}, dependencyError(err)
	}
	var invitation domain.Invitation
	if scoped, ok := m.store.(ScopedInvitationStore); ok {
		invitation, err = scoped.ResendInvitationWithinScope(ctx, actorID, invitationID, locale, material.Selector, material.VerifierDigest, m.invitationTTL)
	} else {
		invitation, err = m.store.ResendInvitation(ctx, invitationID, locale, material.Selector, material.VerifierDigest, m.invitationTTL)
	}
	if err != nil {
		return domain.Invitation{}, normalizeAccessError(err)
	}
	return normalizeInvitation(m.catalog, invitation)
}

// ResendInvitationWithSnapshot keeps the invitation and its role descriptors
// from the mutation transaction, avoiding an HTTP-side read racing a resend.
func (m *AccessManagement) ResendInvitationWithSnapshot(ctx context.Context, actorID, invitationID string) (domain.Invitation, domain.Invitation, error) {
	principal, err := m.invitationManager(ctx, actorID)
	if err != nil {
		return domain.Invitation{}, domain.Invitation{}, err
	}
	if err := m.authorizeExistingInvitation(ctx, principal, invitationID); err != nil {
		return domain.Invitation{}, domain.Invitation{}, err
	}
	if m.random == nil || len(m.invitationKey) != 32 || m.invitationTTL <= 0 {
		return domain.Invitation{}, domain.Invitation{}, ErrDependencyUnavailable
	}
	existingInvitation, err := m.store.FindInvitation(ctx, invitationID)
	if err != nil {
		return domain.Invitation{}, domain.Invitation{}, normalizeAccessError(err)
	}
	email, err := domain.NewEmail(existingInvitation.Email)
	if err != nil {
		return domain.Invitation{}, domain.Invitation{}, ErrDependencyUnavailable
	}
	if err := m.allowInvitationSend(ctx, principal.User.ID, email.Canonical); err != nil {
		return domain.Invitation{}, domain.Invitation{}, err
	}
	locale, err := m.defaultMailLocale(ctx)
	if err != nil {
		return domain.Invitation{}, domain.Invitation{}, err
	}
	selector := make([]byte, 16)
	if err := m.random.Read(selector); err != nil {
		return domain.Invitation{}, domain.Invitation{}, dependencyError(err)
	}
	material, err := domain.NewInvitationMaterial(m.invitationKey, selector)
	if err != nil {
		return domain.Invitation{}, domain.Invitation{}, dependencyError(err)
	}
	if audited, ok := m.store.(OperationAuditMutationStore); ok {
		before, after, resendErr := audited.ResendInvitationWithSnapshot(ctx, actorID, invitationID, locale, material.Selector, material.VerifierDigest, m.invitationTTL)
		if resendErr != nil {
			return domain.Invitation{}, domain.Invitation{}, normalizeAccessError(resendErr)
		}
		before, err = normalizeInvitation(m.catalog, before)
		if err != nil {
			return domain.Invitation{}, domain.Invitation{}, err
		}
		after, err = normalizeInvitation(m.catalog, after)
		if err != nil {
			return domain.Invitation{}, domain.Invitation{}, err
		}
		return before, after, nil
	}
	before, beforeErr := m.store.FindInvitation(ctx, invitationID)
	if beforeErr != nil {
		return domain.Invitation{}, domain.Invitation{}, normalizeAccessError(beforeErr)
	}
	var after domain.Invitation
	if scoped, ok := m.store.(ScopedInvitationStore); ok {
		after, err = scoped.ResendInvitationWithinScope(ctx, actorID, invitationID, locale, material.Selector, material.VerifierDigest, m.invitationTTL)
	} else {
		after, err = m.store.ResendInvitation(ctx, invitationID, locale, material.Selector, material.VerifierDigest, m.invitationTTL)
	}
	if err != nil {
		return domain.Invitation{}, domain.Invitation{}, normalizeAccessError(err)
	}
	before, err = normalizeInvitation(m.catalog, before)
	if err != nil {
		return domain.Invitation{}, domain.Invitation{}, err
	}
	after, err = normalizeInvitation(m.catalog, after)
	if err != nil {
		return domain.Invitation{}, domain.Invitation{}, err
	}
	return before, after, nil
}

func (m *AccessManagement) RevokeInvitation(ctx context.Context, actorID, invitationID string) error {
	principal, err := m.invitationManager(ctx, actorID)
	if err != nil {
		return err
	}
	if err := m.authorizeExistingInvitation(ctx, principal, invitationID); err != nil {
		return err
	}
	var revokeErr error
	if scoped, ok := m.store.(ScopedInvitationStore); ok {
		revokeErr = scoped.RevokeInvitationWithinScope(ctx, actorID, invitationID)
	} else {
		revokeErr = m.store.RevokeInvitation(ctx, invitationID)
	}
	if revokeErr != nil {
		return normalizeAccessError(revokeErr)
	}
	return nil
}

// RevokeInvitationWithSnapshot returns the locked invitation before deletion.
func (m *AccessManagement) RevokeInvitationWithSnapshot(ctx context.Context, actorID, invitationID string) (domain.Invitation, error) {
	principal, err := m.invitationManager(ctx, actorID)
	if err != nil {
		return domain.Invitation{}, err
	}
	if err := m.authorizeExistingInvitation(ctx, principal, invitationID); err != nil {
		return domain.Invitation{}, err
	}
	if audited, ok := m.store.(OperationAuditMutationStore); ok {
		before, revokeErr := audited.RevokeInvitationWithSnapshot(ctx, actorID, invitationID)
		if revokeErr != nil {
			return domain.Invitation{}, normalizeAccessError(revokeErr)
		}
		return normalizeInvitation(m.catalog, before)
	}
	before, beforeErr := m.store.FindInvitation(ctx, invitationID)
	if beforeErr != nil {
		return domain.Invitation{}, normalizeAccessError(beforeErr)
	}
	if scoped, ok := m.store.(ScopedInvitationStore); ok {
		err = scoped.RevokeInvitationWithinScope(ctx, actorID, invitationID)
	} else {
		err = m.store.RevokeInvitation(ctx, invitationID)
	}
	if err != nil {
		return domain.Invitation{}, normalizeAccessError(err)
	}
	return normalizeInvitation(m.catalog, before)
}

func (m *AccessManagement) invitationManager(ctx context.Context, actorID string) (domain.Principal, error) {
	principal, err := m.principal(ctx, actorID)
	if err != nil {
		return domain.Principal{}, err
	}
	if !principal.SuperAdmin && !principal.Has(domain.PermissionInvitationsWrite) {
		return domain.Principal{}, ErrForbidden
	}
	return principal, nil
}

func (m *AccessManagement) allowInvitationSend(ctx context.Context, actorID, canonicalEmail string) error {
	if m.mailLimiter == nil {
		return nil
	}
	allowed, err := m.mailLimiter.AllowInvitationSend(ctx, actorID, canonicalEmail)
	if err != nil {
		return dependencyError(err)
	}
	if !allowed {
		return ErrRateLimited
	}
	return nil
}

func (m *AccessManagement) defaultMailLocale(ctx context.Context) (domain.Locale, error) {
	if m.mailSettings != nil {
		if err := m.mailSettings.EnsureMailConfigured(ctx); err != nil {
			return "", err
		}
		locale, err := m.mailSettings.DefaultMailLocale(ctx)
		if err != nil {
			return "", err
		}
		return locale, nil
	}
	// Small embedders and isolated tests may not wire system settings. The
	// production server always supplies the provider and requires an explicit
	// first configuration before creating a mail task.
	return domain.LocaleEnglish, nil
}

func (m *AccessManagement) ensureGrantable(principal domain.Principal, permissions []domain.PermissionKey) error {
	if principal.SuperAdmin {
		return nil
	}
	for _, permission := range permissions {
		if !principal.Has(permission) {
			return ErrPermissionScope
		}
	}
	return nil
}

func (m *AccessManagement) authorizeInvitationRoles(ctx context.Context, principal domain.Principal, roleIDs []string) error {
	if principal.SuperAdmin {
		return nil
	}
	effective := make(map[domain.PermissionKey]struct{}, len(principal.Permissions))
	for _, permission := range principal.EffectivePermissions(m.catalog) {
		effective[permission] = struct{}{}
	}
	for _, roleID := range roleIDs {
		role, err := m.store.FindRole(ctx, roleID)
		if err != nil {
			return normalizeAccessError(err)
		}
		role, err = normalizeRole(m.catalog, role)
		if err != nil {
			return err
		}
		if role.IsSystem() {
			return ErrInvitationRoleForbidden
		}
		for _, permission := range role.Permissions {
			if _, ok := effective[permission]; !ok {
				return ErrInvitationRoleForbidden
			}
		}
	}
	return nil
}

func (m *AccessManagement) authorizeExistingInvitation(ctx context.Context, principal domain.Principal, invitationID string) error {
	invitation, err := m.store.FindInvitation(ctx, invitationID)
	if err != nil {
		return normalizeAccessError(err)
	}
	roleIDs := make([]string, 0, len(invitation.Roles))
	for _, role := range invitation.Roles {
		roleIDs = append(roleIDs, role.ID)
	}
	if err := m.authorizeInvitationRoles(ctx, principal, roleIDs); err != nil {
		if errors.Is(err, ErrInvitationRoleForbidden) {
			return ErrInvitationNotManageable
		}
		return err
	}
	return nil
}

type InvitationAcceptance struct {
	store   AccessStore
	hasher  PasswordHasher
	key     []byte
	limiter InvitationAcceptLimiter
}

type InvitationTargetStore interface {
	FindInvitationTarget(context.Context, []byte, []byte) (domain.Invitation, error)
}

// InvitationLocaleStore lets acceptance persist the locale captured when the
// invitation was created without changing the legacy acceptance seam.
type InvitationLocaleStore interface {
	CompleteInvitationWithLocale(context.Context, []byte, []byte, string, domain.Locale) error
}

func NewInvitationAcceptance(store AccessStore, hasher PasswordHasher, key []byte, limiters ...InvitationAcceptLimiter) *InvitationAcceptance {
	var limiter InvitationAcceptLimiter
	if len(limiters) > 0 {
		limiter = limiters[0]
	}
	return &InvitationAcceptance{store: store, hasher: hasher, key: append([]byte(nil), key...), limiter: limiter}
}

func (a *InvitationAcceptance) Complete(ctx context.Context, token, password string) error {
	_, err := a.CompleteWithTarget(ctx, token, password)
	return err
}

// CompleteWithSource is the source-aware anonymous acceptance path. The
// legacy Complete methods remain available for small embedders that do not
// expose an HTTP source identity.
func (a *InvitationAcceptance) CompleteWithSource(ctx context.Context, sourceIP, token, password string) error {
	_, err := a.completeWithTarget(ctx, sourceIP, token, password)
	return err
}

func (a *InvitationAcceptance) CompleteWithTarget(ctx context.Context, token, password string) (domain.Invitation, error) {
	return a.completeWithTarget(ctx, "", token, password)
}

func (a *InvitationAcceptance) CompleteWithTargetAndSource(ctx context.Context, sourceIP, token, password string) (domain.Invitation, error) {
	return a.completeWithTarget(ctx, sourceIP, token, password)
}

func (a *InvitationAcceptance) completeWithTarget(ctx context.Context, sourceIP, token, password string) (domain.Invitation, error) {
	if a.limiter != nil {
		allowed, err := a.limiter.AllowInvitationAccept(ctx, sourceIP)
		if err != nil {
			return domain.Invitation{}, dependencyError(err)
		}
		if !allowed {
			return domain.Invitation{}, ErrRateLimited
		}
	}
	selector, digest, ok := domain.ParseInvitationToken(token)
	if !ok {
		return domain.Invitation{}, ErrInvitationInvalid
	}
	var err error
	var target domain.Invitation
	if targetStore, ok := a.store.(InvitationTargetStore); ok {
		target, err = targetStore.FindInvitationTarget(ctx, selector, digest)
		if err != nil {
			if errors.Is(err, ErrInvitationInvalid) {
				return domain.Invitation{}, ErrInvitationInvalid
			}
			return domain.Invitation{}, dependencyError(err)
		}
	}
	if a.store == nil || a.hasher == nil || len(a.key) != 32 {
		return domain.Invitation{}, ErrDependencyUnavailable
	}
	if err := a.store.PreflightInvitation(ctx, selector, digest); err != nil {
		if errors.Is(err, ErrInvitationInvalid) {
			return domain.Invitation{}, ErrInvitationInvalid
		}
		return domain.Invitation{}, dependencyError(err)
	}
	value, err := domain.NewPassword(password)
	if err != nil {
		return domain.Invitation{}, err
	}
	hash, err := a.hasher.Hash(ctx, string(value))
	if err != nil {
		if errors.Is(err, ErrPasswordHashBusy) {
			return domain.Invitation{}, ErrDependencyUnavailable
		}
		return domain.Invitation{}, dependencyError(err)
	}
	locale := target.Locale
	if !locale.Valid() {
		locale = domain.LocaleEnglish
	}
	var completeErr error
	if localized, ok := a.store.(InvitationLocaleStore); ok {
		completeErr = localized.CompleteInvitationWithLocale(ctx, selector, digest, hash, locale)
	} else {
		completeErr = a.store.CompleteInvitation(ctx, selector, digest, hash)
	}
	if completeErr != nil {
		if errors.Is(completeErr, ErrInvitationInvalid) || errors.Is(completeErr, ErrEmailAlreadyRegistered) {
			return domain.Invitation{}, ErrInvitationInvalid
		}
		return domain.Invitation{}, dependencyError(completeErr)
	}
	return target, nil
}

func validateRoleInput(catalog domain.PermissionCatalog, input RoleMutationInput) (string, string, []domain.PermissionKey, error) {
	name, err := domain.NewName(input.Name)
	if err != nil {
		if validation, ok := err.(*domain.ValidationErrors); ok {
			validation.Items[0].Field = "name"
		}
		return "", "", nil, err
	}
	description, err := validateDescription(input.Description)
	if err != nil {
		return "", "", nil, err
	}
	permissions, err := catalog.ValidateFeatureSet(input.Permissions)
	if err != nil {
		return "", "", nil, err
	}
	if input.Revision < 0 {
		return "", "", nil, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "revision", Code: "invalid_revision"}}}
	}
	return string(name), description, permissions, nil
}

func validateDescription(value string) (string, error) {
	for _, r := range value {
		if r == '\n' || r == '\r' || r == '\t' || r < 0x20 || r == '\u2028' || r == '\u2029' {
			return "", &domain.ValidationErrors{Items: []domain.FieldError{{Field: "description", Code: "invalid_description"}}}
		}
	}
	if len([]rune(value)) > 500 {
		return "", &domain.ValidationErrors{Items: []domain.FieldError{{Field: "description", Code: "invalid_description"}}}
	}
	return value, nil
}

func validateRoleIDs(ids []string) error {
	if len(ids) == 0 {
		return ErrInvalidRoleSet
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !domain.IsCanonicalUUID(id) {
			return &domain.ValidationErrors{Items: []domain.FieldError{{Field: "roleIds", Code: "invalid_role"}}}
		}
		if _, ok := seen[id]; ok {
			return &domain.ValidationErrors{Items: []domain.FieldError{{Field: "roleIds", Code: "duplicate_role"}}}
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validatePage(cursor string, limit int) error {
	if limit == 0 {
		limit = DefaultAccessPageSize
	}
	if limit < 1 || limit > MaxAccessPageSize {
		return &domain.ValidationErrors{Items: []domain.FieldError{{Field: "limit", Code: "invalid_limit"}}}
	}
	if cursor != "" && !domain.IsCanonicalUUID(cursor) {
		decoded, err := DecodeAccessCursor(cursor)
		if err != nil || decoded.Query != "" || decoded.Sort != "createdAt" || decoded.Direction != "desc" || validateAccessCursorValue(decoded, false) != nil {
			return &domain.ValidationErrors{Items: []domain.FieldError{{Field: "cursor", Code: "invalid_cursor"}}}
		}
	}
	return nil
}

func normalizeAccessListOptions(options AccessListOptions, invitations bool) (AccessListOptions, error) {
	if options.Limit == 0 {
		options.Limit = DefaultAccessPageSize
	}
	if options.Limit < 1 || options.Limit > MaxAccessPageSize {
		return AccessListOptions{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "limit", Code: "invalid_limit"}}}
	}
	options.Query = norm.NFC.String(strings.TrimFunc(options.Query, unicode.IsSpace))
	options.RoleID = strings.TrimSpace(options.RoleID)
	if options.RoleID != "" && !domain.IsCanonicalUUID(options.RoleID) {
		return AccessListOptions{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "roleId", Code: "invalid_role"}}}
	}
	options.Status = strings.TrimSpace(options.Status)
	if !invitations && options.Status != "" && options.Status != "active" && options.Status != "disabled" {
		return AccessListOptions{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "status", Code: "invalid_value"}}}
	}
	if invitations && options.Status != "" && options.Status != "pending" && options.Status != "expired" {
		return AccessListOptions{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "status", Code: "invalid_value"}}}
	}
	if options.Sort == "" {
		options.Sort = "createdAt"
	}
	if options.Direction == "" {
		options.Direction = "desc"
	}
	allowed := map[string]struct{}{"name": {}, "email": {}, "createdAt": {}}
	if !invitations {
		allowed["roles"] = struct{}{}
	}
	if invitations {
		allowed["expiresAt"] = struct{}{}
	}
	if _, ok := allowed[options.Sort]; !ok {
		return AccessListOptions{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "sort", Code: "invalid_value"}}}
	}
	if options.Direction != "asc" && options.Direction != "desc" {
		return AccessListOptions{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "direction", Code: "invalid_value"}}}
	}
	if options.Cursor != "" {
		// Keep accepting the pre-query UUID cursor for the default listing so
		// older clients can finish a pagination sequence while the API rolls
		// out opaque, query-aware cursors.
		if domain.IsCanonicalUUID(options.Cursor) {
			if options.Query != "" || options.RoleID != "" || options.Status != "" || options.Sort != "createdAt" || options.Direction != "desc" {
				return AccessListOptions{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "cursor", Code: "invalid_cursor"}}}
			}
			return options, nil
		}
		cursor, err := DecodeAccessCursor(options.Cursor)
		if err != nil || cursor.Query != options.Query || cursor.RoleID != options.RoleID || cursor.Status != options.Status || cursor.Sort != options.Sort || cursor.Direction != options.Direction {
			return AccessListOptions{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "cursor", Code: "invalid_cursor"}}}
		}
		if err := validateAccessCursorValue(cursor, invitations); err != nil {
			return AccessListOptions{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "cursor", Code: "invalid_cursor"}}}
		}
	}
	return options, nil
}

func validateAccessCursorValue(cursor AccessCursor, invitations bool) error {
	if cursor.Value == "" {
		return ErrInvalidCursor
	}
	if cursor.Sort != "createdAt" && !(invitations && cursor.Sort == "expiresAt") {
		return nil
	}
	if _, err := time.Parse(time.RFC3339Nano, cursor.Value); err != nil {
		return ErrInvalidCursor
	}
	return nil
}

func normalizeAccessError(err error) error {
	if err == nil {
		return nil
	}
	for _, known := range []error{ErrRoleNotFound, ErrRoleAlreadyExists, ErrUserNotFound, ErrInvitationNotFound, ErrRoleInUse, ErrImmutableRole, ErrLastSuperAdmin, ErrStaleRevision, ErrInvalidRoleSet, ErrInvitationPending, ErrInvitationInvalid, ErrInvitationRoleForbidden, ErrInvitationNotManageable, ErrEmailAlreadyRegistered, ErrForbidden, ErrPermissionScope, ErrMailNotConfigured, ErrInvalidMailSettings, ErrDependencyUnavailable} {
		if errors.Is(err, known) {
			return err
		}
	}
	return dependencyError(err)
}

func (m *AccessManagement) knownPermissions(keys []domain.PermissionKey) bool {
	if len(keys) == 0 {
		return false
	}
	for _, key := range keys {
		if !m.catalog.Has(key) {
			return false
		}
	}
	return true
}

// normalizeRole is the single boundary between persisted role projections and
// authorization. The database deliberately does not copy the live catalog
// into the built-in role, so that role is expanded here. Custom role data must
// be entirely known to the current process before it can be returned or used
// for authorization.
//
// A role may hold no permissions at all: Bind Plane's Operator role signs in
// and releases bindings but administers nothing. Such a role grants no
// permission here; the role editor still refuses to save a custom role
// without any.
func normalizeRole(catalog domain.PermissionCatalog, role domain.Role) (domain.Role, error) {
	if role.IsSystem() {
		if role.SystemKey != "super_admin" {
			return domain.Role{}, ErrDependencyUnavailable
		}
		role.Permissions = catalog.Keys()
		return role, nil
	}
	if len(role.Permissions) == 0 {
		role.Permissions = []domain.PermissionKey{}
		return role, nil
	}
	permissions, err := catalog.ValidateFeatureSet(role.Permissions)
	if err != nil {
		return domain.Role{}, ErrDependencyUnavailable
	}
	role.Permissions = permissions
	return role, nil
}

func normalizePrincipal(catalog domain.PermissionCatalog, principal domain.Principal) (domain.Principal, error) {
	if len(principal.Roles) == 0 {
		return domain.Principal{}, ErrDependencyUnavailable
	}
	superAdmin := false
	rolePermissions := make([]domain.PermissionKey, 0)
	for index := range principal.Roles {
		role, err := normalizeRole(catalog, principal.Roles[index])
		if err != nil {
			return domain.Principal{}, err
		}
		principal.Roles[index] = role
		rolePermissions = append(rolePermissions, role.Permissions...)
		if role.IsSystem() {
			superAdmin = true
		}
	}
	rolePermissions = uniquePermissionKeys(rolePermissions)
	if len(principal.Permissions) > 0 {
		// PostgreSQL currently returns this denormalized summary as a useful
		// integrity signal. It is never an authority source: only permissions
		// carried by the normalized role assignments are used below.
		summary := append([]domain.PermissionKey(nil), principal.Permissions...)
		for _, permission := range summary {
			if !catalog.Has(permission) {
				return domain.Principal{}, ErrDependencyUnavailable
			}
		}
		sort.Slice(summary, func(i, j int) bool { return summary[i] < summary[j] })
		if !samePermissionKeys(summary, rolePermissions) {
			return domain.Principal{}, ErrDependencyUnavailable
		}
	}
	principal.SuperAdmin = superAdmin
	principal.Permissions = rolePermissions
	return principal, nil
}

func uniquePermissionKeys(keys []domain.PermissionKey) []domain.PermissionKey {
	seen := make(map[domain.PermissionKey]struct{}, len(keys))
	result := make([]domain.PermissionKey, 0, len(keys))
	for _, key := range keys {
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func samePermissionKeys(left, right []domain.PermissionKey) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// ensurePrincipalIdentity keeps the projection lookup bound to the identity
// which authenticated the request. A repository that accidentally returns a
// different user's projection must never grant that projection's authority to
// the requested actor.
func ensurePrincipalIdentity(expectedID string, principal domain.Principal) error {
	if expectedID == "" || principal.User.ID != expectedID {
		return ErrDependencyUnavailable
	}
	return nil
}

func normalizeAccessUser(catalog domain.PermissionCatalog, user domain.AccessUser) (domain.AccessUser, error) {
	if len(user.Roles) == 0 {
		return domain.AccessUser{}, ErrDependencyUnavailable
	}
	for index := range user.Roles {
		role, err := normalizeRole(catalog, user.Roles[index])
		if err != nil {
			return domain.AccessUser{}, err
		}
		user.Roles[index] = role
	}
	return user, nil
}

func normalizeInvitation(catalog domain.PermissionCatalog, invitation domain.Invitation) (domain.Invitation, error) {
	if len(invitation.Roles) == 0 {
		return domain.Invitation{}, ErrDependencyUnavailable
	}
	for index := range invitation.Roles {
		role, err := normalizeRole(catalog, invitation.Roles[index])
		if err != nil {
			return domain.Invitation{}, err
		}
		invitation.Roles[index] = role
	}
	return invitation, nil
}
