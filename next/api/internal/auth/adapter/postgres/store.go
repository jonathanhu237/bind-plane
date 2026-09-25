package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	"github.com/jonathanhu237/bind-plane/next/api/internal/config"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const ExpectedMigrationVersion int64 = 16

const stateOperationTimeout = time.Second

var ErrSchemaNotReady = errors.New("database schema is not ready")

type Store struct {
	db                           *sql.DB
	operationTimeout             time.Duration
	idleTimeout                  time.Duration
	absoluteTimeout              time.Duration
	globalCapacity               int
	globalRefill                 time.Duration
	emailCapacity                int
	emailRefill                  time.Duration
	resetGlobalCapacity          int
	resetGlobalRefill            time.Duration
	resetEmailCapacity           int
	resetEmailRefill             time.Duration
	loginIPCapacity              int
	loginIPRefill                time.Duration
	resetIPCapacity              int
	resetIPRefill                time.Duration
	resetCompleteIPCapacity      int
	resetCompleteIPRefill        time.Duration
	setupIPCapacity              int
	setupIPRefill                time.Duration
	invitationAcceptIPCapacity   int
	invitationAcceptIPRefill     time.Duration
	invitationActorCapacity      int
	invitationActorRefill        time.Duration
	invitationRecipientCapacity  int
	invitationRecipientRefill    time.Duration
	testMailGlobalCapacity       int
	testMailGlobalRefill         time.Duration
	testMailActorCapacity        int
	testMailActorRefill          time.Duration
	testMailRecipientCapacity    int
	testMailRecipientRefill      time.Duration
	mailTaskSecretBox            application.SecretBox
	emailChangeActorCapacity     int
	emailChangeActorRefill       time.Duration
	emailChangeRecipientCapacity int
	emailChangeRecipientRefill   time.Duration
}

func Open(ctx context.Context, cfg config.Config) (*sql.DB, error) {
	db, err := sql.Open("pgx", cfg.DatabaseDSN())
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(cfg.DBMaxOpenConns)
	db.SetMaxIdleConns(cfg.DBMaxIdleConns)
	db.SetConnMaxIdleTime(cfg.DBConnMaxIdleTime)
	db.SetConnMaxLifetime(cfg.DBConnMaxLifetime)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return db, nil
}

func (s *Store) SetMailTaskSecretBox(box application.SecretBox) {
	if s != nil {
		s.mailTaskSecretBox = box
	}
}

