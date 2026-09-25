package config

import (
	"testing"
	"time"
)

func env(values map[string]string) Lookup {
	return func(key string) string { return values[key] }
}

func TestLoadDefaultsAndModes(t *testing.T) {
	values := map[string]string{"POSTGRES_PASSWORD": "pg-secret", "PASSWORD_RESET_TOKEN_KEY": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "INVITATION_TOKEN_KEY": "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"}
	c, err := Load(env(values))
	if err != nil {
		t.Fatal(err)
	}
	if c.CookieName != "temvia_session" || c.SecureCookie || c.Origin != "http://localhost:5173" {
		t.Fatalf("development config = %#v", c)
	}
	if c.ShutdownTimeout != 30*time.Second {
		t.Fatalf("shutdown timeout = %s, want 30s", c.ShutdownTimeout)
	}
	if c.LoginGlobalCapacity != 60 || c.LoginIPCapacity != 30 || c.PasswordResetIPCapacity != 30 || c.SetupIPCapacity != 10 || c.TestEmailRecipientCapacity != 3 || c.EmailChangeActorCapacity != 5 || c.EmailChangeActorRefill != time.Hour || c.EmailChangeRecipientCapacity != 3 || c.EmailChangeRecipientRefill != 20*time.Minute {
		t.Fatalf("abuse-protection defaults = %#v", c)
	}
	if len(c.EmailChangeCodeKey) != 32 || string(c.EmailChangeCodeKey) == string(c.PasswordResetTokenKey) {
		t.Fatalf("email-change key was not purpose-separated: %#v", c.EmailChangeCodeKey)
	}
	values["SHUTDOWN_TIMEOUT"] = "7s"
	c, err = Load(env(values))
	if err != nil || c.ShutdownTimeout != 7*time.Second {
		t.Fatalf("shutdown timeout override = %s, %v", c.ShutdownTimeout, err)
	}
	values["APP_PUBLIC_URL"] = "http://LOCALHOST:05173/"
	c, err = Load(env(values))
	if err != nil || c.PublicURL != "http://LOCALHOST:05173" || c.Origin != "http://localhost:5173" {
		t.Fatalf("normalized development URL = %#v, %v", c, err)
	}
	values["APP_PUBLIC_URL"] = "http://192.0.2.10:5173"
	c, err = Load(env(values))
	if err != nil || !c.WarnInsecurePublicURL {
		t.Fatalf("non-loopback development warning = %t, %v", c.WarnInsecurePublicURL, err)
	}
	values["APP_ENV"] = "production"
	values["APP_PUBLIC_URL"] = "https://example.com"
	c, err = Load(env(values))
	if err != nil {
		t.Fatal(err)
	}
	if c.CookieName != "__Host-temvia_session" || !c.SecureCookie {
		t.Fatalf("production cookie config = %#v", c)
	}
}

