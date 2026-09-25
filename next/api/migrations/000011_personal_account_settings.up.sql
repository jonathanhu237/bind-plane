-- Personal account preferences and security authorities.
ALTER TABLE auth_users
    ADD COLUMN locale text,
    ADD CONSTRAINT auth_users_locale_check CHECK (locale IS NULL OR locale IN ('en', 'zh-CN'));

CREATE TABLE auth_user_avatars (
    user_id uuid PRIMARY KEY REFERENCES auth_users(id) ON DELETE CASCADE,
    media_type text NOT NULL DEFAULT 'image/png',
    image_bytes bytea NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT auth_user_avatars_media_type_check CHECK (media_type = 'image/png'),
    CONSTRAINT auth_user_avatars_version_check CHECK (version > 0),
    CONSTRAINT auth_user_avatars_size_check CHECK (octet_length(image_bytes) > 0 AND octet_length(image_bytes) <= 5242880)
);

-- A request is the independent authority for a pending email change. The
-- selector is public lookup data; verifier_digest is keyed and never contains
-- the six-digit code. A user has at most one active generation.
CREATE TABLE auth_email_change_requests (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id uuid NOT NULL UNIQUE REFERENCES auth_users(id) ON DELETE CASCADE,
    old_email text NOT NULL,
    new_email text NOT NULL,
    new_email_canonical text NOT NULL,
    selector bytea NOT NULL UNIQUE,
    verifier_digest bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    resend_after timestamptz NOT NULL,
    attempts_remaining integer NOT NULL DEFAULT 5,
    revision bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT auth_email_change_selector_size_check CHECK (octet_length(selector) = 16),
    CONSTRAINT auth_email_change_verifier_size_check CHECK (octet_length(verifier_digest) = 32),
    CONSTRAINT auth_email_change_attempts_check CHECK (attempts_remaining BETWEEN 0 AND 5),
    CONSTRAINT auth_email_change_revision_check CHECK (revision > 0),
    CONSTRAINT auth_email_change_email_check CHECK (
        octet_length(old_email) BETWEEN 3 AND 254
        AND octet_length(new_email) BETWEEN 3 AND 254
        AND octet_length(new_email_canonical) BETWEEN 3 AND 254
        AND position(chr(13) in old_email) = 0 AND position(chr(10) in old_email) = 0
        AND position(chr(13) in new_email) = 0 AND position(chr(10) in new_email) = 0
        AND position(chr(13) in new_email_canonical) = 0 AND position(chr(10) in new_email_canonical) = 0
        AND new_email_canonical = lower(new_email_canonical)
    ),
    CONSTRAINT auth_email_change_time_check CHECK (expires_at > created_at AND resend_after >= created_at)
);
CREATE INDEX auth_email_change_expires_idx ON auth_email_change_requests(expires_at);

-- Extend the durable outbox with recipient and authority snapshots. Existing
-- password-reset/invitation rows keep their old selector columns.
ALTER TABLE auth_mail_outbox
    ADD COLUMN recipient_email text,
    ADD COLUMN email_change_request_id uuid REFERENCES auth_email_change_requests(id) ON DELETE CASCADE,
    ADD COLUMN email_change_selector bytea;
ALTER TABLE auth_mail_outbox
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_kind,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_selector_by_kind,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_error_code;
ALTER TABLE auth_mail_outbox
    ADD CONSTRAINT auth_mail_outbox_kind CHECK (kind IN ('password_reset', 'password_changed', 'user_invitation', 'email_change_code', 'email_changed')),
    ADD CONSTRAINT auth_mail_outbox_selector_by_kind CHECK (
        (kind = 'password_reset' AND user_id IS NOT NULL AND invitation_id IS NULL AND reset_selector IS NOT NULL AND octet_length(reset_selector) = 16 AND email_change_request_id IS NULL AND email_change_selector IS NULL)
        OR (kind = 'password_changed' AND user_id IS NOT NULL AND invitation_id IS NULL AND reset_selector IS NULL AND email_change_request_id IS NULL AND email_change_selector IS NULL)
        OR (kind = 'user_invitation' AND user_id IS NULL AND invitation_id IS NOT NULL AND reset_selector IS NOT NULL AND octet_length(reset_selector) = 16 AND email_change_request_id IS NULL AND email_change_selector IS NULL)
        OR (kind = 'email_change_code' AND user_id IS NOT NULL AND invitation_id IS NULL AND reset_selector IS NULL AND email_change_selector IS NOT NULL AND octet_length(email_change_selector) = 16 AND email_change_request_id IS NOT NULL AND recipient_email IS NOT NULL)
        OR (kind = 'email_changed' AND user_id IS NOT NULL AND invitation_id IS NULL AND reset_selector IS NULL AND email_change_request_id IS NULL AND email_change_selector IS NULL AND recipient_email IS NOT NULL)
    ),
    ADD CONSTRAINT auth_mail_outbox_error_code CHECK (
        last_error_code IS NULL OR last_error_code IN ('temporary', 'permanent', 'expired', 'superseded', 'invalid_reset', 'invalid_invitation', 'invalid_email_change', 'dependency')
    ),
    ADD CONSTRAINT auth_mail_outbox_recipient_email_check CHECK (recipient_email IS NULL OR (octet_length(recipient_email) BETWEEN 3 AND 254 AND position(chr(13) in recipient_email) = 0 AND position(chr(10) in recipient_email) = 0));
CREATE INDEX auth_mail_outbox_email_change_idx ON auth_mail_outbox(email_change_request_id) WHERE email_change_request_id IS NOT NULL;

ALTER TABLE auth_rate_limit_buckets
    DROP CONSTRAINT IF EXISTS auth_rate_limit_namespace,
    ADD CONSTRAINT auth_rate_limit_namespace CHECK (namespace IN ('login', 'password-reset', 'password-reset-complete', 'setup', 'invitation-accept', 'invitation-send', 'test-email', 'email-change'));
