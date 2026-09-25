package config

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment string
	PublicURL   string
	Origin      string
	HTTPAddr    string
	// TrustedProxyCIDRs controls which immediate peers may supply
	// X-Forwarded-For for operation-log source attribution. It is empty by
	// default so a direct client cannot spoof its source address.
	TrustedProxyCIDRs []string

	SetupLinkTTL time.Duration

	PasswordResetTokenKey       []byte
	InvitationTokenKey          []byte
	EmailChangeCodeKey          []byte
	EmailSettingsEncryptionKey  []byte
	PasswordResetLinkTTL        time.Duration
	InvitationLinkTTL           time.Duration
	PasswordResetResponseMin    time.Duration
	PasswordResetGlobalCapacity int
	PasswordResetGlobalRefill   time.Duration
	PasswordResetEmailCapacity  int
	PasswordResetEmailRefill    time.Duration
	PasswordResetIPCapacity     int
	PasswordResetIPRefill       time.Duration

	// Anonymous source buckets protect the high-cost token and setup entries
	// without coupling them to account or invitation state.
	SetupIPCapacity                 int
	SetupIPRefill                   time.Duration
	InvitationAcceptIPCapacity      int
	InvitationAcceptIPRefill        time.Duration
	PasswordResetCompleteIPCapacity int
	PasswordResetCompleteIPRefill   time.Duration

	// Authenticated mail actions share the same invitation recipient bucket
	// across create and resend, while test mail has its own namespace.
	InvitationSendActorCapacity     int
	InvitationSendActorRefill       time.Duration
	InvitationSendRecipientCapacity int
	InvitationSendRecipientRefill   time.Duration
	TestEmailGlobalCapacity         int
	TestEmailGlobalRefill           time.Duration
	TestEmailActorCapacity          int
	TestEmailActorRefill            time.Duration
	TestEmailRecipientCapacity      int
	TestEmailRecipientRefill        time.Duration
	EmailChangeActorCapacity         int
	EmailChangeActorRefill           time.Duration
	EmailChangeRecipientCapacity     int
	EmailChangeRecipientRefill       time.Duration

	MailOutboxPollInterval    time.Duration
	MailOutboxLeaseDuration   time.Duration
	MailOutboxRetryInitial    time.Duration
	MailOutboxRetryMax        time.Duration
	MailOutboxNotificationTTL time.Duration

	SMTPTimeout     time.Duration
	ShutdownTimeout time.Duration

	PostgresHost     string
	PostgresPort     string
	PostgresDatabase string
	PostgresUser     string
	PostgresPassword string
	PostgresSSLMode  string

	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxIdleTime time.Duration
	DBConnMaxLifetime time.Duration

	SessionIdleTimeout     time.Duration
	SessionAbsoluteTimeout time.Duration

	PasswordHashMaxConcurrency int

	LoginGlobalCapacity       int
	LoginGlobalRefillInterval time.Duration
	LoginEmailCapacity        int
	LoginEmailRefillInterval  time.Duration
	LoginIPCapacity           int
	LoginIPRefillInterval     time.Duration

	CookieName            string
	SecureCookie          bool
	WarnInsecurePublicURL bool
}

type Lookup func(string) string