func TestLoadEmailChangeSettings(t *testing.T) {
	values := map[string]string{
		"POSTGRES_PASSWORD":        "pg-secret",
		"PASSWORD_RESET_TOKEN_KEY": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"INVITATION_TOKEN_KEY":     "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE",
	}
	c, err := Load(env(values))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.EmailChangeCodeKey) != 32 || c.EmailChangeActorCapacity != 5 || c.EmailChangeActorRefill != time.Hour || c.EmailChangeRecipientCapacity != 3 || c.EmailChangeRecipientRefill != 20*time.Minute {
		t.Fatalf("email-change defaults = %#v", c)
	}

	values["EMAIL_CHANGE_CODE_KEY"] = "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI"
	values["EMAIL_CHANGE_RATE_LIMIT_ACTOR_CAPACITY"] = "7"
	values["EMAIL_CHANGE_RATE_LIMIT_ACTOR_REFILL_INTERVAL"] = "2m"
	values["EMAIL_CHANGE_RATE_LIMIT_RECIPIENT_CAPACITY"] = "9"
	values["EMAIL_CHANGE_RATE_LIMIT_RECIPIENT_REFILL_INTERVAL"] = "30m"
	c, err = Load(env(values))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.EmailChangeCodeKey) != 32 || c.EmailChangeCodeKey[0] != 2 || c.EmailChangeActorCapacity != 7 || c.EmailChangeActorRefill != 2*time.Minute || c.EmailChangeRecipientCapacity != 9 || c.EmailChangeRecipientRefill != 30*time.Minute {
		t.Fatalf("email-change overrides = %#v", c)
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	base := map[string]string{"POSTGRES_PASSWORD": "pg-secret", "PASSWORD_RESET_TOKEN_KEY": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "INVITATION_TOKEN_KEY": "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"}
	for name, change := range map[string]func(map[string]string){
		"missing postgres secret":      func(v map[string]string) { delete(v, "POSTGRES_PASSWORD") },
		"missing invitation secret":    func(v map[string]string) { delete(v, "INVITATION_TOKEN_KEY") },
		"production http":              func(v map[string]string) { v["APP_ENV"] = "production" },
		"bad duration":                 func(v map[string]string) { v["SETUP_LINK_TTL"] = "nope" },
		"bad shutdown duration":        func(v map[string]string) { v["SHUTDOWN_TIMEOUT"] = "nope" },
		"zero shutdown duration":       func(v map[string]string) { v["SHUTDOWN_TIMEOUT"] = "0s" },
		"negative shutdown duration":   func(v map[string]string) { v["SHUTDOWN_TIMEOUT"] = "-1s" },
		"idle after absolute":          func(v map[string]string) { v["SESSION_IDLE_TIMEOUT"] = "13h" },
		"idle pool above open":         func(v map[string]string) { v["DB_MAX_IDLE_CONNS"] = "11" },
		"bad port":                     func(v map[string]string) { v["POSTGRES_PORT"] = "postgres" },
		"unsafe database name":         func(v map[string]string) { v["POSTGRES_DB"] = "database/name" },
		"unsafe database user":         func(v map[string]string) { v["POSTGRES_USER"] = "user@example" },
		"invalid invitation key":       func(v map[string]string) { v["INVITATION_TOKEN_KEY"] = "not-base64" },
		"reused email-change key":      func(v map[string]string) { v["EMAIL_CHANGE_CODE_KEY"] = v["PASSWORD_RESET_TOKEN_KEY"] },
		"invitation ttl above maximum": func(v map[string]string) { v["INVITATION_LINK_TTL"] = "8d" },
		"zero login ip capacity":       func(v map[string]string) { v["LOGIN_RATE_LIMIT_IP_CAPACITY"] = "0" },
		"short setup refill":           func(v map[string]string) { v["SETUP_RATE_LIMIT_IP_REFILL_INTERVAL"] = "500us" },
		"invalid trusted proxy":        func(v map[string]string) { v["TRUSTED_PROXY_CIDRS"] = "not-a-network" },
		"retention overflow":           func(v map[string]string) { v["TEST_EMAIL_RATE_LIMIT_GLOBAL_CAPACITY"] = "9223372036854775807" },
	} {
		t.Run(name, func(t *testing.T) {
			values := map[string]string{}
			for key, value := range base {
				values[key] = value
			}
			change(values)
			if _, err := Load(env(values)); err == nil {
				t.Fatal("Load accepted invalid configuration")
			}
		})
	}
}

func TestLoadPasswordRecoveryAndMailRuntimeSettings(t *testing.T) {
	values := map[string]string{
		"POSTGRES_PASSWORD":                "pg-secret",
		"PASSWORD_RESET_TOKEN_KEY":         "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"INVITATION_TOKEN_KEY":             "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE",
		"PASSWORD_RESET_MIN_RESPONSE_TIME": "250ms",
		"SMTP_DELIVERY_TIMEOUT":            "5s",
		"MAIL_OUTBOX_LEASE_TTL":            "10s",
	}
	c, err := Load(env(values))
	if err != nil {
		t.Fatal(err)
	}
	if c.InvitationLinkTTL != 72*time.Hour || len(c.InvitationTokenKey) != 32 {
		t.Fatalf("invitation config = %#v", c)
	}
	if c.PasswordResetResponseMin != 250*time.Millisecond || c.SMTPTimeout != 5*time.Second || c.MailOutboxLeaseDuration != 10*time.Second {
		t.Fatalf("loaded recovery config = %#v", c)
	}
	if _, err := Load(env(map[string]string{
		"POSTGRES_PASSWORD":        "pg-secret",
		"PASSWORD_RESET_TOKEN_KEY": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"INVITATION_TOKEN_KEY":     "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE",
		"SMTP_PORT":                "bad",
		"SMTP_TLS_MODE":            "invalid",
		"MAIL_FROM_ADDRESS":        "bad\r\n@example.com",
	})); err != nil {
		t.Fatalf("legacy SMTP environment should be ignored: %v", err)
	}
	values["MAIL_OUTBOX_LEASE_TTL"] = "5s"
	if _, err := Load(env(values)); err == nil {
		t.Fatal("outbox lease shorter than delivery timeout accepted")
	}
}

func TestCanonicalOrigin(t *testing.T) {
	for input, want := range map[string]string{
		"http://LOCALHOST:05173": "http://localhost:5173",
		"https://example.com":    "https://example.com:443",
		"http://[::1]:8080":      "http://[::1]:8080",
	} {
		got, err := CanonicalOrigin(input)
		if err != nil || got != want {
			t.Errorf("CanonicalOrigin(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"null", "ftp://example.com", "https://example.com/", "https://example.com?", "https://example.com#", "https://user@example.com"} {
		if _, err := CanonicalOrigin(input); err == nil {
			t.Errorf("CanonicalOrigin(%q) accepted invalid Origin", input)
		}
	}
}
