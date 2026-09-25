package application

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type sourceAwareApplicationLimiter struct {
	allowLoginResult      bool
	allowResetResult      bool
	allowCompletionResult bool
	allowSetupResult      bool
	allowErr              error
	loginSource           string
	loginEmail            string
	resetSource           string
	resetEmail            string
	completionSource      string
	setupSource           string
	loginCalls            int
	resetCalls            int
	completionCalls       int
	setupCalls            int
}

func (l *sourceAwareApplicationLimiter) Allow(context.Context, string) (bool, error) {
	return l.allowLoginResult, l.allowErr
}

func (l *sourceAwareApplicationLimiter) ResetEmail(context.Context, string) error { return nil }

func (l *sourceAwareApplicationLimiter) AllowLogin(_ context.Context, source, email string) (bool, error) {
	l.loginCalls++
	l.loginSource, l.loginEmail = source, email
	return l.allowLoginResult, l.allowErr
}

func (l *sourceAwareApplicationLimiter) AllowPasswordReset(context.Context, string) (bool, error) {
	return l.allowResetResult, l.allowErr
}

func (l *sourceAwareApplicationLimiter) AllowPasswordResetFromSource(_ context.Context, source, email string) (bool, error) {
	l.resetCalls++
	l.resetSource, l.resetEmail = source, email
	return l.allowResetResult, l.allowErr
}

func (l *sourceAwareApplicationLimiter) AllowPasswordResetComplete(_ context.Context, source string) (bool, error) {
	l.completionCalls++
	l.completionSource = source
	return l.allowCompletionResult, l.allowErr
}

func (l *sourceAwareApplicationLimiter) AllowSetup(_ context.Context, source string) (bool, error) {
	l.setupCalls++
	l.setupSource = source
	return l.allowSetupResult, l.allowErr
}

