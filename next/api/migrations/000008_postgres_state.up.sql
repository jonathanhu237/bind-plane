CREATE TABLE auth_sessions (
    token_digest bytea PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    auth_version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    idle_expires_at timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    CONSTRAINT auth_sessions_token_digest_length CHECK (octet_length(token_digest) = 32),
    CONSTRAINT auth_sessions_auth_version_positive CHECK (auth_version > 0),
    CONSTRAINT auth_sessions_last_seen_after_creation CHECK (last_seen_at >= created_at),
    CONSTRAINT auth_sessions_idle_expiry_after_last_seen CHECK (idle_expires_at > last_seen_at),
    CONSTRAINT auth_sessions_absolute_expiry_after_creation CHECK (absolute_expires_at > created_at),
    CONSTRAINT auth_sessions_idle_before_absolute CHECK (idle_expires_at <= absolute_expires_at)
);

CREATE INDEX auth_sessions_user_valid_idx
    ON auth_sessions (user_id, idle_expires_at, absolute_expires_at, auth_version);

CREATE INDEX auth_sessions_cleanup_idx
    ON auth_sessions (idle_expires_at, absolute_expires_at);

CREATE TABLE auth_rate_limit_buckets (
    namespace text NOT NULL,
    bucket_kind text NOT NULL,
    bucket_digest bytea NOT NULL,
    tokens bigint NOT NULL,
    last_refill_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (namespace, bucket_kind, bucket_digest),
    CONSTRAINT auth_rate_limit_namespace CHECK (namespace IN ('login', 'password-reset')),
    CONSTRAINT auth_rate_limit_kind CHECK (bucket_kind IN ('global', 'email')),
    CONSTRAINT auth_rate_limit_digest_length CHECK (octet_length(bucket_digest) = 32),
    CONSTRAINT auth_rate_limit_tokens_nonnegative CHECK (tokens >= 0),
    CONSTRAINT auth_rate_limit_expiry_after_refill CHECK (expires_at > last_refill_at)
);

CREATE INDEX auth_rate_limit_cleanup_idx
    ON auth_rate_limit_buckets (expires_at);
