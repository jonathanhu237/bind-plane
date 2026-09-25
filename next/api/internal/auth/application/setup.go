package application

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

const setupTokenBytes = 32

type SetupStatus string

const (
	SetupRequired SetupStatus = "required"
	SetupComplete SetupStatus = "complete"
)

type SetupInput struct {
	Token    string
	Name     string
	Email    string
	Password string
	Locale   domain.Locale
	// SourceIP is supplied by the trusted HTTP adapter, never by JSON input.
	SourceIP string
}

type Setup struct {
	store   SetupStore
	hasher  PasswordHasher
	random  RandomSource
	ttl     time.Duration
	limiter SetupLimiter
}

func NewSetup(store SetupStore, hasher PasswordHasher, random RandomSource, ttl time.Duration, limiters ...SetupLimiter) *Setup {
	var limiter SetupLimiter
	if len(limiters) > 0 {
		limiter = limiters[0]
	}
	return &Setup{store: store, hasher: hasher, random: random, ttl: ttl, limiter: limiter}
}

func (s *Setup) Status(ctx context.Context) (SetupStatus, error) {
	complete, err := s.store.Status(ctx)
	if err != nil {
		return "", dependencyError(err)
	}
	if complete {
		return SetupComplete, nil
	}
	return SetupRequired, nil
}

// IssueStartupToken replaces the current setup credential only while setup is open.
// The returned token is intended solely for the startup log link.
func (s *Setup) IssueStartupToken(ctx context.Context) (token string, required bool, err error) {
	complete, err := s.store.Status(ctx)
	if err != nil {
		return "", false, dependencyError(err)
	}
	if complete {
		return "", false, nil
	}
	raw := make([]byte, setupTokenBytes)
	if err := s.random.Read(raw); err != nil {
		return "", false, dependencyError(err)
	}
	digest := sha256.Sum256(raw)
	complete, err = s.store.ReplaceCurrentToken(ctx, digest[:], s.ttl)
	if err != nil {
		return "", false, dependencyError(err)
	}
	if complete {
		return "", false, nil
	}
	return base64.RawURLEncoding.EncodeToString(raw), true, nil
}

func (s *Setup) Complete(ctx context.Context, input SetupInput) (domain.User, error) {
	complete, err := s.store.Status(ctx)
	if err != nil {
		return domain.User{}, dependencyError(err)
	}
	if complete {
		return domain.User{}, ErrSetupComplete
	}
	if s.limiter != nil {
		allowed, err := s.limiter.AllowSetup(ctx, input.SourceIP)
		if err != nil {
			return domain.User{}, dependencyError(err)
		}
		if !allowed {
			return domain.User{}, ErrRateLimited
		}
	}
	if !isUnpaddedBase64URL(input.Token, setupTokenBytes) {
		return domain.User{}, ErrInvalidSetupToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(input.Token)
	if err != nil {
		return domain.User{}, ErrInvalidSetupToken
	}
	digest := sha256.Sum256(raw)
	if err := s.store.PreflightToken(ctx, digest[:]); err != nil {
		if errors.Is(err, ErrSetupComplete) || errors.Is(err, ErrInvalidSetupToken) {
			return domain.User{}, err
		}
		return domain.User{}, dependencyError(err)
	}
	name, err := domain.NewName(input.Name)
	if err != nil {
		return domain.User{}, err
	}
	email, err := domain.NewEmail(input.Email)
	if err != nil {
		return domain.User{}, err
	}
	password, err := domain.NewPassword(input.Password)
	if err != nil {
		return domain.User{}, err
	}
	hash, err := s.hasher.Hash(ctx, string(password))
	if err != nil {
		if errors.Is(err, ErrPasswordHashBusy) {
			return domain.User{}, ErrDependencyUnavailable
		}
		return domain.User{}, dependencyError(err)
	}
	locale := input.Locale
	if !locale.Valid() {
		locale = domain.LocaleEnglish
	}
	var user domain.User
	if localized, ok := s.store.(SetupLocaleStore); ok {
		user, err = localized.CompleteWithLocale(ctx, digest[:], name, email, hash, locale)
	} else {
		user, err = s.store.Complete(ctx, digest[:], name, email, hash)
	}
	if err != nil {
		switch {
		case errors.Is(err, ErrSetupComplete), errors.Is(err, ErrInvalidSetupToken), errors.Is(err, ErrEmailAlreadyRegistered):
			return domain.User{}, err
		default:
			return domain.User{}, dependencyError(err)
		}
	}
	return user, nil
}

func isUnpaddedBase64URL(value string, decodedBytes int) bool {
	if len(value) != (decodedBytes*8+5)/6 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == decodedBytes && base64.RawURLEncoding.EncodeToString(decoded) == value
}

func dependencyError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrAccountDisabled) || errors.Is(err, ErrSelfUserOperation) || errors.Is(err, ErrSetupComplete) || errors.Is(err, ErrInvalidSetupToken) || errors.Is(err, ErrInvalidPasswordResetToken) || errors.Is(err, ErrEmailAlreadyRegistered) || errors.Is(err, ErrAccountNotFound) || errors.Is(err, ErrUnauthenticated) || errors.Is(err, ErrRateLimited) || errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrPasswordHashBusy) || errors.Is(err, ErrDependencyUnavailable) || errors.Is(err, ErrForbidden) || errors.Is(err, ErrRoleNotFound) || errors.Is(err, ErrRoleAlreadyExists) || errors.Is(err, ErrUserNotFound) || errors.Is(err, ErrInvitationNotFound) || errors.Is(err, ErrOperationLogNotFound) || errors.Is(err, ErrRoleInUse) || errors.Is(err, ErrImmutableRole) || errors.Is(err, ErrLastSuperAdmin) || errors.Is(err, ErrStaleRevision) || errors.Is(err, ErrInvalidRoleSet) || errors.Is(err, ErrInvitationPending) || errors.Is(err, ErrInvitationInvalid) || errors.Is(err, ErrSystemIdentityNotConfigured) || errors.Is(err, ErrInvalidSystemIdentity) || errors.Is(err, ErrEmailChangeNotFound) || errors.Is(err, ErrInvalidEmailChange) || errors.Is(err, ErrInvalidEmailChangeCode) || errors.Is(err, ErrEmailChangeExpired) || errors.Is(err, ErrEmailChangeResendTooSoon) || errors.Is(err, ErrEmailChangeAttemptsExceeded) || errors.Is(err, ErrAvatarNotFound) || errors.Is(err, ErrMailNotConfigured) || errors.Is(err, ErrMailSettingsNotSaved) || errors.Is(err, ErrMailTaskNotFound) || errors.Is(err, ErrMailTaskSending) || errors.Is(err, ErrMailTaskNotRetryable) || errors.Is(err, ErrMailTaskDependency) || errors.Is(err, ErrMailTaskNotConfigured) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrDependencyUnavailable, err)
}
