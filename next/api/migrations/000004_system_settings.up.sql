-- The previous catalog called the invitation write capability
-- `invitations.manage` and supplied read dependencies at authorization time.
-- Migrate existing roles to the explicit permissions before the application
-- starts enforcing the independent read/write catalog.
INSERT INTO auth_role_permissions (role_id, permission_key)
SELECT role_id, 'invitations.read'
FROM auth_role_permissions
WHERE permission_key = 'invitations.manage'
ON CONFLICT DO NOTHING;

INSERT INTO auth_role_permissions (role_id, permission_key)
SELECT role_id, 'roles.read'
FROM auth_role_permissions
WHERE permission_key = 'invitations.manage'
ON CONFLICT DO NOTHING;

DELETE FROM auth_role_permissions AS legacy
USING auth_role_permissions AS current
WHERE legacy.role_id = current.role_id
  AND legacy.permission_key = 'invitations.manage'
  AND current.permission_key = 'invitations.write';

UPDATE auth_role_permissions
SET permission_key = 'invitations.write'
WHERE permission_key = 'invitations.manage';

CREATE TABLE auth_email_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton = true),
    smtp_host text NOT NULL,
    smtp_port integer NOT NULL,
    smtp_security text NOT NULL,
    smtp_username text NOT NULL DEFAULT '',
    smtp_password_ciphertext bytea,
    from_address text NOT NULL,
    from_name text NOT NULL,
    default_locale text NOT NULL,
    revision bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT auth_email_settings_port CHECK (smtp_port BETWEEN 1 AND 65535),
    CONSTRAINT auth_email_settings_security CHECK (smtp_security IN ('none', 'starttls', 'tls')),
    CONSTRAINT auth_email_settings_locale CHECK (default_locale IN ('en', 'zh-CN')),
    CONSTRAINT auth_email_settings_revision_positive CHECK (revision > 0),
    CONSTRAINT auth_email_settings_username_password CHECK (smtp_username = '' OR smtp_password_ciphertext IS NOT NULL),
    CONSTRAINT auth_email_settings_text_lengths CHECK (
        char_length(smtp_host) BETWEEN 1 AND 255
        AND char_length(from_name) BETWEEN 1 AND 200
        AND octet_length(from_address) BETWEEN 3 AND 320
    )
);