// NewStore receives the generated Config in production, deriving the
// mail-task box from its stable password-reset key. No-config embedders must
// install a box explicitly before creating mail tasks; an unconfigured process
// never falls back to mutable legacy material.
func NewStore(db *sql.DB, configs ...config.Config) *Store {
	settings := config.Config{
		SessionIdleTimeout:              30 * time.Minute,
		SessionAbsoluteTimeout:          12 * time.Hour,
		LoginGlobalCapacity:             60,
		LoginGlobalRefillInterval:       6 * time.Second,
		LoginEmailCapacity:              5,
		LoginEmailRefillInterval:        time.Minute,
		PasswordResetGlobalCapacity:     10,
		PasswordResetGlobalRefill:       6 * time.Second,
		PasswordResetEmailCapacity:      3,
		PasswordResetEmailRefill:        20 * time.Minute,
		LoginIPCapacity:                 30,
		LoginIPRefillInterval:           6 * time.Second,
		PasswordResetIPCapacity:         30,
		PasswordResetIPRefill:           6 * time.Second,
		PasswordResetCompleteIPCapacity: 20,
		PasswordResetCompleteIPRefill:   time.Minute,
		SetupIPCapacity:                 10,
		SetupIPRefill:                   time.Minute,
		InvitationAcceptIPCapacity:      30,
		InvitationAcceptIPRefill:        time.Minute,
		InvitationSendActorCapacity:     20,
		InvitationSendActorRefill:       time.Minute,
		InvitationSendRecipientCapacity: 3,
		InvitationSendRecipientRefill:   20 * time.Minute,
		TestEmailGlobalCapacity:         20,
		TestEmailGlobalRefill:           time.Hour,
		TestEmailActorCapacity:          5,
		TestEmailActorRefill:            time.Hour,
		TestEmailRecipientCapacity:      3,
		TestEmailRecipientRefill:        20 * time.Minute,
		EmailChangeActorCapacity:        5,
		EmailChangeActorRefill:          time.Hour,
		EmailChangeRecipientCapacity:    3,
		EmailChangeRecipientRefill:      20 * time.Minute,
	}
	var mailTaskSecretBox application.SecretBox
	if len(configs) > 0 {
		settings = configs[0]
		// Mail-task material has its own purpose-derived key, but its master is
		// the required password-reset authority rather than the optional SMTP
		// settings key. Changing SMTP credential encryption must not make
		// retained tasks undecryptable after a restart.
		if box, boxErr := application.NewMailTaskSecretBox(settings.PasswordResetTokenKey); boxErr == nil {
			mailTaskSecretBox = box
		}
		if settings.SessionIdleTimeout <= 0 {
			settings.SessionIdleTimeout = 30 * time.Minute
		}
		if settings.SessionAbsoluteTimeout <= 0 {
			settings.SessionAbsoluteTimeout = 12 * time.Hour
		}
		if settings.LoginGlobalCapacity <= 0 {
			settings.LoginGlobalCapacity = 60
		}
		if settings.LoginGlobalRefillInterval <= 0 {
			settings.LoginGlobalRefillInterval = 6 * time.Second
		}
		if settings.LoginEmailCapacity <= 0 {
			settings.LoginEmailCapacity = 5
		}
		if settings.LoginEmailRefillInterval <= 0 {
			settings.LoginEmailRefillInterval = time.Minute
		}
		if settings.PasswordResetGlobalCapacity <= 0 {
			settings.PasswordResetGlobalCapacity = 10
		}
		if settings.PasswordResetGlobalRefill <= 0 {
			settings.PasswordResetGlobalRefill = 6 * time.Second
		}
		if settings.PasswordResetEmailCapacity <= 0 {
			settings.PasswordResetEmailCapacity = 3
		}
		if settings.PasswordResetEmailRefill <= 0 {
			settings.PasswordResetEmailRefill = 20 * time.Minute
		}
		if settings.LoginIPCapacity <= 0 {
			settings.LoginIPCapacity = 30
		}
		if settings.LoginIPRefillInterval <= 0 {
			settings.LoginIPRefillInterval = 6 * time.Second
		}
		if settings.PasswordResetIPCapacity <= 0 {
			settings.PasswordResetIPCapacity = 30
		}
		if settings.PasswordResetIPRefill <= 0 {
			settings.PasswordResetIPRefill = 6 * time.Second
		}
		if settings.PasswordResetCompleteIPCapacity <= 0 {
			settings.PasswordResetCompleteIPCapacity = 20
		}
		if settings.PasswordResetCompleteIPRefill <= 0 {
			settings.PasswordResetCompleteIPRefill = time.Minute
		}
		if settings.SetupIPCapacity <= 0 {
			settings.SetupIPCapacity = 10
		}
		if settings.SetupIPRefill <= 0 {
			settings.SetupIPRefill = time.Minute
		}
		if settings.InvitationAcceptIPCapacity <= 0 {
			settings.InvitationAcceptIPCapacity = 30
		}
		if settings.InvitationAcceptIPRefill <= 0 {
			settings.InvitationAcceptIPRefill = time.Minute
		}
		if settings.InvitationSendActorCapacity <= 0 {
			settings.InvitationSendActorCapacity = 20
		}
		if settings.InvitationSendActorRefill <= 0 {
			settings.InvitationSendActorRefill = time.Minute
		}
		if settings.InvitationSendRecipientCapacity <= 0 {
			settings.InvitationSendRecipientCapacity = 3
		}
		if settings.InvitationSendRecipientRefill <= 0 {
			settings.InvitationSendRecipientRefill = 20 * time.Minute
		}
		if settings.TestEmailGlobalCapacity <= 0 {
			settings.TestEmailGlobalCapacity = 20
		}
		if settings.TestEmailGlobalRefill <= 0 {
			settings.TestEmailGlobalRefill = time.Hour
		}
		if settings.TestEmailActorCapacity <= 0 {
			settings.TestEmailActorCapacity = 5
		}
		if settings.TestEmailActorRefill <= 0 {
			settings.TestEmailActorRefill = time.Hour
		}
		if settings.TestEmailRecipientCapacity <= 0 {
			settings.TestEmailRecipientCapacity = 3
		}
		if settings.TestEmailRecipientRefill <= 0 {
			settings.TestEmailRecipientRefill = 20 * time.Minute
		}
		if settings.EmailChangeActorCapacity <= 0 {
			settings.EmailChangeActorCapacity = 5
		}
		if settings.EmailChangeActorRefill <= 0 {
			settings.EmailChangeActorRefill = time.Hour
		}
		if settings.EmailChangeRecipientCapacity <= 0 {
			settings.EmailChangeRecipientCapacity = 3
		}
		if settings.EmailChangeRecipientRefill <= 0 {
			settings.EmailChangeRecipientRefill = 20 * time.Minute
		}
	}
	return &Store{
		db:                           db,
		operationTimeout:             stateOperationTimeout,
		idleTimeout:                  settings.SessionIdleTimeout,
		absoluteTimeout:              settings.SessionAbsoluteTimeout,
		globalCapacity:               settings.LoginGlobalCapacity,
		globalRefill:                 settings.LoginGlobalRefillInterval,
		emailCapacity:                settings.LoginEmailCapacity,
		emailRefill:                  settings.LoginEmailRefillInterval,
		resetGlobalCapacity:          settings.PasswordResetGlobalCapacity,
		resetGlobalRefill:            settings.PasswordResetGlobalRefill,
		resetEmailCapacity:           settings.PasswordResetEmailCapacity,
		resetEmailRefill:             settings.PasswordResetEmailRefill,
		loginIPCapacity:              settings.LoginIPCapacity,
		loginIPRefill:                settings.LoginIPRefillInterval,
		resetIPCapacity:              settings.PasswordResetIPCapacity,
		resetIPRefill:                settings.PasswordResetIPRefill,
		resetCompleteIPCapacity:      settings.PasswordResetCompleteIPCapacity,
		resetCompleteIPRefill:        settings.PasswordResetCompleteIPRefill,
		setupIPCapacity:              settings.SetupIPCapacity,
		setupIPRefill:                settings.SetupIPRefill,
		invitationAcceptIPCapacity:   settings.InvitationAcceptIPCapacity,
		invitationAcceptIPRefill:     settings.InvitationAcceptIPRefill,
		invitationActorCapacity:      settings.InvitationSendActorCapacity,
		invitationActorRefill:        settings.InvitationSendActorRefill,
		invitationRecipientCapacity:  settings.InvitationSendRecipientCapacity,
		invitationRecipientRefill:    settings.InvitationSendRecipientRefill,
		testMailGlobalCapacity:       settings.TestEmailGlobalCapacity,
		testMailGlobalRefill:         settings.TestEmailGlobalRefill,
		testMailActorCapacity:        settings.TestEmailActorCapacity,
		testMailActorRefill:          settings.TestEmailActorRefill,
		testMailRecipientCapacity:    settings.TestEmailRecipientCapacity,
		testMailRecipientRefill:      settings.TestEmailRecipientRefill,
		mailTaskSecretBox:            mailTaskSecretBox,
		emailChangeActorCapacity:     settings.EmailChangeActorCapacity,
		emailChangeActorRefill:       settings.EmailChangeActorRefill,
		emailChangeRecipientCapacity: settings.EmailChangeRecipientCapacity,
		emailChangeRecipientRefill:   settings.EmailChangeRecipientRefill,
	}
}