func Load(get Lookup) (Config, error) {
	c := Config{
		Environment:       getDefault(get, "APP_ENV", "development"),
		PublicURL:         getDefault(get, "APP_PUBLIC_URL", "http://localhost:5173"),
		HTTPAddr:          getDefault(get, "HTTP_ADDR", "127.0.0.1:8080"),
		TrustedProxyCIDRs: parseCSV(get("TRUSTED_PROXY_CIDRS")),
		SetupLinkTTL:      parseDuration(get, "SETUP_LINK_TTL", 30*time.Minute),

		PasswordResetTokenKey:           parseTokenKey(get("PASSWORD_RESET_TOKEN_KEY")),
		InvitationTokenKey:              parseTokenKey(get("INVITATION_TOKEN_KEY")),
		EmailChangeCodeKey:              parseOptionalTokenKey(get("EMAIL_CHANGE_CODE_KEY")),
		EmailSettingsEncryptionKey:      parseOptionalTokenKey(get("EMAIL_SETTINGS_ENCRYPTION_KEY")),

		PasswordResetLinkTTL:            parseDuration(get, "PASSWORD_RESET_LINK_TTL", 30*time.Minute),
		InvitationLinkTTL:               parseDuration(get, "INVITATION_LINK_TTL", 72*time.Hour),
		PasswordResetResponseMin:        parseDuration(get, "PASSWORD_RESET_MIN_RESPONSE_TIME", 500*time.Millisecond),
		PasswordResetGlobalCapacity:     parseInt(get, "PASSWORD_RESET_RATE_LIMIT_GLOBAL_CAPACITY", 10),
		PasswordResetGlobalRefill:       parseDuration(get, "PASSWORD_RESET_RATE_LIMIT_GLOBAL_REFILL_INTERVAL", 6*time.Second),
		PasswordResetEmailCapacity:      parseInt(get, "PASSWORD_RESET_RATE_LIMIT_EMAIL_CAPACITY", 3),
		PasswordResetEmailRefill:        parseDuration(get, "PASSWORD_RESET_RATE_LIMIT_EMAIL_REFILL_INTERVAL", 20*time.Minute),
		PasswordResetIPCapacity:         parseInt(get, "PASSWORD_RESET_RATE_LIMIT_IP_CAPACITY", 30),
		PasswordResetIPRefill:           parseDuration(get, "PASSWORD_RESET_RATE_LIMIT_IP_REFILL_INTERVAL", 6*time.Second),
		SetupIPCapacity:                 parseInt(get, "SETUP_RATE_LIMIT_IP_CAPACITY", 10),
		SetupIPRefill:                   parseDuration(get, "SETUP_RATE_LIMIT_IP_REFILL_INTERVAL", time.Minute),
		InvitationAcceptIPCapacity:      parseInt(get, "INVITATION_ACCEPT_RATE_LIMIT_IP_CAPACITY", 30),
		InvitationAcceptIPRefill:        parseDuration(get, "INVITATION_ACCEPT_RATE_LIMIT_IP_REFILL_INTERVAL", time.Minute),
		PasswordResetCompleteIPCapacity: parseInt(get, "PASSWORD_RESET_COMPLETE_RATE_LIMIT_IP_CAPACITY", 20),
		PasswordResetCompleteIPRefill:   parseDuration(get, "PASSWORD_RESET_COMPLETE_RATE_LIMIT_IP_REFILL_INTERVAL", time.Minute),
		InvitationSendActorCapacity:     parseInt(get, "INVITATION_RATE_LIMIT_ACTOR_CAPACITY", 20),
		InvitationSendActorRefill:       parseDuration(get, "INVITATION_RATE_LIMIT_ACTOR_REFILL_INTERVAL", time.Minute),
		InvitationSendRecipientCapacity: parseInt(get, "INVITATION_RATE_LIMIT_RECIPIENT_CAPACITY", 3),
		InvitationSendRecipientRefill:   parseDuration(get, "INVITATION_RATE_LIMIT_RECIPIENT_REFILL_INTERVAL", 20*time.Minute),
		TestEmailGlobalCapacity:         parseInt(get, "TEST_EMAIL_RATE_LIMIT_GLOBAL_CAPACITY", 20),
		TestEmailGlobalRefill:           parseDuration(get, "TEST_EMAIL_RATE_LIMIT_GLOBAL_REFILL_INTERVAL", time.Hour),
		TestEmailActorCapacity:          parseInt(get, "TEST_EMAIL_RATE_LIMIT_ACTOR_CAPACITY", 5),
		TestEmailActorRefill:            parseDuration(get, "TEST_EMAIL_RATE_LIMIT_ACTOR_REFILL_INTERVAL", time.Hour),
		TestEmailRecipientCapacity:      parseInt(get, "TEST_EMAIL_RATE_LIMIT_RECIPIENT_CAPACITY", 3),
		TestEmailRecipientRefill:        parseDuration(get, "TEST_EMAIL_RATE_LIMIT_RECIPIENT_REFILL_INTERVAL", 20*time.Minute),
		EmailChangeActorCapacity:        parseInt(get, "EMAIL_CHANGE_RATE_LIMIT_ACTOR_CAPACITY", 5),
		EmailChangeActorRefill:          parseDuration(get, "EMAIL_CHANGE_RATE_LIMIT_ACTOR_REFILL_INTERVAL", time.Hour),
		EmailChangeRecipientCapacity:    parseInt(get, "EMAIL_CHANGE_RATE_LIMIT_RECIPIENT_CAPACITY", 3),
		EmailChangeRecipientRefill:      parseDuration(get, "EMAIL_CHANGE_RATE_LIMIT_RECIPIENT_REFILL_INTERVAL", 20*time.Minute),

		MailOutboxPollInterval:    parseDuration(get, "MAIL_DISPATCH_INTERVAL", time.Second),
		MailOutboxLeaseDuration:   parseDuration(get, "MAIL_OUTBOX_LEASE_TTL", 30*time.Second),
		MailOutboxRetryInitial:    parseDuration(get, "MAIL_RETRY_INITIAL_INTERVAL", 5*time.Second),
		MailOutboxRetryMax:        parseDuration(get, "MAIL_RETRY_MAX_INTERVAL", 10*time.Minute),
		MailOutboxNotificationTTL: parseDuration(get, "MAIL_NOTIFICATION_TTL", 24*time.Hour),

		SMTPTimeout:     parseDuration(get, "SMTP_DELIVERY_TIMEOUT", 10*time.Second),
		ShutdownTimeout: parseDuration(get, "SHUTDOWN_TIMEOUT", 30*time.Second),

		PostgresHost:     getDefault(get, "POSTGRES_HOST", "localhost"),
		PostgresPort:     getDefault(get, "POSTGRES_PORT", "5432"),
		PostgresDatabase: getDefault(get, "POSTGRES_DB", "temvia"),
		PostgresUser:     getDefault(get, "POSTGRES_USER", "temvia"),
		PostgresPassword: get("POSTGRES_PASSWORD"),
		PostgresSSLMode:  getDefault(get, "POSTGRES_SSLMODE", "disable"),

		DBMaxOpenConns:    parseInt(get, "DB_MAX_OPEN_CONNS", 10),
		DBMaxIdleConns:    parseInt(get, "DB_MAX_IDLE_CONNS", 5),
		DBConnMaxIdleTime: parseDuration(get, "DB_CONN_MAX_IDLE_TIME", 5*time.Minute),
		DBConnMaxLifetime: parseDuration(get, "DB_CONN_MAX_LIFETIME", 0),

		SessionIdleTimeout:     parseDuration(get, "SESSION_IDLE_TIMEOUT", 30*time.Minute),
		SessionAbsoluteTimeout: parseDuration(get, "SESSION_ABSOLUTE_TIMEOUT", 12*time.Hour),

		PasswordHashMaxConcurrency: parseInt(get, "PASSWORD_HASH_MAX_CONCURRENCY", 2),

		LoginGlobalCapacity:       parseInt(get, "LOGIN_RATE_LIMIT_GLOBAL_CAPACITY", 60),
		LoginGlobalRefillInterval: parseDuration(get, "LOGIN_RATE_LIMIT_GLOBAL_REFILL_INTERVAL", 6*time.Second),
		LoginEmailCapacity:        parseInt(get, "LOGIN_RATE_LIMIT_EMAIL_CAPACITY", 5),
		LoginEmailRefillInterval:  parseDuration(get, "LOGIN_RATE_LIMIT_EMAIL_REFILL_INTERVAL", time.Minute),
		LoginIPCapacity:           parseInt(get, "LOGIN_RATE_LIMIT_IP_CAPACITY", 30),
		LoginIPRefillInterval:     parseDuration(get, "LOGIN_RATE_LIMIT_IP_REFILL_INTERVAL", 6*time.Second),
	}
	if len(c.EmailChangeCodeKey) == 0 && len(c.PasswordResetTokenKey) == 32 {
		// A dedicated environment value is supported, while installations that
		// predate this setting still get a purpose-separated key derived from
		// the existing secret at process startup. Never use the password-reset
		// key directly for a different credential authority.
		c.EmailChangeCodeKey = derivePurposeKey(c.PasswordResetTokenKey, "temvia-email-change-code-key-v1")
	}

	if err := c.validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func getDefault(get Lookup, key, fallback string) string {
	if value := get(key); value != "" {
		return value
	}
	return fallback
}

func parseDuration(get Lookup, key string, fallback time.Duration) time.Duration {
	value := get(key)
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return -1
	}
	return duration
}

