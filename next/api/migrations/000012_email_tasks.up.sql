-- Durable email-task management. The previous outbox used account and
-- credential foreign keys to rebuild messages at send time; those dependencies
-- are deliberately removed so a task remains a fixed, retryable historical
-- message after an account, invitation, or credential changes.
ALTER TABLE auth_email_settings
    ADD COLUMN auto_retry_count integer NOT NULL DEFAULT 9,
    ADD COLUMN retention_days integer NOT NULL DEFAULT 30,
    ADD CONSTRAINT auth_email_settings_auto_retry_count_check CHECK (auto_retry_count BETWEEN 0 AND 100),
    ADD CONSTRAINT auth_email_settings_retention_days_check CHECK (retention_days BETWEEN 1 AND 3650);

ALTER TABLE auth_mail_outbox
    ALTER COLUMN user_id DROP NOT NULL,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_user_id_fkey,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_invitation_fk,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_email_change_request_id_fkey,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_authority_pair,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_kind,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_selector_by_kind,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_error_code,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_attempt_nonnegative,
    ADD COLUMN recipient_name text,
    ADD COLUMN system_name text NOT NULL DEFAULT 'Temvia',
    ADD COLUMN material_ciphertext bytea,
    ADD COLUMN submitted_by uuid,
    ADD COLUMN round_number integer NOT NULL DEFAULT 1,
    ADD COLUMN round_attempt_count integer NOT NULL DEFAULT 0,
    ADD COLUMN finished_at timestamptz;

-- Backfill the only terminal timestamp needed by the new retention policy.
UPDATE auth_mail_outbox
SET finished_at = COALESCE(sent_at, dead_at, canceled_at)
WHERE finished_at IS NULL AND (sent_at IS NOT NULL OR dead_at IS NOT NULL OR canceled_at IS NOT NULL);

ALTER TABLE auth_mail_outbox
    ADD CONSTRAINT auth_mail_outbox_kind CHECK (kind IN ('password_reset', 'password_changed', 'user_invitation', 'email_change_code', 'email_changed', 'test_email')),
    ADD CONSTRAINT auth_mail_outbox_selector_by_kind CHECK (
        (kind = 'password_reset' AND user_id IS NOT NULL AND invitation_id IS NULL AND reset_selector IS NOT NULL AND octet_length(reset_selector) = 16 AND email_change_request_id IS NULL AND email_change_selector IS NULL)
        OR (kind = 'password_changed' AND user_id IS NOT NULL AND invitation_id IS NULL AND reset_selector IS NULL AND email_change_request_id IS NULL AND email_change_selector IS NULL)
        OR (kind = 'user_invitation' AND user_id IS NULL AND invitation_id IS NOT NULL AND reset_selector IS NOT NULL AND octet_length(reset_selector) = 16 AND email_change_request_id IS NULL AND email_change_selector IS NULL)
        OR (kind = 'email_change_code' AND user_id IS NOT NULL AND invitation_id IS NULL AND reset_selector IS NULL AND email_change_selector IS NOT NULL AND octet_length(email_change_selector) = 16 AND email_change_request_id IS NOT NULL)
        OR (kind = 'email_changed' AND user_id IS NOT NULL AND invitation_id IS NULL AND reset_selector IS NULL AND email_change_request_id IS NULL AND email_change_selector IS NULL)
        OR (kind = 'test_email' AND user_id IS NULL AND invitation_id IS NULL AND reset_selector IS NULL AND email_change_request_id IS NULL AND email_change_selector IS NULL AND recipient_email IS NOT NULL)
    ),
    ADD CONSTRAINT auth_mail_outbox_error_code CHECK (
        last_error_code IS NULL OR last_error_code IN ('temporary', 'permanent', 'expired', 'superseded', 'invalid_reset', 'invalid_invitation', 'invalid_email_change', 'dependency')
    ),
    ADD CONSTRAINT auth_mail_outbox_attempt_nonnegative CHECK (attempt_count BETWEEN 0 AND 1000000),
    ADD CONSTRAINT auth_mail_outbox_round_check CHECK (round_number > 0 AND round_attempt_count >= 0 AND round_attempt_count <= 1000000),
    ADD CONSTRAINT auth_mail_outbox_recipient_check CHECK (recipient_email IS NULL OR (octet_length(recipient_email) BETWEEN 3 AND 254 AND position(chr(13) in recipient_email) = 0 AND position(chr(10) in recipient_email) = 0)),
    ADD CONSTRAINT auth_mail_outbox_recipient_name_check CHECK (recipient_name IS NULL OR (char_length(recipient_name) BETWEEN 1 AND 100 AND position(chr(13) in recipient_name) = 0 AND position(chr(10) in recipient_name) = 0)),
    ADD CONSTRAINT auth_mail_outbox_system_name_check CHECK (char_length(system_name) BETWEEN 1 AND 100 AND position(chr(13) in system_name) = 0 AND position(chr(10) in system_name) = 0),
    ADD CONSTRAINT auth_mail_outbox_finished_state_check CHECK (finished_at IS NULL OR sent_at IS NOT NULL OR dead_at IS NOT NULL OR canceled_at IS NOT NULL);

