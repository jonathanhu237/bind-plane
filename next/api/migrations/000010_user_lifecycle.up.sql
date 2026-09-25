ALTER TABLE auth_users ADD COLUMN disabled_at timestamptz;
ALTER TABLE auth_sessions ADD COLUMN disabled_revoked boolean NOT NULL DEFAULT false;

-- Historical identities are not accounts: no credentials, roles or restore path.
CREATE TABLE auth_deleted_user_identities (
    user_id uuid PRIMARY KEY,
    name text NOT NULL,
    email text NOT NULL,
    deleted_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- Audit identities must survive deletion, including inserts from in-flight requests.
ALTER TABLE auth_operation_logs DROP CONSTRAINT auth_operation_logs_actor_user_id_fkey;

ALTER TABLE auth_user_invitations
    ALTER COLUMN created_by DROP NOT NULL,
    DROP CONSTRAINT auth_user_invitations_created_by_fkey,
    ADD COLUMN created_by_name text NOT NULL DEFAULT '',
    ADD COLUMN created_by_email text NOT NULL DEFAULT '',
    ADD COLUMN created_by_user_key text NOT NULL DEFAULT '',
    ADD CONSTRAINT auth_user_invitations_created_by_fkey FOREIGN KEY (created_by) REFERENCES auth_users(id) ON DELETE SET NULL;

UPDATE auth_user_invitations i SET created_by_name=u.name, created_by_email=u.email, created_by_user_key=u.id::text FROM auth_users u WHERE i.created_by=u.id;
CREATE INDEX auth_users_disabled_idx ON auth_users(disabled_at);