func parseInt(get Lookup, key string, fallback int) int {
	value := get(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return parsed
}

func parseTokenKey(value string) []byte {
	if value == "" {
		return nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != value {
		return []byte{1}
	}
	return decoded
}

func parseOptionalTokenKey(value string) []byte {
	if value == "" {
		return nil
	}
	return parseTokenKey(value)
}

func derivePurposeKey(master []byte, purpose string) []byte {
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte(purpose))
	return mac.Sum(nil)
}

func (c *Config) validate() error {
	if c.Environment != "development" && c.Environment != "production" {
		return fmt.Errorf("APP_ENV must be development or production")
	}
	public, err := parseOriginURL(c.PublicURL, true)
	if err != nil {
		return fmt.Errorf("APP_PUBLIC_URL: %w", err)
	}
	if public.Scheme != "http" && public.Scheme != "https" {
		return fmt.Errorf("APP_PUBLIC_URL must use http or https")
	}
	if c.Environment == "production" && public.Scheme != "https" {
		return fmt.Errorf("APP_PUBLIC_URL must use https in production")
	}
	origin, err := canonicalOrigin(public)
	if err != nil {
		return err
	}
	c.Origin = origin
	c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	c.WarnInsecurePublicURL = c.Environment == "development" && public.Scheme == "http" && !isLoopbackHost(public.Hostname())
	if c.PublicURL == "" {
		return fmt.Errorf("APP_PUBLIC_URL must not be empty")
	}
	for _, value := range c.TrustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(value); err != nil {
			return fmt.Errorf("TRUSTED_PROXY_CIDRS contains invalid CIDR %q", value)
		}
	}
	if c.SetupLinkTTL <= 0 || c.SetupLinkTTL > 24*time.Hour {
		return fmt.Errorf("SETUP_LINK_TTL must be between 0 and 24h")
	}
	if len(c.PasswordResetTokenKey) != 32 {
		return fmt.Errorf("PASSWORD_RESET_TOKEN_KEY must be a canonical unpadded Base64URL encoding of 32 bytes")
	}
	if len(c.InvitationTokenKey) != 32 {
		return fmt.Errorf("INVITATION_TOKEN_KEY must be a canonical unpadded Base64URL encoding of 32 bytes")
	}
	if len(c.EmailChangeCodeKey) != 32 {
		return fmt.Errorf("EMAIL_CHANGE_CODE_KEY must be a canonical unpadded Base64URL encoding of 32 bytes")
	}
	if hmac.Equal(c.EmailChangeCodeKey, c.PasswordResetTokenKey) {
		return fmt.Errorf("EMAIL_CHANGE_CODE_KEY must be distinct from PASSWORD_RESET_TOKEN_KEY")
	}
	if c.EmailSettingsEncryptionKey != nil && len(c.EmailSettingsEncryptionKey) != 32 {
		return fmt.Errorf("EMAIL_SETTINGS_ENCRYPTION_KEY must be a canonical unpadded Base64URL encoding of 32 bytes")
	}
	if c.PasswordResetLinkTTL <= 0 || c.PasswordResetLinkTTL > 24*time.Hour {
		return fmt.Errorf("PASSWORD_RESET_LINK_TTL must be between 0 and 24h")
	}
	if c.InvitationLinkTTL <= 0 || c.InvitationLinkTTL > 7*24*time.Hour {
		return fmt.Errorf("INVITATION_LINK_TTL must be between 0 and 7 days")
	}
	if c.PasswordResetResponseMin < 0 || c.PasswordResetResponseMin > 5*time.Second {
		return fmt.Errorf("PASSWORD_RESET_MIN_RESPONSE_TIME must be between 0 and 5s")
	}
	if err := validateRateLimitSettings("password-reset global", c.PasswordResetGlobalCapacity, c.PasswordResetGlobalRefill); err != nil {
		return err
	}
	if err := validateRateLimitSettings("password-reset email", c.PasswordResetEmailCapacity, c.PasswordResetEmailRefill); err != nil {
		return err
	}
	if err := validateRateLimitSettings("password-reset IP", c.PasswordResetIPCapacity, c.PasswordResetIPRefill); err != nil {
		return err
	}
	if err := validateRateLimitSettings("setup IP", c.SetupIPCapacity, c.SetupIPRefill); err != nil {
		return err
	}
	if err := validateRateLimitSettings("invitation-accept IP", c.InvitationAcceptIPCapacity, c.InvitationAcceptIPRefill); err != nil {
		return err
	}
	if err := validateRateLimitSettings("password-reset-complete IP", c.PasswordResetCompleteIPCapacity, c.PasswordResetCompleteIPRefill); err != nil {
		return err
	}
	if err := validateRateLimitSettings("invitation actor", c.InvitationSendActorCapacity, c.InvitationSendActorRefill); err != nil {
		return err
	}
	if err := validateRateLimitSettings("invitation recipient", c.InvitationSendRecipientCapacity, c.InvitationSendRecipientRefill); err != nil {
		return err
	}
	if err := validateRateLimitSettings("test-email global", c.TestEmailGlobalCapacity, c.TestEmailGlobalRefill); err != nil {
		return err
	}
	if err := validateRateLimitSettings("test-email actor", c.TestEmailActorCapacity, c.TestEmailActorRefill); err != nil {
		return err
	}
	if err := validateRateLimitSettings("test-email recipient", c.TestEmailRecipientCapacity, c.TestEmailRecipientRefill); err != nil {
		return err
	}
	if err := validateRateLimitSettings("email-change actor", c.EmailChangeActorCapacity, c.EmailChangeActorRefill); err != nil {
		return err
	}
	if err := validateRateLimitSettings("email-change recipient", c.EmailChangeRecipientCapacity, c.EmailChangeRecipientRefill); err != nil {
		return err
	}
	if c.MailOutboxPollInterval < time.Millisecond || c.MailOutboxLeaseDuration < time.Second || c.MailOutboxLeaseDuration <= c.SMTPTimeout || c.MailOutboxRetryInitial < time.Millisecond || c.MailOutboxRetryMax < c.MailOutboxRetryInitial || c.MailOutboxNotificationTTL < time.Minute {
		return fmt.Errorf("mail outbox settings are invalid")
	}
	if c.SMTPTimeout < time.Millisecond || c.SMTPTimeout > 5*time.Minute {
		return fmt.Errorf("SMTP_DELIVERY_TIMEOUT must be between 1ms and 5m")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("SHUTDOWN_TIMEOUT must be a positive Go duration")
	}
	if c.HTTPAddr == "" {
		return fmt.Errorf("HTTP_ADDR must not be empty")
	}
	if c.PostgresHost == "" || c.PostgresPort == "" || c.PostgresDatabase == "" || c.PostgresUser == "" {
		return fmt.Errorf("PostgreSQL host, port, database, and user must not be empty")
	}
	identifier := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,62}$`)
	if !identifier.MatchString(c.PostgresDatabase) || !identifier.MatchString(c.PostgresUser) {
		return fmt.Errorf("POSTGRES_DB and POSTGRES_USER must be conservative ASCII identifiers of at most 63 characters")
	}
	port, err := strconv.Atoi(c.PostgresPort)
	if _, _, splitErr := net.SplitHostPort(net.JoinHostPort(c.PostgresHost, c.PostgresPort)); splitErr != nil || err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("POSTGRES_PORT must be a valid port")
	}
	if c.PostgresPassword == "" {
		return fmt.Errorf("POSTGRES_PASSWORD must not be empty")
	}
	if c.PostgresSSLMode == "" {
		return fmt.Errorf("POSTGRES_SSLMODE must not be empty")
	}
	if c.DBMaxOpenConns <= 0 || c.DBMaxIdleConns < 0 || c.DBMaxIdleConns > c.DBMaxOpenConns || c.DBConnMaxIdleTime < 0 || c.DBConnMaxLifetime < 0 {
		return fmt.Errorf("database pool settings are invalid")
	}
	if c.SessionIdleTimeout < time.Millisecond || c.SessionAbsoluteTimeout < time.Millisecond || c.SessionIdleTimeout >= c.SessionAbsoluteTimeout {
		return fmt.Errorf("session idle timeout must be at least 1ms and less than absolute timeout")
	}
	if c.PasswordHashMaxConcurrency <= 0 {
		return fmt.Errorf("PASSWORD_HASH_MAX_CONCURRENCY must be positive")
	}
	if err := validateRateLimitSettings("login global", c.LoginGlobalCapacity, c.LoginGlobalRefillInterval); err != nil {
		return err
	}
	if err := validateRateLimitSettings("login email", c.LoginEmailCapacity, c.LoginEmailRefillInterval); err != nil {
		return err
	}
	if err := validateRateLimitSettings("login IP", c.LoginIPCapacity, c.LoginIPRefillInterval); err != nil {
		return err
	}
	if public.Scheme == "https" {
		c.CookieName = "__Host-temvia_session"
		c.SecureCookie = true
	} else {
		c.CookieName = "temvia_session"
	}
	return nil
}

func validateRateLimitSettings(name string, capacity int, refill time.Duration) error {
	if capacity <= 0 || refill < time.Millisecond {
		return fmt.Errorf("%s rate-limit settings must use a positive capacity and an interval of at least 1ms", name)
	}
	// The database stores retention as milliseconds and computes (capacity+1)
	// intervals. Reject values that would overflow either representation rather
	// than silently wrapping into a short retention period.
	maxDuration := time.Duration(1<<63 - 1)
	maxIntervals := int64(maxDuration / refill)
	if int64(capacity) >= maxIntervals {
		return fmt.Errorf("%s rate-limit settings overflow the retention duration", name)
	}
	durationMillis := int64(refill / time.Millisecond)
	if refill%time.Millisecond != 0 {
		durationMillis++
	}
	if durationMillis <= 0 || int64(capacity) >= (int64(1<<63-1)/durationMillis) {
		return fmt.Errorf("%s rate-limit settings overflow database milliseconds", name)
	}
	return nil
}

func parseCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func canonicalOrigin(value *url.URL) (string, error) {
	host := strings.ToLower(value.Hostname())
	if host == "" {
		return "", fmt.Errorf("APP_PUBLIC_URL must include a host")
	}
	port := value.Port()
	if port == "" {
		if value.Scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}
	parsed, err := strconv.Atoi(port)
	if err != nil || parsed < 1 || parsed > 65535 {
		return "", fmt.Errorf("APP_PUBLIC_URL contains an invalid port")
	}
	port = strconv.Itoa(parsed)
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return value.Scheme + "://" + host + ":" + port, nil
}

// CanonicalOrigin parses an Origin header value into scheme, lowercase host,
// and explicit effective port. It rejects every component an Origin header
// must not carry.
func CanonicalOrigin(raw string) (string, error) {
	value, err := parseOriginURL(raw, false)
	if err != nil {
		return "", err
	}
	return canonicalOrigin(value)
}

func parseOriginURL(raw string, allowRootPath bool) (*url.URL, error) {
	value, err := url.Parse(raw)
	validPath := value != nil && value.Path == ""
	if allowRootPath && value != nil {
		validPath = value.Path == "" || value.Path == "/"
	}
	if err != nil || value == nil || value.Scheme == "" || value.Host == "" || value.User != nil || !validPath || value.RawQuery != "" || value.ForceQuery || value.Fragment != "" || strings.Contains(raw, "#") {
		return nil, fmt.Errorf("must be an absolute origin without credentials, query, fragment, or path")
	}
	if value.Scheme != "http" && value.Scheme != "https" {
		return nil, fmt.Errorf("must use http or https")
	}
	return value, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c Config) DatabaseDSN() string {
	// pgx's keyword format avoids URL escaping pitfalls for arbitrary passwords.
	return "host=" + quoteDSN(c.PostgresHost) + " port=" + quoteDSN(c.PostgresPort) + " dbname=" + quoteDSN(c.PostgresDatabase) + " user=" + quoteDSN(c.PostgresUser) + " password=" + quoteDSN(c.PostgresPassword) + " sslmode=" + quoteDSN(c.PostgresSSLMode)
}

func quoteDSN(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	return "'" + strings.ReplaceAll(value, "'", "\\'") + "'"
}
