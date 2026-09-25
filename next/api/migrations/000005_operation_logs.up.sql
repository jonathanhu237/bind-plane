CREATE TABLE auth_operation_logs (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    actor_user_id uuid REFERENCES auth_users(id) ON DELETE SET NULL,
    actor_kind text,
    actor_label text,
    action text NOT NULL,
    object_type text NOT NULL,
    object_id text,
    result text NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    source_ip text,
    attempted_account text,
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT auth_operation_logs_result CHECK (result IN ('success', 'failure')),
    CONSTRAINT auth_operation_logs_actor_kind CHECK (actor_kind IS NULL OR actor_kind IN ('authenticated', 'unverified', 'system')),
    CONSTRAINT auth_operation_logs_action_length CHECK (char_length(action) BETWEEN 1 AND 120),
    CONSTRAINT auth_operation_logs_object_type_length CHECK (char_length(object_type) BETWEEN 1 AND 80),
    CONSTRAINT auth_operation_logs_details_object CHECK (jsonb_typeof(details) = 'object')
);

CREATE INDEX auth_operation_logs_time_idx
    ON auth_operation_logs (occurred_at DESC, id DESC);
CREATE INDEX auth_operation_logs_action_idx
    ON auth_operation_logs (action, occurred_at DESC, id DESC);
CREATE INDEX auth_operation_logs_actor_idx
    ON auth_operation_logs (actor_user_id, occurred_at DESC, id DESC);
CREATE INDEX auth_operation_logs_result_idx
    ON auth_operation_logs (result, occurred_at DESC, id DESC);

CREATE TABLE auth_operation_log_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton = true),
    retention_days integer NOT NULL DEFAULT 180,
    revision bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT auth_operation_log_settings_days CHECK (retention_days BETWEEN 1 AND 3650),
    CONSTRAINT auth_operation_log_settings_revision_positive CHECK (revision > 0)
);

INSERT INTO auth_operation_log_settings (singleton, retention_days, revision)
VALUES (true, 180, 1);
