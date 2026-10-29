package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata" // schedule time zones work in minimal containers without zoneinfo

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/adapter/httpapi"
	mailadapter "github.com/jonathanhu237/bind-plane/next/api/internal/auth/adapter/mail"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/adapter/password"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/adapter/postgres"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	"github.com/jonathanhu237/bind-plane/next/api/internal/config"
	"github.com/jonathanhu237/bind-plane/next/api/internal/release"
)

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func healthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})
}

// newHandler preserves the original health-only test seam. Runtime wiring is
// performed by newApplicationHandler after configuration and schema checks.
func newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /health", healthHandler())
	return mux
}

func setupLimiter(value any) application.SetupLimiter {
	limiter, _ := value.(application.SetupLimiter)
	return limiter
}

func invitationAcceptLimiter(value any) application.InvitationAcceptLimiter {
	limiter, _ := value.(application.InvitationAcceptLimiter)
	return limiter
}

func invitationSendLimiter(value any) application.InvitationSendLimiter {
	limiter, _ := value.(application.InvitationSendLimiter)
	return limiter
}

func firstRecovery(recovery []httpapi.PasswordRecoveryService) httpapi.PasswordRecoveryService {
	if len(recovery) == 0 {
		return nil
	}
	return recovery[0]
}

func personalSettingsService(auth application.AccountStore, hasher application.PasswordHasher, random application.RandomSource, cfg config.Config, settings *application.SettingsManagement) *application.PersonalSettings {
	profile, profileOK := auth.(application.PersonalProfileStore)
	passwords, passwordsOK := auth.(application.PasswordChangeStore)
	email, emailOK := auth.(application.EmailChangeStore)
	if !profileOK || !passwordsOK || !emailOK {
		return nil
	}
	personal := application.NewPersonalSettings(auth, profile, passwords, email, hasher, random, cfg.EmailChangeCodeKey, cfg.MailOutboxNotificationTTL, settings)
	if limiter, ok := auth.(application.EmailChangeLimiter); ok {
		personal.SetEmailChangeLimiter(limiter)
	}
	return personal
}

func buildAuthHandler(setup httpapi.SetupService, auth httpapi.AuthenticationService, cfg config.Config, recovery httpapi.PasswordRecoveryService, access httpapi.AccessService, accept httpapi.InvitationAcceptanceService, settings httpapi.SettingsService, operations httpapi.OperationLogService, identity httpapi.SystemIdentityService, personal httpapi.PersonalSettingsService, mailTasks ...httpapi.MailTaskService) http.Handler {
	if personal != nil {
		return httpapi.NewHandlerWithAccessAndOperationLogAndIdentityAndPersonalSettings(setup, auth, cfg, recovery, access, accept, settings, operations, identity, personal, mailTasks...)
	}
	if identity != nil && operations != nil {
		return httpapi.NewHandlerWithAccessAndOperationLogAndIdentity(setup, auth, cfg, recovery, access, accept, settings, operations, identity, mailTasks...)
	}
	if operations != nil {
		return httpapi.NewHandlerWithAccessAndOperationLog(setup, auth, cfg, recovery, access, accept, settings, operations, mailTasks...)
	}
	if settings != nil && len(mailTasks) > 0 {
		return httpapi.NewHandlerWithAccessAndMailTasks(setup, auth, cfg, recovery, access, accept, settings, mailTasks[0])
	}
	if settings != nil {
		return httpapi.NewHandlerWithAccess(setup, auth, cfg, recovery, access, accept, settings)
	}
	if len(mailTasks) > 0 {
		return httpapi.NewHandlerWithMailTasks(setup, auth, cfg, recovery, mailTasks[0])
	}
	return httpapi.NewHandler(setup, auth, cfg, recovery)
}

func newApplicationHandler(cfg config.Config, setup application.SetupStore, auth application.AccountStore, hasher application.PasswordHasher, sessions application.SessionStore, limiter application.LoginLimiter, random application.RandomSource, recovery ...httpapi.PasswordRecoveryService) http.Handler {
	return newApplicationHandlerWithSettings(cfg, setup, auth, hasher, sessions, limiter, random, firstRecovery(recovery), nil)
}

func newApplicationHandlerWithSettings(cfg config.Config, setup application.SetupStore, auth application.AccountStore, hasher application.PasswordHasher, sessions application.SessionStore, limiter application.LoginLimiter, random application.RandomSource, recovery httpapi.PasswordRecoveryService, settingsService *application.SettingsManagement) http.Handler {
	return newApplicationHandlerWithOperationLog(cfg, setup, auth, hasher, sessions, limiter, random, recovery, settingsService, nil)
}

