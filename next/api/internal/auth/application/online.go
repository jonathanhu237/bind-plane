package application

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// SessionActivity contains no credential or session identifier. A session is
// already known to be valid by the store; LastSeenAt is only display data.
type SessionActivity struct {
	UserID      string
	AuthVersion int64
	LastSeenAt  time.Time
}

type OnlineSessionStore interface {
	ValidSessions(context.Context) ([]SessionActivity, error)
}

type SessionRevocationStore interface {
	RevokeUserSessions(context.Context, string, string) error
}

type OnlineUser struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	AvatarURL     string    `json:"avatarUrl,omitempty"`
	HasAvatar     bool      `json:"hasAvatar"`
	AvatarVersion int64     `json:"avatarVersion,omitempty"`
	LastSeenAt    time.Time `json:"lastSeenAt"`
	SessionCount  int       `json:"sessionCount"`
}

func (a *Authentication) OnlineUsers(ctx context.Context, actorID string) ([]OnlineUser, error) {
	principals, ok := a.accounts.(PrincipalStore)
	if !ok {
		return nil, ErrDependencyUnavailable
	}
	actor, err := principals.FindPrincipalByID(ctx, actorID)
	if err != nil {
		return nil, dependencyError(err)
	}
	if !actor.SuperAdmin && !actor.Has(domain.PermissionOnlineUsersRead) {
		return nil, ErrForbidden
	}
	sessions, ok := a.sessions.(OnlineSessionStore)
	if !ok {
		return nil, ErrDependencyUnavailable
	}
	accounts, ok := a.accounts.(VersionedAccountStore)
	if !ok {
		return nil, ErrDependencyUnavailable
	}
	activity, err := sessions.ValidSessions(ctx)
	if err != nil {
		return nil, dependencyError(err)
	}
	grouped := make(map[string][]SessionActivity)
	for _, item := range activity {
		grouped[item.UserID] = append(grouped[item.UserID], item)
	}
	result := make([]OnlineUser, 0, len(grouped))
	for id, items := range grouped {
		account, err := accounts.FindPublicAccountByID(ctx, id)
		if errors.Is(err, ErrAccountNotFound) {
			continue
		}
		if err != nil {
			return nil, dependencyError(err)
		}
		user := OnlineUser{ID: id, Name: account.User.Name, Email: account.User.Email, HasAvatar: account.User.HasAvatar, AvatarVersion: account.User.AvatarVersion}
		if account.User.HasAvatar {
			user.AvatarURL = "/api/users/" + id + "/avatar?v=" + strconv.FormatInt(account.User.AvatarVersion, 10)
		}
		for _, item := range items {
			if item.AuthVersion <= 0 || item.AuthVersion != account.AuthVersion {
				continue
			}
			if item.LastSeenAt.IsZero() {
				continue
			}
			user.SessionCount++
			if item.LastSeenAt.After(user.LastSeenAt) {
				user.LastSeenAt = item.LastSeenAt
			}
		}
		if user.SessionCount > 0 {
			result = append(result, user)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].LastSeenAt.Equal(result[j].LastSeenAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].LastSeenAt.After(result[j].LastSeenAt)
	})
	return result, nil
}

func (a *Authentication) KickUser(ctx context.Context, actorID, userID string) error {
	if !domain.IsCanonicalUUID(userID) {
		return ErrUserNotFound
	}
	store, ok := a.accounts.(SessionRevocationStore)
	if !ok {
		return ErrDependencyUnavailable
	}
	return store.RevokeUserSessions(ctx, actorID, userID)
}
