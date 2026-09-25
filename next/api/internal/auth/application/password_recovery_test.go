package application

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type recoveryLimiterFake struct {
	allowed  bool
	allowErr error
	calls    []string
}

func (f *recoveryLimiterFake) AllowPasswordReset(_ context.Context, email string) (bool, error) {
	f.calls = append(f.calls, email)
	return f.allowed, f.allowErr
}

type recoveryStoreFake struct {
	requestEmails  []string
	selectors      [][]byte
	digests        [][]byte
	preflightErr   error
	completeErr    error
	completeHash   string
	completeLocale domain.Locale
	changedAt      time.Time
	events         *[]string
}

func (f *recoveryStoreFake) RequestPasswordReset(_ context.Context, email string, selector, digest []byte, _ time.Duration, _ domain.Locale) error {
	f.requestEmails = append(f.requestEmails, email)
	f.selectors = append(f.selectors, append([]byte(nil), selector...))
	f.digests = append(f.digests, append([]byte(nil), digest...))
	if f.events != nil {
		*f.events = append(*f.events, "request")
	}
	return nil
}

func (f *recoveryStoreFake) PreflightPasswordReset(context.Context, []byte, []byte) error {
	if f.events != nil {
		*f.events = append(*f.events, "preflight")
	}
	return f.preflightErr
}

func (f *recoveryStoreFake) CompletePasswordReset(_ context.Context, _ []byte, _ []byte, hash string, locale domain.Locale, _ time.Duration) (time.Time, error) {
	if f.events != nil {
		*f.events = append(*f.events, "complete")
	}
	f.completeHash = hash
	f.completeLocale = locale
	return f.changedAt, f.completeErr
}

type recoveryHasherFake struct {
	hashCalls int
	hashErr   error
}

func (f *recoveryHasherFake) Hash(context.Context, string) (string, error) {
	f.hashCalls++
	return "new-hash", f.hashErr
}

func (f *recoveryHasherFake) Verify(context.Context, string, string) (bool, error) {
	return true, nil
}

type sequenceRandom struct {
	next byte
}

func (r *sequenceRandom) Read(dst []byte) error {
	for i := range dst {
		dst[i] = r.next
	}
	r.next++
	return nil
}

func TestPasswordRecoveryRequestDoesNotExposeAccountLookupAndWaitsMinimum(t *testing.T) {
	key := bytes.Repeat([]byte{0x2a}, domain.PasswordResetVerifierBytes)
	store := &recoveryStoreFake{}
	limiter := &recoveryLimiterFake{allowed: true}
	var slept time.Duration
	recovery := NewPasswordRecoveryWithClock(
		store,
		limiter,
		&recoveryHasherFake{},
		&sequenceRandom{next: 7},
		key,
		30*time.Minute,
		24*time.Hour,
		500*time.Millisecond,
		func() time.Time { return time.Unix(0, 0) },
		func(value time.Duration) { slept += value },
	)
	if err := recovery.Request(context.Background(), PasswordResetRequestInput{Email: " ADA@Example.COM "}); err != nil {
		t.Fatal(err)
	}
	if got := limiter.calls; len(got) != 1 || got[0] != "ada@example.com" {
		t.Fatalf("limiter calls = %#v", got)
	}
	if got := store.requestEmails; len(got) != 1 || got[0] != "ada@example.com" {
		t.Fatalf("store calls = %#v", got)
	}
	if slept != 500*time.Millisecond {
		t.Fatalf("minimum response wait = %s", slept)
	}
	if len(store.selectors) != 1 || len(store.selectors[0]) != domain.PasswordResetSelectorBytes || len(store.digests[0]) != domain.PasswordResetVerifierBytes {
		t.Fatalf("reset material lengths = %d, %d", len(store.selectors[0]), len(store.digests[0]))
	}
}

func TestPasswordRecoveryRequestValidationAndLimiterDenial(t *testing.T) {
	store := &recoveryStoreFake{}
	limiter := &recoveryLimiterFake{allowed: false}
	recovery := NewPasswordRecovery(store, limiter, &recoveryHasherFake{}, &sequenceRandom{}, bytes.Repeat([]byte{1}, 32), time.Minute, time.Hour, 0)
	for _, input := range []PasswordResetRequestInput{{Email: "bad"}} {
		if err := recovery.Request(context.Background(), input); err == nil {
			t.Errorf("Request(%#v) accepted invalid input", input)
		}
	}
	if len(limiter.calls) != 0 || len(store.requestEmails) != 0 {
		t.Fatalf("invalid requests reached dependencies: limiter=%#v store=%#v", limiter.calls, store.requestEmails)
	}
	if err := recovery.Request(context.Background(), PasswordResetRequestInput{Email: "a@example.com"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("limiter denial = %v", err)
	}
	if len(store.requestEmails) != 0 {
		t.Fatal("rate-limited request persisted reset state")
	}
}

func TestPasswordRecoveryCompletionPreflightsBeforeHashAndUsesSystemFallback(t *testing.T) {
	key := bytes.Repeat([]byte{0x3c}, domain.PasswordResetVerifierBytes)
	material, err := domain.NewPasswordResetMaterial(key, bytes.Repeat([]byte{0x19}, domain.PasswordResetSelectorBytes))
	if err != nil {
		t.Fatal(err)
	}
	events := []string{}
	store := &recoveryStoreFake{events: &events, changedAt: time.Unix(123, 0)}
	hasher := &recoveryHasherFake{}
	recovery := NewPasswordRecovery(store, &recoveryLimiterFake{allowed: true}, hasher, &sequenceRandom{}, key, time.Minute, time.Hour, 0)
	token, err := domain.NewPasswordResetToken(key, material.Selector)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovery.Complete(context.Background(), PasswordResetCompleteInput{Token: token, Password: "Aa1!xxxx"}); err != nil {
		t.Fatal(err)
	}
	if hasher.hashCalls != 1 || store.completeHash != "new-hash" || store.completeLocale != domain.LocaleEnglish {
		t.Fatalf("completion hash/locale = %d, %q, %q", hasher.hashCalls, store.completeHash, store.completeLocale)
	}
	if got, want := events, []string{"preflight", "complete"}; !bytes.Equal([]byte(joinEvents(got)), []byte(joinEvents(want))) {
		t.Fatalf("dependency order = %#v, want %#v", got, want)
	}
}

func TestPasswordRecoveryInvalidAuthorityNeverHashes(t *testing.T) {
	key := bytes.Repeat([]byte{0x3c}, domain.PasswordResetVerifierBytes)
	store := &recoveryStoreFake{preflightErr: ErrInvalidPasswordResetToken}
	hasher := &recoveryHasherFake{}
	recovery := NewPasswordRecovery(store, &recoveryLimiterFake{allowed: true}, hasher, &sequenceRandom{}, key, time.Minute, time.Hour, 0)
	material, err := domain.NewPasswordResetMaterial(key, bytes.Repeat([]byte{0x19}, domain.PasswordResetSelectorBytes))
	if err != nil {
		t.Fatal(err)
	}
	token, err := domain.NewPasswordResetToken(key, material.Selector)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovery.Complete(context.Background(), PasswordResetCompleteInput{Token: token, Password: "Aa1!xxxx"}); !errors.Is(err, ErrInvalidPasswordResetToken) {
		t.Fatalf("invalid authority error = %v", err)
	}
	if hasher.hashCalls != 0 {
		t.Fatalf("invalid authority hashed password %d times", hasher.hashCalls)
	}
}

func joinEvents(events []string) string {
	result := ""
	for _, event := range events {
		result += event + ","
	}
	return result
}
