CREATE TABLE auth_roles (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    system_key text UNIQUE,
    name text NOT NULL,
    name_canonical text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    revision bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT auth_roles_name_length CHECK (char_length(name) BETWEEN 1 AND 100),
    CONSTRAINT auth_roles_name_canonical_length CHECK (char_length(name_canonical) BETWEEN 1 AND 100),
    CONSTRAINT auth_roles_description_length CHECK (char_length(description) <= 500),
    CONSTRAINT auth_roles_revision_positive CHECK (revision > 0),
    CONSTRAINT auth_roles_system_key CHECK (system_key IS NULL OR system_key = 'super_admin')
);

CREATE TABLE auth_role_permissions (
    role_id uuid NOT NULL REFERENCES auth_roles(id) ON DELETE CASCADE,
    permission_key text NOT NULL,
    PRIMARY KEY (role_id, permission_key),
    CONSTRAINT auth_role_permissions_key_length CHECK (char_length(permission_key) BETWEEN 3 AND 100)
);

CREATE TABLE auth_user_roles (
    user_id uuid NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    role_id uuid NOT NULL REFERENCES auth_roles(id) ON DELETE RESTRICT,
    assigned_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (user_id, role_id)
);

INSERT INTO auth_roles (system_key, name, name_canonical, description)
VALUES ('super_admin', 'Super Admin', 'super admin', 'Full access to every permission in the live catalog.');

INSERT INTO auth_user_roles (user_id, role_id)
SELECT u.id, r.id
FROM auth_users AS u
CROSS JOIN auth_roles AS r
WHERE r.system_key = 'super_admin';

ALTER TABLE auth_mail_outbox
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_kind,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_selector_by_kind,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_error_code,
    ALTER COLUMN user_id DROP NOT NULL,
    ADD COLUMN invitation_id uuid;

CREATE TABLE auth_user_invitations (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    name text NOT NULL,
    email text NOT NULL,
    email_canonical text NOT NULL UNIQUE,
    selector bytea NOT NULL UNIQUE,
    verifier_digest bytea NOT NULL,
    locale text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_by uuid NOT NULL REFERENCES auth_users(id) ON DELETE RESTRICT,
    revision bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT auth_user_invitations_name_length CHECK (char_length(name) BETWEEN 1 AND 100),
    CONSTRAINT auth_user_invitations_email_length CHECK (octet_length(email) BETWEEN 1 AND 254),
    CONSTRAINT auth_user_invitations_canonical_lower CHECK (email_canonical = lower(email_canonical)),
    CONSTRAINT auth_user_invitations_selector_length CHECK (octet_length(selector) = 16),
    CONSTRAINT auth_user_invitations_digest_length CHECK (octet_length(verifier_digest) = 32),
    CONSTRAINT auth_user_invitations_locale CHECK (locale IN ('en', 'zh-CN')),
    CONSTRAINT auth_user_invitations_revision_positive CHECK (revision > 0),
    CONSTRAINT auth_user_invitations_expiry_after_creation CHECK (expires_at > created_at)
);

CREATE TABLE auth_invitation_roles (
    invitation_id uuid NOT NULL REFERENCES auth_user_invitations(id) ON DELETE CASCADE,
    role_id uuid NOT NULL REFERENCES auth_roles(id) ON DELETE RESTRICT,
    PRIMARY KEY (invitation_id, role_id)
);

ALTER TABLE auth_mail_outbox
    ADD CONSTRAINT auth_mail_outbox_kind CHECK (kind IN ('password_reset', 'password_changed', 'user_invitation')),
    ADD CONSTRAINT auth_mail_outbox_selector_by_kind CHECK (
        (kind = 'password_reset' AND user_id IS NOT NULL AND invitation_id IS NULL AND reset_selector IS NOT NULL AND octet_length(reset_selector) = 16)
        OR (kind = 'password_changed' AND user_id IS NOT NULL AND invitation_id IS NULL AND reset_selector IS NULL)
        OR (kind = 'user_invitation' AND user_id IS NULL AND invitation_id IS NOT NULL AND reset_selector IS NOT NULL AND octet_length(reset_selector) = 16)
    ),
    ADD CONSTRAINT auth_mail_outbox_invitation_fk FOREIGN KEY (invitation_id) REFERENCES auth_user_invitations(id) ON DELETE CASCADE,
    ADD CONSTRAINT auth_mail_outbox_authority_pair CHECK ((user_id IS NULL) <> (invitation_id IS NULL)),
    ADD CONSTRAINT auth_mail_outbox_error_code CHECK (
        last_error_code IS NULL OR last_error_code IN ('temporary', 'permanent', 'expired', 'superseded', 'invalid_reset', 'invalid_invitation', 'dependency')
    );

CREATE INDEX auth_user_invitations_expiry_idx
    ON auth_user_invitations (expires_at, created_at, id);

CREATE INDEX auth_invitation_roles_role_idx ON auth_invitation_roles (role_id);
CREATE INDEX auth_user_roles_role_idx ON auth_user_roles (role_id);