func TestAuthenticationUsesSourceBucketBeforePasswordVerification(t *testing.T) {
	account := domain.Account{User: domain.User{ID: "user-1", Email: "ada@example.com"}, PasswordHash: "hash"}
	hasher := &fakeHasher{valid: true}
	limiter := &sourceAwareApplicationLimiter{allowLoginResult: false}
	sessions := &fakeSessions{}
	auth := NewAuthentication(fakeAccounts{account}, hasher, sessions, limiter, fakeRandom{value: 1})

	if _, _, err := auth.Login(context.Background(), LoginInput{Email: " ADA@Example.COM ", Password: "a sufficiently long password", SourceIP: "203.0.113.8"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("source denial = %v", err)
	}
	if limiter.loginCalls != 1 || limiter.loginSource != "203.0.113.8" || limiter.loginEmail != "ada@example.com" {
		t.Fatalf("login limiter call = %#v", limiter)
	}
	if hasher.verifyCalls != 0 || sessions.created != "" {
		t.Fatalf("source denial reached expensive/session work: verify=%d session=%q", hasher.verifyCalls, sessions.created)
	}

	limiter.allowLoginResult = true
	if _, _, err := auth.Login(context.Background(), LoginInput{Email: "ada@example.com", Password: "a sufficiently long password", SourceIP: "203.0.113.8"}); err != nil {
		t.Fatal(err)
	}
	if limiter.loginCalls != 2 || hasher.verifyCalls != 1 {
		t.Fatalf("source-aware login calls = %d, verify calls = %d", limiter.loginCalls, hasher.verifyCalls)
	}
}

func TestPasswordRecoveryUsesIndependentSourceBucketsBeforeResetSideEffects(t *testing.T) {
	key := bytes.Repeat([]byte{0x2a}, domain.PasswordResetVerifierBytes)
	store := &recoveryStoreFake{}
	limiter := &sourceAwareApplicationLimiter{allowResetResult: false, allowCompletionResult: false}
	recovery := NewPasswordRecovery(store, limiter, &recoveryHasherFake{}, &sequenceRandom{}, key, time.Minute, time.Hour, 0)

	if err := recovery.Request(context.Background(), PasswordResetRequestInput{Email: "ADA@EXAMPLE.COM", SourceIP: "198.51.100.4"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("reset request denial = %v", err)
	}
	if limiter.resetCalls != 1 || limiter.resetSource != "198.51.100.4" || limiter.resetEmail != "ada@example.com" || len(store.requestEmails) != 0 {
		t.Fatalf("reset source call/state = %#v, %#v", limiter, store.requestEmails)
	}

	if err := recovery.Complete(context.Background(), PasswordResetCompleteInput{Token: "invalid", Password: "Aa1!xxxx", SourceIP: "198.51.100.5"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("reset completion denial = %v", err)
	}
	if limiter.completionCalls != 1 || limiter.completionSource != "198.51.100.5" {
		t.Fatalf("completion source call = %#v", limiter)
	}
}

func TestSetupUsesSourceBucketBeforeTokenAndHashWork(t *testing.T) {
	store := &fakeSetupStore{}
	hasher := &fakeHasher{}
	limiter := &sourceAwareApplicationLimiter{allowSetupResult: false}
	setup := NewSetup(store, hasher, fakeRandom{value: 7}, time.Minute, limiter)
	token := base64SetupToken(7)

	if _, err := setup.Complete(context.Background(), SetupInput{Token: token, Name: "Ada", Email: "ada@example.com", Password: "Aa1!xxxx", SourceIP: "192.0.2.7"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("setup denial = %v", err)
	}
	if limiter.setupCalls != 1 || limiter.setupSource != "192.0.2.7" || hasher.hashCalls != 0 || store.digest != nil {
		t.Fatalf("setup source call/state = %#v, hash=%d, digest=%x", limiter, hasher.hashCalls, store.digest)
	}
}

func base64SetupToken(value byte) string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{value}, setupTokenBytes))
}

func TestProtectedOperationsFailClosedOnLimiterDependencyFailure(t *testing.T) {
	limiter := &sourceAwareApplicationLimiter{allowErr: ErrDependencyUnavailable}
	hasher := &fakeHasher{valid: true}
	sessions := &fakeSessions{}
	auth := NewAuthentication(fakeAccounts{domain.Account{User: domain.User{ID: "user-1", Email: "ada@example.com"}, PasswordHash: "hash"}}, hasher, sessions, limiter, fakeRandom{value: 1})
	if _, _, err := auth.Login(context.Background(), LoginInput{Email: "ada@example.com", Password: "a sufficiently long password", SourceIP: "198.51.100.7"}); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("login limiter failure = %v", err)
	}
	if hasher.verifyCalls != 0 || sessions.created != "" {
		t.Fatalf("login limiter failure reached side effects: verify=%d session=%q", hasher.verifyCalls, sessions.created)
	}

	recoveryStore := &recoveryStoreFake{}
	recovery := NewPasswordRecovery(recoveryStore, limiter, &recoveryHasherFake{}, &sequenceRandom{}, bytes.Repeat([]byte{0x2a}, domain.PasswordResetVerifierBytes), time.Minute, time.Hour, 0)
	if err := recovery.Request(context.Background(), PasswordResetRequestInput{Email: "ada@example.com", SourceIP: "198.51.100.7"}); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("password-reset limiter failure = %v", err)
	}
	if len(recoveryStore.requestEmails) != 0 {
		t.Fatalf("password-reset limiter failure created requests: %v", recoveryStore.requestEmails)
	}

	setupStore := &fakeSetupStore{}
	setup := NewSetup(setupStore, &fakeHasher{}, fakeRandom{value: 7}, time.Minute, limiter)
	if _, err := setup.Complete(context.Background(), SetupInput{Token: base64SetupToken(7), Name: "Ada", Email: "ada@example.com", Password: "Aa1!xxxx", SourceIP: "198.51.100.7"}); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("setup limiter failure = %v", err)
	}
	if setupStore.digest != nil {
		t.Fatalf("setup limiter failure persisted token digest: %x", setupStore.digest)
	}
}
