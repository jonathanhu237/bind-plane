CREATE TABLE auth_setup (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    token_digest bytea,
    token_expires_at timestamptz,
    completed_at timestamptz,
    CONSTRAINT auth_setup_token_pair CHECK ((token_digest IS NULL) = (token_expires_at IS NULL)),
    CONSTRAINT auth_setup_completed_token CHECK (completed_at IS NULL OR (token_digest IS NULL AND token_expires_at IS NULL))
);

INSERT INTO auth_setup (singleton) VALUES (true);

CREATE TABLE auth_users (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    name text NOT NULL,
    email text NOT NULL,
    email_canonical text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT auth_users_name_length CHECK (char_length(name) BETWEEN 1 AND 100),
    CONSTRAINT auth_users_email_length CHECK (octet_length(email) BETWEEN 1 AND 254),
    CONSTRAINT auth_users_canonical_length CHECK (octet_length(email_canonical) BETWEEN 1 AND 254),
    CONSTRAINT auth_users_canonical_lower CHECK (email_canonical = lower(email_canonical))
);
