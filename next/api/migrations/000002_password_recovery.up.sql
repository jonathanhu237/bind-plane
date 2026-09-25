ALTER TABLE auth_users
    ADD COLUMN auth_version bigint NOT NULL DEFAULT 1,
    ADD CONSTRAINT auth_users_auth_version_positive CHECK (auth_version > 0);

CREATE TABLE auth_password_resets (
    user_id uuid PRIMARY KEY REFERENCES auth_users(id) ON DELETE CASCADE,
    selector bytea NOT NULL UNIQUE,
    verifier_digest bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT auth_password_resets_selector_length CHECK (octet_length(selector) = 16),
    CONSTRAINT auth_password_resets_digest_length CHECK (octet_length(verifier_digest) = 32),
    CONSTRAINT auth_password_resets_expiry_after_creation CHECK (expires_at > created_at)
);

CREATE TABLE auth_mail_outbox (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    kind text NOT NULL,
    user_id uuid NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    reset_selector bytea,
    locale text NOT NULL,
    attempt_count integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    lease_token uuid,
    lease_expires_at timestamptz,
    sent_at timestamptz,
    canceled_at timestamptz,
    dead_at timestamptz,
    last_error_code text,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT auth_mail_outbox_kind CHECK (kind IN ('password_reset', 'password_changed')),
    CONSTRAINT auth_mail_outbox_selector_by_kind CHECK (
        (kind = 'password_reset' AND reset_selector IS NOT NULL AND octet_length(reset_selector) = 16)
        OR (kind = 'password_changed' AND reset_selector IS NULL)
    ),
    CONSTRAINT auth_mail_outbox_locale CHECK (locale IN ('en', 'zh-CN')),
    CONSTRAINT auth_mail_outbox_attempt_nonnegative CHECK (attempt_count >= 0 AND attempt_count <= 20),
    CONSTRAINT auth_mail_outbox_lease_pair CHECK ((lease_token IS NULL) = (lease_expires_at IS NULL)),
    CONSTRAINT auth_mail_outbox_terminal_state CHECK (
        ((sent_at IS NOT NULL)::integer + (canceled_at IS NOT NULL)::integer + (dead_at IS NOT NULL)::integer) <= 1
    ),
    CONSTRAINT auth_mail_outbox_error_code CHECK (
        last_error_code IS NULL OR last_error_code IN ('temporary', 'permanent', 'expired', 'superseded', 'invalid_reset', 'dependency')
    ),
    CONSTRAINT auth_mail_outbox_expiry_after_creation CHECK (expires_at > created_at),
    CONSTRAINT auth_mail_outbox_lease_after_available CHECK (lease_expires_at IS NULL OR lease_expires_at > available_at)
);

CREATE INDEX auth_mail_outbox_available_idx
    ON auth_mail_outbox (available_at, created_at)
    WHERE sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL;

CREATE INDEX auth_mail_outbox_retention_idx
    ON auth_mail_outbox (sent_at, canceled_at, dead_at);
