-- Release evidence and execution queue are independent of Temvia's best-effort operation history.
INSERT INTO auth_roles (name, name_canonical, description)
VALUES ('Operator', 'operator', 'Can perform normal IP binding releases.'),
       ('Admin', 'admin', 'Can administer bind-plane and force a release.');
INSERT INTO auth_role_permissions (role_id, permission_key)
SELECT id, permission_key FROM auth_roles CROSS JOIN (VALUES
    ('online-users.read'), ('online-users.write'), ('users.read'), ('users.write'),
    ('roles.read'), ('roles.write'), ('invitations.read'), ('invitations.write'),
    ('settings.read'), ('settings.write'), ('operation-logs.read'),
    ('mail-tasks.read'), ('mail-tasks.write')
) AS permissions(permission_key) WHERE name_canonical='admin';

CREATE FUNCTION bp_protect_roles() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.name_canonical IN ('operator', 'admin') THEN
        RAISE EXCEPTION 'bind-plane business roles cannot be changed';
    END IF;
    RETURN OLD;
END $$;
CREATE TRIGGER bp_protect_roles_update BEFORE UPDATE OR DELETE ON auth_roles
FOR EACH ROW EXECUTE FUNCTION bp_protect_roles();

CREATE TABLE bp_credentials (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text UNIQUE NOT NULL,
    username text NOT NULL, encrypted_password text NOT NULL, encrypted_secret text,
    is_active boolean NOT NULL DEFAULT true, description text
);
CREATE TABLE bp_command_profiles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text UNIQUE NOT NULL,
    login_prompt_patterns jsonb NOT NULL DEFAULT '{}', command_templates jsonb NOT NULL DEFAULT '{}',
    prompt_patterns jsonb NOT NULL DEFAULT '{}', pagination_rules jsonb NOT NULL DEFAULT '{}',
    success_patterns jsonb NOT NULL DEFAULT '[]', error_patterns jsonb NOT NULL DEFAULT '[]',
    parser_rules jsonb NOT NULL DEFAULT '{}', is_active boolean NOT NULL DEFAULT true
);
CREATE TABLE bp_switches (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL,
    management_ip inet UNIQUE NOT NULL, credential_id uuid NOT NULL REFERENCES bp_credentials(id) ON DELETE RESTRICT,
    command_profile_id uuid NOT NULL REFERENCES bp_command_profiles(id) ON DELETE RESTRICT,
    is_enabled boolean NOT NULL DEFAULT true
);
CREATE TABLE bp_networks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), switch_id uuid NOT NULL REFERENCES bp_switches(id) ON DELETE CASCADE,
    cidr cidr NOT NULL, is_enabled boolean NOT NULL DEFAULT true, is_validated boolean NOT NULL DEFAULT false,
    vlan text, description text
);
CREATE INDEX bp_networks_active_idx ON bp_networks(is_enabled, is_validated);
CREATE TABLE bp_release_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), target_ip inet NOT NULL,
    kind text NOT NULL CHECK (kind IN ('pre_release_query', 'release')),
    reason text NOT NULL, ticket_id text, force boolean NOT NULL DEFAULT false,
    status text NOT NULL, phase text NOT NULL,
    before_state jsonb NOT NULL DEFAULT '{}', after_state jsonb NOT NULL DEFAULT '{}', result jsonb NOT NULL DEFAULT '{}',
    raw_before_output text, raw_release_output text, raw_after_output text, error_message text,
    actor_id uuid REFERENCES auth_users(id) ON DELETE SET NULL, actor_email text NOT NULL,
    switch_id uuid NOT NULL REFERENCES bp_switches(id) ON DELETE RESTRICT,
    command_profile_id uuid NOT NULL REFERENCES bp_command_profiles(id) ON DELETE RESTRICT,
    retry_of_id uuid REFERENCES bp_release_jobs(id) ON DELETE RESTRICT,
    preparation_job_id uuid UNIQUE REFERENCES bp_release_jobs(id) ON DELETE RESTRICT,
    lease_token uuid, lease_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz, finished_at timestamptz,
    CONSTRAINT bp_jobs_ipv4_only CHECK (family(target_ip) = 4)
);
CREATE INDEX bp_jobs_queue_idx ON bp_release_jobs(created_at) WHERE status = 'queued';
CREATE INDEX bp_jobs_actor_idx ON bp_release_jobs(actor_id, created_at DESC);
CREATE TABLE bp_release_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    job_id uuid NOT NULL REFERENCES bp_release_jobs(id) ON DELETE RESTRICT,
    phase text NOT NULL, status text NOT NULL, message text,
    payload jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX bp_events_job_idx ON bp_release_events(job_id, created_at);
-- No automatic retention or cascading actor deletion for release evidence.