func (s *Store) CheckSchema(ctx context.Context) error {
	var version int64
	var dirty bool
	err := s.db.QueryRowContext(ctx, `SELECT version, dirty FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&version, &dirty)
	if err != nil {
		return fmt.Errorf("%w: read migration state: %v", ErrSchemaNotReady, err)
	}
	if dirty || version != ExpectedMigrationVersion {
		return fmt.Errorf("%w: expected version %d clean, got version %d dirty=%t", ErrSchemaNotReady, ExpectedMigrationVersion, version, dirty)
	}
	return nil
}

func (s *Store) Status(ctx context.Context) (bool, error) {
	var complete bool
	err := s.db.QueryRowContext(ctx, `SELECT completed_at IS NOT NULL FROM auth_setup WHERE singleton = true`).Scan(&complete)
	if err != nil {
		return false, err
	}
	return complete, nil
}

func (s *Store) ReplaceCurrentToken(ctx context.Context, digest []byte, ttl time.Duration) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var completedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT completed_at FROM auth_setup WHERE singleton = true FOR UPDATE`).Scan(&completedAt); err != nil {
		return false, err
	}
	if completedAt.Valid {
		return true, tx.Commit()
	}
	_, err = tx.ExecContext(ctx, `UPDATE auth_setup SET token_digest = $1, token_expires_at = clock_timestamp() + ($2 * INTERVAL '1 second') WHERE singleton = true`, digest, ttl.Seconds())
	if err != nil {
		return false, err
	}
	return false, tx.Commit()
}

