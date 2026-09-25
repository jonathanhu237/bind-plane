DROP TABLE IF EXISTS auth_mail_outbox;
DROP TABLE IF EXISTS auth_password_resets;
ALTER TABLE auth_users
    DROP CONSTRAINT IF EXISTS auth_users_auth_version_positive,
    DROP COLUMN IF EXISTS auth_version;
