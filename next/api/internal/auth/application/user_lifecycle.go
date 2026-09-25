package application

import (
	"context"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

func (m *AccessManagement) lifecycleStore(ctx context.Context, actorID, userID string, revision int64) (VersionedUserLifecycleStore, error) {
	if !domain.IsCanonicalUUID(userID) {
		return nil, ErrUserNotFound
	}
	if actorID == userID {
		return nil, ErrSelfUserOperation
	}
	if revision < 0 {
		return nil, ErrStaleRevision
	}
	actor, err := m.principal(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if !actor.SuperAdmin && !actor.Has(domain.PermissionUsersWrite) {
		return nil, ErrForbidden
	}
	store, ok := m.store.(VersionedUserLifecycleStore)
	if !ok {
		return nil, ErrDependencyUnavailable
	}
	return store, nil
}
func (m *AccessManagement) DeactivateUserWithRevision(ctx context.Context, actorID, userID string, revision int64) (domain.AccessUser, error) {
	store, err := m.lifecycleStore(ctx, actorID, userID, revision)
	if err != nil {
		return domain.AccessUser{}, err
	}
	user, err := store.DeactivateUserWithRevision(ctx, actorID, userID, revision)
	if err != nil {
		return domain.AccessUser{}, dependencyError(err)
	}
	return normalizeAccessUser(m.catalog, user)
}
func (m *AccessManagement) ReactivateUserWithRevision(ctx context.Context, actorID, userID string, revision int64) (domain.AccessUser, error) {
	store, err := m.lifecycleStore(ctx, actorID, userID, revision)
	if err != nil {
		return domain.AccessUser{}, err
	}
	user, err := store.ReactivateUserWithRevision(ctx, actorID, userID, revision)
	if err != nil {
		return domain.AccessUser{}, dependencyError(err)
	}
	return normalizeAccessUser(m.catalog, user)
}
func (m *AccessManagement) DeleteUserWithRevision(ctx context.Context, actorID, userID string, revision int64) error {
	store, err := m.lifecycleStore(ctx, actorID, userID, revision)
	if err != nil {
		return err
	}
	return dependencyError(store.DeleteUserWithRevision(ctx, actorID, userID, revision))
}
