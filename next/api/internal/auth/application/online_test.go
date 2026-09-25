package application

import (
	"context"
	"errors"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	"testing"
	"time"
)

type onlineAccounts struct {
	AccountStore
	principal domain.Principal
	accounts  map[string]domain.Account
}

func (s *onlineAccounts) FindPrincipalByID(context.Context, string) (domain.Principal, error) {
	return s.principal, nil
}
func (s *onlineAccounts) FindPublicAccountByID(_ context.Context, id string) (domain.Account, error) {
	a, ok := s.accounts[id]
	if !ok {
		return a, ErrAccountNotFound
	}
	return a, nil
}

type onlineSessions struct {
	SessionStore
	items []SessionActivity
	calls int
	err   error
}

func (s *onlineSessions) ValidSessions(context.Context) ([]SessionActivity, error) {
	s.calls++
	return s.items, s.err
}

func TestOnlineUsersExcludeRevokedAndDeletedAndAggregateDevices(t *testing.T) {
	now := time.Now()
	accounts := &onlineAccounts{principal: domain.Principal{Permissions: []domain.PermissionKey{domain.PermissionOnlineUsersRead}}, accounts: map[string]domain.Account{
		"a": {User: domain.User{ID: "a", Name: "Alice"}, AuthVersion: 2},
		"b": {User: domain.User{ID: "b", Name: "Bob"}, AuthVersion: 1},
	}}
	sessions := &onlineSessions{items: []SessionActivity{{"a", 1, now}, {"a", 2, now.Add(-6 * time.Minute)}, {"a", 2, now}, {"b", 1, now.Add(-time.Second)}, {"deleted", 1, now}}}
	auth := &Authentication{accounts: accounts, sessions: sessions}
	users, err := auth.OnlineUsers(context.Background(), "actor")
	if err != nil || len(users) != 2 || users[0].ID != "a" || users[0].SessionCount != 2 || !users[0].LastSeenAt.Equal(now) {
		t.Fatalf("users=%+v err=%v", users, err)
	}
	accounts.accounts["a"] = domain.Account{User: domain.User{ID: "a"}, AuthVersion: 3}
	users, err = auth.OnlineUsers(context.Background(), "actor")
	if err != nil || len(users) != 1 || users[0].ID != "b" {
		t.Fatalf("revoked users=%+v err=%v", users, err)
	}
}
func TestOnlineUsersRequireReadAndFailClosed(t *testing.T) {
	accounts := &onlineAccounts{}
	sessions := &onlineSessions{}
	auth := &Authentication{accounts: accounts, sessions: sessions}
	if _, err := auth.OnlineUsers(context.Background(), "actor"); !errors.Is(err, ErrForbidden) || sessions.calls != 0 {
		t.Fatalf("permission check: %v calls=%d", err, sessions.calls)
	}
	accounts.principal.SuperAdmin = true
	sessions.err = errors.New("offline")
	if _, err := auth.OnlineUsers(context.Background(), "actor"); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("dependency error: %v", err)
	}
}
