package application

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

const sessionIDBytes = 32

type LoginInput struct {
	Email    string
	Password string
	Locale   domain.Locale
	// SourceIP is supplied by the trusted HTTP adapter, never by JSON input.
	SourceIP string
}

type Authentication struct {
	accounts AccountStore
	hasher   PasswordHasher
	sessions SessionStore
	limiter  LoginLimiter
	random   RandomSource
	catalog  domain.PermissionCatalog
}

func NewAuthentication(accounts AccountStore, hasher PasswordHasher, sessions SessionStore, limiter LoginLimiter, random RandomSource, catalogs ...domain.PermissionCatalog) *Authentication {
	catalog := domain.DefaultPermissionCatalog()
	if len(catalogs) > 0 && len(catalogs[0].Definitions()) > 0 {
		catalog = catalogs[0]
	}
	return &Authentication{accounts: accounts, hasher: hasher, sessions: sessions, limiter: limiter, random: random, catalog: catalog}
}

func (a *Authentication) Login(ctx context.Context, input LoginInput) (domain.User, string, error) {
	email, err := domain.NewEmail(input.Email)
	if err != nil {
		return domain.User{}, "", err
	}
	password, err := domain.NewLoginPassword(input.Password)
	if err != nil {
		return domain.User{}, "", err
	}
	allowed, err := a.allowLogin(ctx, input.SourceIP, email.Canonical)
	if err != nil {
		return domain.User{}, "", dependencyError(err)
	}
	if !allowed {
		return domain.User{}, "", ErrRateLimited
	}
	account, err := a.accounts.FindByCanonicalEmail(ctx, email.Canonical)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			return domain.User{}, "", ErrInvalidCredentials
		}
		return domain.User{}, "", dependencyError(err)
	}
	valid, err := a.hasher.Verify(ctx, account.PasswordHash, string(password))
	if err != nil {
		if errors.Is(err, ErrPasswordHashBusy) {
			return domain.User{}, "", ErrDependencyUnavailable
		}
		return domain.User{}, "", dependencyError(err)
	}
	if !valid {
		return domain.User{}, "", ErrInvalidCredentials
	}
	if err := a.limiter.ResetEmail(ctx, email.Canonical); err != nil {
		return domain.User{}, "", dependencyError(err)
	}
	raw := make([]byte, sessionIDBytes)
	if err := a.random.Read(raw); err != nil {
		return domain.User{}, "", dependencyError(err)
	}
	sessionID := base64.RawURLEncoding.EncodeToString(raw)
	authVersion := account.AuthVersion
	if authVersion <= 0 {
		// A versioned session store is only safe when the PostgreSQL-backed
		// account version is authoritative. Do not silently turn a malformed
		// or missing version into the initial version in that mode.
		if _, ok := a.sessions.(VersionedSessionStore); ok {
			return domain.User{}, "", ErrDependencyUnavailable
		}
		authVersion = 1
	}
	var createErr error
	if versioned, ok := a.sessions.(VersionedSessionStore); ok {
		createErr = versioned.CreateVersioned(ctx, sessionID, account.User.ID, authVersion)
	} else {
		createErr = a.sessions.Create(ctx, sessionID, account.User.ID)
	}
	if err := createErr; err != nil {
		return domain.User{}, "", dependencyError(err)
	}
	if accountLocales, ok := a.accounts.(AccountLocaleStore); ok && !account.User.Locale.Valid() && input.Locale.Valid() {
		localized, err := accountLocales.InitializeLocale(ctx, account.User.ID, input.Locale)
		if err != nil {
			_ = a.sessions.Delete(ctx, sessionID)
			return domain.User{}, "", dependencyError(err)
		}
		account = localized
	}
	return account.User, sessionID, nil
}

func (a *Authentication) allowLogin(ctx context.Context, sourceIP, canonicalEmail string) (bool, error) {
	if a.limiter == nil {
		return false, ErrDependencyUnavailable
	}
	if sourceAware, ok := a.limiter.(SourceAwareLoginLimiter); ok {
		return sourceAware.AllowLogin(ctx, sourceIP, canonicalEmail)
	}
	return a.limiter.Allow(ctx, canonicalEmail)
}

func (a *Authentication) Current(ctx context.Context, sessionID string) (domain.User, error) {
	return a.current(ctx, sessionID, true)
}

// CurrentNoTouch checks a session without updating its activity timestamp or
// idle expiry. Background status checks must use this path so an open page
// cannot keep an otherwise idle session alive.
func (a *Authentication) CurrentNoTouch(ctx context.Context, sessionID string) (domain.User, error) {
	return a.current(ctx, sessionID, false)
}