CREATE TABLE auth_mail_task_attempts (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    task_id uuid NOT NULL REFERENCES auth_mail_outbox(id) ON DELETE CASCADE,
    round_number integer NOT NULL,
    attempt_number integer NOT NULL,
    outcome text NOT NULL,
    error_code text,
    occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT auth_mail_task_attempts_round_check CHECK (round_number > 0 AND attempt_number > 0),
    CONSTRAINT auth_mail_task_attempts_outcome_check CHECK (outcome IN ('sent', 'failed')),
    CONSTRAINT auth_mail_task_attempts_error_check CHECK (error_code IS NULL OR error_code IN ('temporary', 'permanent', 'expired', 'superseded', 'invalid_reset', 'invalid_invitation', 'invalid_email_change', 'dependency')),
    CONSTRAINT auth_mail_task_attempts_success_error_check CHECK (outcome = 'failed' OR error_code IS NULL),
    UNIQUE (task_id, round_number, attempt_number)
);

CREATE INDEX auth_mail_outbox_tasks_created_idx
    ON auth_mail_outbox (created_at DESC, id DESC);
CREATE INDEX auth_mail_outbox_tasks_recipient_idx
    ON auth_mail_outbox (lower(recipient_email), created_at DESC, id DESC)
    WHERE recipient_email IS NOT NULL;
CREATE INDEX auth_mail_outbox_tasks_kind_idx
    ON auth_mail_outbox (kind, created_at DESC, id DESC);
CREATE INDEX auth_mail_outbox_tasks_finished_idx
    ON auth_mail_outbox (finished_at, id)
    WHERE finished_at IS NOT NULL;
CREATE INDEX auth_mail_outbox_tasks_submitted_by_idx
    ON auth_mail_outbox (submitted_by, created_at DESC, id DESC)
    WHERE submitted_by IS NOT NULL;
CREATE INDEX auth_mail_task_attempts_task_idx
    ON auth_mail_task_attempts (task_id, occurred_at, id);

-- The old expiry indexes remain useful to the bounded sweep. No task row is
-- linked to an authority table after this migration.
CREATE INDEX auth_mail_outbox_tasks_status_idx
    ON auth_mail_outbox (available_at, created_at, id)
    WHERE sent_at IS NULL AND dead_at IS NULL AND canceled_at IS NULL;
CREATE INDEX auth_mail_outbox_tasks_sending_idx
    ON auth_mail_outbox (lease_expires_at, created_at, id)
    WHERE sent_at IS NULL AND dead_at IS NULL AND canceled_at IS NULL AND lease_token IS NOT NULL;
CREATE INDEX auth_mail_outbox_tasks_sent_idx
    ON auth_mail_outbox (sent_at, created_at, id)
    WHERE sent_at IS NOT NULL;
CREATE INDEX auth_mail_outbox_tasks_dead_idx
    ON auth_mail_outbox (dead_at, created_at, id)
    WHERE dead_at IS NOT NULL;
CREATE INDEX auth_mail_outbox_tasks_canceled_idx
    ON auth_mail_outbox (canceled_at, created_at, id)
    WHERE canceled_at IS NOT NULL;