func (s *Store) PreflightToken(ctx context.Context, digest []byte) error {
	var complete bool
	var stored []byte
	var valid bool
	err := s.db.QueryRowContext(ctx, `SELECT completed_at IS NOT NULL, token_digest, COALESCE(token_expires_at > clock_timestamp(), false) FROM auth_setup WHERE singleton = true`).Scan(&complete, &stored, &valid)
	if err != nil {
		return err
	}
	if complete {
		return application.ErrSetupComplete
	}
	if !valid || !equalDigest(stored, digest) {
		return application.ErrInvalidSetupToken
	}
	return nil
}

func equalDigest(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var different byte
	for i := range left {
		different |= left[i] ^ right[i]
	}
	return different == 0
}

func (s *Store) Complete(ctx context.Context, digest []byte, name domain.Name, email domain.Email, passwordHash string) (domain.User, error) {
	return s.CompleteWithLocale(ctx, digest, name, email, passwordHash, domain.LocaleEnglish)
}

func (s *Store) CompleteWithLocale(ctx context.Context, digest []byte, name domain.Name, email domain.Email, passwordHash string, locale domain.Locale) (domain.User, error) {
	if !locale.Valid() {
		locale = domain.LocaleEnglish
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.User{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var completedAt sql.NullTime
	var stored []byte
	var valid bool
	if err := tx.QueryRowContext(ctx, `SELECT completed_at, token_digest, COALESCE(token_expires_at > clock_timestamp(), false) FROM auth_setup WHERE singleton = true FOR UPDATE`).Scan(&completedAt, &stored, &valid); err != nil {
		return domain.User{}, err
	}
	if completedAt.Valid {
		return domain.User{}, application.ErrSetupComplete
	}
	if !valid || !equalDigest(stored, digest) {
		return domain.User{}, application.ErrInvalidSetupToken
	}
	var user domain.User
	err = tx.QueryRowContext(ctx, `INSERT INTO auth_users (name, email, email_canonical, password_hash, locale) VALUES ($1, $2, $3, $4, $5) RETURNING id::text, name, email, created_at, locale`, string(name), email.Display, email.Canonical, passwordHash, locale).Scan(&user.ID, &user.Name, &user.Email, &user.CreatedAt, &user.Locale)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.User{}, application.ErrEmailAlreadyRegistered
		}
		return domain.User{}, err
	}
	// Migration 3 creates the immutable system role before the setup flow can
	// complete. Keep the first assignment in the same winner transaction so a
	// fresh install is never left with an administrator who cannot administer
	// access.
	result, err := tx.ExecContext(ctx, `
		INSERT INTO auth_user_roles (user_id, role_id)
		SELECT $1::uuid, id FROM auth_roles WHERE system_key = 'super_admin'`, user.ID)
	if err != nil {
		return domain.User{}, err
	}
	if assigned, err := result.RowsAffected(); err != nil || assigned != 1 {
		if err != nil {
			return domain.User{}, err
		}
		return domain.User{}, application.ErrDependencyUnavailable
	}
	if _, err := tx.ExecContext(ctx, `UPDATE auth_setup SET token_digest = NULL, token_expires_at = NULL, completed_at = clock_timestamp() WHERE singleton = true`); err != nil {
		return domain.User{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