func newApplicationHandlerWithOperationLog(cfg config.Config, setup application.SetupStore, auth application.AccountStore, hasher application.PasswordHasher, sessions application.SessionStore, limiter application.LoginLimiter, random application.RandomSource, recovery httpapi.PasswordRecoveryService, settingsService *application.SettingsManagement, operationLogs *application.OperationLogService) http.Handler {
	return newApplicationHandlerWithOperationLogAndIdentity(cfg, setup, auth, hasher, sessions, limiter, random, recovery, settingsService, operationLogs, nil)
}

func newApplicationHandlerWithOperationLogAndIdentity(cfg config.Config, setup application.SetupStore, auth application.AccountStore, hasher application.PasswordHasher, sessions application.SessionStore, limiter application.LoginLimiter, random application.RandomSource, recovery httpapi.PasswordRecoveryService, settingsService *application.SettingsManagement, operationLogs *application.OperationLogService, identity *application.SystemIdentityManagement, mailTasks ...httpapi.MailTaskService) http.Handler {
	setupService := application.NewSetup(setup, hasher, random, cfg.SetupLinkTTL, setupLimiter(limiter))
	catalog := domain.DefaultPermissionCatalog()
	authService := application.NewAuthentication(auth, hasher, sessions, limiter, random, catalog)
	mux := http.NewServeMux()
	mux.Handle("GET /health", healthHandler())
	if store, ok := auth.(application.AccessStore); ok {
		if principals, principalOK := auth.(application.PrincipalStore); principalOK {
			var access *application.AccessManagement
			if settingsService != nil {
				access = application.NewAccessManagementWithInvitations(store, principals, catalog, cfg.InvitationTokenKey, random, cfg.InvitationLinkTTL, settingsService)
			} else {
				access = application.NewAccessManagementWithInvitations(store, principals, catalog, cfg.InvitationTokenKey, random, cfg.InvitationLinkTTL)
			}
			access.SetInvitationSendLimiter(invitationSendLimiter(store))
			accept := application.NewInvitationAcceptance(store, hasher, cfg.InvitationTokenKey, invitationAcceptLimiter(store))
			personal := personalSettingsService(auth, hasher, random, cfg, settingsService)
			authHandler := buildAuthHandler(setupService, authService, cfg, recovery, access, accept, settingsService, operationLogs, identity, personal, mailTasks...)
			mux.Handle("/api", authHandler)
			mux.Handle("/api/", authHandler)
			return mux
		}
	}
	personal := personalSettingsService(auth, hasher, random, cfg, settingsService)
	authHandler := buildAuthHandler(setupService, authService, cfg, recovery, nil, nil, nil, nil, nil, personal, mailTasks...)
	mux.Handle("/api", authHandler)
	mux.Handle("/api/", authHandler)
	return mux
}

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}
	if cfg.WarnInsecurePublicURL {
		log.Printf("WARNING: development APP_PUBLIC_URL uses HTTP on a non-loopback host; setup and session credentials can be exposed in transit")
	}
	startupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := postgres.Open(startupContext, cfg)
	if err != nil {
		log.Fatalf("database startup failed: %v", err)
	}
	postgresStore := postgres.NewStore(db, cfg)
	if err := postgresStore.CheckSchema(startupContext); err != nil {
		log.Fatalf("database schema is not ready: %v", err)
	}
	hasher, err := password.NewHasher(cfg.PasswordHashMaxConcurrency)
	if err != nil {
		log.Fatalf("password hashing configuration failed: %v", err)
	}
	random := application.CryptoRandom()
	setupService := application.NewSetup(postgresStore, hasher, random, cfg.SetupLinkTTL)
	if token, required, err := setupService.IssueStartupToken(startupContext); err != nil {
		log.Fatalf("setup initialization failed: %v", err)
	} else if required {
		log.Printf("initial setup link (expires in %s): %s/setup#token=%s", cfg.SetupLinkTTL, cfg.PublicURL, token)
	}
	var secretBox application.SecretBox
	if len(cfg.EmailSettingsEncryptionKey) > 0 {
		secretBox, err = application.NewAESGCMSecretBox(cfg.EmailSettingsEncryptionKey)
		if err != nil {
			log.Fatalf("email settings encryption configuration failed: %v", err)
		}
	}
	// Task material must remain decryptable when the optional SMTP-settings
	// encryption key is added, removed, or rotated. Derive its purpose-specific
	// key only from the required password-reset authority, whose rotation already
	// has the intentional effect of invalidating password-reset credentials.
	mailTaskSecretBox, err := application.NewMailTaskSecretBox(cfg.PasswordResetTokenKey)
	if err != nil {
		log.Fatalf("mail task encryption configuration failed: %v", err)
	}
	postgresStore.SetMailTaskSecretBox(mailTaskSecretBox)
	runtimeMailer := application.NewReloadableMailer()
	operationLogs := application.NewOperationLogService(postgresStore)
	identityService := application.NewSystemIdentityManagement(postgresStore)
	settingsService := application.NewSettingsManagement(postgresStore, secretBox, func(settings application.SMTPSettings) (application.Mailer, error) {
		return mailadapter.NewSMTPMailerFromSettingsWithTimeout(settings, cfg.SMTPTimeout)
	}, runtimeMailer)
	settingsService.SetProductionMode(cfg.Environment == "production")
	settingsService.SetTestEmailLimiter(postgresStore)
	settingsService.SetSystemIdentityProvider(identityService)
	settingsService.SetMailTaskEnqueuer(postgresStore)
	if err := settingsService.LoadRuntime(startupContext); err != nil {
		log.Fatalf("email settings initialization failed: %v", err)
	}
	recovery := application.NewPasswordRecovery(
		postgresStore,
		postgresStore,
		hasher,
		random,
		cfg.PasswordResetTokenKey,
		cfg.PasswordResetLinkTTL,
		cfg.MailOutboxNotificationTTL,
		cfg.PasswordResetResponseMin,
		settingsService,
	)
	dispatcher := application.NewMailDispatcher(
		postgresStore,
		runtimeMailer,
		random,
		cfg.PasswordResetTokenKey,
		cfg.PublicURL,
		cfg.MailOutboxPollInterval,
		cfg.MailOutboxLeaseDuration,
		cfg.MailOutboxRetryInitial,
		cfg.MailOutboxRetryMax,
		cfg.InvitationTokenKey,
		cfg.EmailChangeCodeKey,
	)
	dispatcher.SetSystemIdentityProvider(identityService)
	dispatcher.SetMailTaskSecretBox(mailTaskSecretBox)
	dispatcher.SetMailerProvider(settingsService)
	dispatcher.SetMailRetryPolicyProvider(postgresStore)
	mailTaskManagement := application.NewMailTaskManagement(postgresStore, postgresStore, domain.DefaultPermissionCatalog())
	authHandler := newApplicationHandlerWithOperationLogAndIdentity(cfg, postgresStore, postgresStore, hasher, postgresStore, postgresStore, random, recovery, settingsService, operationLogs, identityService, mailTaskManagement)
	businessAuth := application.NewAuthentication(postgresStore, hasher, postgresStore, postgresStore, random, domain.DefaultPermissionCatalog())
	businessHandler := release.NewHandler(release.NewSQLStore(db), businessAuth, cfg.CookieName, cfg.Origin)
	assetsHandler, err := release.NewAdminHandler(db, businessAuth, cfg.CookieName, cfg.Origin)
	if err != nil {
		log.Fatalf("release credential configuration: %v", err)
	}
	scheduleZone, err := time.LoadLocation(envOr("BIND_PLANE_TIMEZONE", "Asia/Shanghai"))
	if err != nil {
		log.Fatalf("BIND_PLANE_TIMEZONE: %v", err)
	}
	opsHandler, err := release.NewOpsHandler(db, businessAuth, cfg.CookieName, cfg.Origin, scheduleZone)
	if err != nil {
		log.Fatalf("device operations configuration: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/ops/", opsHandler)
	mux.Handle("/api/releases/admin/", assetsHandler)
	mux.Handle("/api/releases", businessHandler)
	mux.Handle("/api/releases/", businessHandler)
	mux.Handle("/", authHandler)
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	dispatcherContext, cancelDispatcher := context.WithCancel(context.Background())
	cleanupContext, cancelCleanup := context.WithCancel(context.Background())
	var backgroundWorkers sync.WaitGroup
	backgroundWorkers.Add(5)
	go func() {
		defer backgroundWorkers.Done()
		opsHandler.RunScheduler(cleanupContext)
	}()
	go func() {
		defer backgroundWorkers.Done()
		opsHandler.RunComplianceEvaluator(cleanupContext)
	}()
	go func() {
		defer backgroundWorkers.Done()
		dispatcher.Run(dispatcherContext)
	}()
	go func() {
		defer backgroundWorkers.Done()
		operationLogs.RunCleanup(cleanupContext, time.Hour)
	}()
	go func() {
		defer backgroundWorkers.Done()
		postgresStore.RunStateCleanup(cleanupContext, time.Minute)
	}()
	workDone := make(chan struct{})
	go func() {
		backgroundWorkers.Wait()
		close(workDone)
	}()

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()
	log.Printf("API listening on %s", cfg.HTTPAddr)
	return runServerLifecycle(
		server,
		serverErrors,
		signals,
		cfg.ShutdownTimeout,
		func(drainContext context.Context) {
			dispatcher.BeginShutdown(drainContext)
			cancelDispatcher()
			cancelCleanup()
		},
		workDone,
		db.Close,
		terminateImmediately,
	)
}