func (a *Authentication) current(ctx context.Context, sessionID string, touch bool) (domain.User, error) {
	if !isUnpaddedBase64URL(sessionID, sessionIDBytes) {
		return domain.User{}, ErrUnauthenticated
	}
	versioned, hasVersionedSession := a.sessions.(VersionedSessionStore)
	var userID string
	var sessionVersion int64
	var err error
	if hasVersionedSession {
		if touch {
			userID, sessionVersion, err = versioned.ResolveAndTouchVersioned(ctx, sessionID)
		} else {
			readOnly, ok := a.sessions.(ReadOnlyVersionedSessionStore)
			if !ok {
				return domain.User{}, ErrDependencyUnavailable
			}
			userID, sessionVersion, err = readOnly.ResolveVersioned(ctx, sessionID)
		}
	} else {
		if touch {
			userID, err = a.sessions.ResolveAndTouch(ctx, sessionID)
		} else {
			readOnly, ok := a.sessions.(ReadOnlySessionStore)
			if !ok {
				return domain.User{}, ErrDependencyUnavailable
			}
			userID, err = readOnly.Resolve(ctx, sessionID)
		}
	}
	if err != nil {
		return domain.User{}, dependencyError(err)
	}
	if userID == "" {
		return domain.User{}, ErrUnauthenticated
	}
	if hasVersionedSession {
		versionedAccounts, ok := a.accounts.(VersionedAccountStore)
		if !ok {
			// Falling back to FindPublicByID would authorize a versioned
			// session without checking the PostgreSQL revocation authority.
			return domain.User{}, ErrDependencyUnavailable
		}
		account, err := versionedAccounts.FindPublicAccountByID(ctx, userID)
		if err != nil {
			if errors.Is(err, ErrAccountNotFound) {
				return domain.User{}, ErrUnauthenticated
			}
			return domain.User{}, dependencyError(err)
		}
		if account.AuthVersion <= 0 || sessionVersion != account.AuthVersion {
			// PostgreSQL auth_version is the revocation authority. Session
			// deletion is only cleanup and cannot turn this into a 503.
			_ = a.sessions.Delete(ctx, sessionID)
			return domain.User{}, ErrUnauthenticated
		}
		return account.User, nil
	}
	user, err := a.accounts.FindPublicByID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			return domain.User{}, ErrUnauthenticated
		}
		return domain.User{}, dependencyError(err)
	}
	return user, nil
}

// PrincipalStore enriches the authenticated identity with its current role
// assignments. It is intentionally optional so the original authentication
// seams remain usable by small consumers and tests.
type PrincipalStore interface {
	FindPrincipalByID(context.Context, string) (domain.Principal, error)
}

type PrincipalAuthenticationService interface {
	LoginWithPrincipal(context.Context, LoginInput) (domain.Principal, string, error)
	CurrentPrincipal(context.Context, string) (domain.Principal, error)
}

// NoTouchAuthenticationService is intentionally separate from
// PrincipalAuthenticationService so existing callers keep the ordinary
// renewing authentication behavior unless they explicitly opt into a
// background probe.
type NoTouchAuthenticationService interface {
	CurrentNoTouch(context.Context, string) (domain.User, error)
}

type NoTouchPrincipalAuthenticationService interface {
	CurrentPrincipalNoTouch(context.Context, string) (domain.Principal, error)
}

func (a *Authentication) LoginWithPrincipal(ctx context.Context, input LoginInput) (domain.Principal, string, error) {
	user, sessionID, err := a.Login(ctx, input)
	if err != nil {
		return domain.Principal{}, "", err
	}
	store, ok := a.accounts.(PrincipalStore)
	if !ok {
		return domain.Principal{User: user}, sessionID, nil
	}
	principal, err := store.FindPrincipalByID(ctx, user.ID)
	if err != nil {
		_ = a.sessions.Delete(ctx, sessionID)
		return domain.Principal{}, "", dependencyError(err)
	}
	if err := ensurePrincipalIdentity(user.ID, principal); err != nil {
		_ = a.sessions.Delete(ctx, sessionID)
		return domain.Principal{}, "", err
	}
	principal, err = a.normalizePrincipal(principal)
	if err != nil {
		_ = a.sessions.Delete(ctx, sessionID)
		return domain.Principal{}, "", err
	}
	return principal, sessionID, nil
}

func (a *Authentication) CurrentPrincipal(ctx context.Context, sessionID string) (domain.Principal, error) {
	user, err := a.Current(ctx, sessionID)
	return a.principalForUser(ctx, user, err)
}

// CurrentPrincipalNoTouch is the read-only authentication path used by
// session probes and background online-user/operation-status requests.
func (a *Authentication) CurrentPrincipalNoTouch(ctx context.Context, sessionID string) (domain.Principal, error) {
	user, err := a.CurrentNoTouch(ctx, sessionID)
	return a.principalForUser(ctx, user, err)
}

func (a *Authentication) principalForUser(ctx context.Context, user domain.User, err error) (domain.Principal, error) {
	if err != nil {
		return domain.Principal{}, err
	}
	store, ok := a.accounts.(PrincipalStore)
	if !ok {
		return domain.Principal{User: user}, nil
	}
	principal, err := store.FindPrincipalByID(ctx, user.ID)
	if err != nil {
		return domain.Principal{}, dependencyError(err)
	}
	if err := ensurePrincipalIdentity(user.ID, principal); err != nil {
		return domain.Principal{}, err
	}
	return a.normalizePrincipal(principal)
}

func (a *Authentication) normalizePrincipal(principal domain.Principal) (domain.Principal, error) {
	return normalizePrincipal(a.catalog, principal)
}

func (a *Authentication) Logout(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	if !isUnpaddedBase64URL(sessionID, sessionIDBytes) {
		return nil
	}
	if err := a.sessions.Delete(ctx, sessionID); err != nil {
		return dependencyError(err)
	}
	return nil
}

// cryptoRandom is kept as a small adapter so use cases remain testable.
type cryptoRandom struct{}

func (cryptoRandom) Read(dst []byte) error {
	_, err := rand.Read(dst)
	return err
}

func CryptoRandom() RandomSource { return cryptoRandom{} }
