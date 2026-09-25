ALTER TABLE auth_operation_logs
    DROP COLUMN IF EXISTS actor_name,
    DROP COLUMN IF EXISTS actor_email;
