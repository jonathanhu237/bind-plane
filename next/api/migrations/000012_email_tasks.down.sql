-- Tasks introduced by this migration cannot be represented by the legacy
-- selector contract. Refuse a destructive downgrade before dropping attempt
-- history or relaxing any of the new constraints.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM auth_mail_outbox AS o
        WHERE o.kind = 'test_email'
           OR o.material_ciphertext IS NOT NULL
           OR o.submitted_by IS NOT NULL
           OR o.recipient_name IS NOT NULL
           OR o.system_name <> 'Temvia'
           OR o.attempt_count > 20
           OR o.round_number <> 1
           OR o.round_attempt_count <> 0
           OR o.finished_at IS NOT NULL
           OR (o.user_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM auth_users AS u WHERE u.id = o.user_id))
           OR (o.invitation_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM auth_user_invitations AS i WHERE i.id = o.invitation_id))
           OR (o.email_change_request_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM auth_email_change_requests AS e WHERE e.id = o.email_change_request_id))
           OR EXISTS (SELECT 1 FROM auth_mail_task_attempts)
    ) THEN
        RAISE EXCEPTION 'cannot downgrade email-task migration while new task data exists';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM auth_email_settings
        WHERE auto_retry_count <> 9 OR retention_days <> 30
    ) THEN
        RAISE EXCEPTION 'cannot downgrade email-task migration while mail policy changes exist';
    END IF;
END
$$;

DROP INDEX IF EXISTS auth_mail_task_attempts_task_idx;
DROP INDEX IF EXISTS auth_mail_outbox_tasks_canceled_idx;
DROP INDEX IF EXISTS auth_mail_outbox_tasks_dead_idx;
DROP INDEX IF EXISTS auth_mail_outbox_tasks_sent_idx;
DROP INDEX IF EXISTS auth_mail_outbox_tasks_sending_idx;
DROP INDEX IF EXISTS auth_mail_outbox_tasks_status_idx;
DROP INDEX IF EXISTS auth_mail_outbox_tasks_submitted_by_idx;
DROP INDEX IF EXISTS auth_mail_outbox_tasks_finished_idx;
DROP INDEX IF EXISTS auth_mail_outbox_tasks_kind_idx;
DROP INDEX IF EXISTS auth_mail_outbox_tasks_recipient_idx;
DROP INDEX IF EXISTS auth_mail_outbox_tasks_created_idx;
DROP TABLE IF EXISTS auth_mail_task_attempts;

ALTER TABLE auth_mail_outbox
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_finished_state_check,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_system_name_check,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_recipient_name_check,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_recipient_check,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_round_check,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_attempt_nonnegative,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_error_code,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_selector_by_kind,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_kind;

ALTER TABLE auth_mail_outbox
    DROP COLUMN IF EXISTS finished_at,
    DROP COLUMN IF EXISTS round_attempt_count,
    DROP COLUMN IF EXISTS round_number,
    DROP COLUMN IF EXISTS submitted_by,
    DROP COLUMN IF EXISTS material_ciphertext,
    DROP COLUMN IF EXISTS system_name,
    DROP COLUMN IF EXISTS recipient_name;

ALTER TABLE auth_mail_outbox
    -- Migration 000003 made user_id nullable for invitation tasks; keep that
    -- shape when returning to the 000011 schema.
    ADD CONSTRAINT auth_mail_outbox_user_id_fkey FOREIGN KEY (user_id) REFERENCES auth_users(id) ON DELETE CASCADE,
    ADD CONSTRAINT auth_mail_outbox_invitation_fk FOREIGN KEY (invitation_id) REFERENCES auth_user_invitations(id) ON DELETE CASCADE,
    ADD CONSTRAINT auth_mail_outbox_email_change_request_id_fkey FOREIGN KEY (email_change_request_id) REFERENCES auth_email_change_requests(id) ON DELETE CASCADE,
    ADD CONSTRAINT auth_mail_outbox_authority_pair CHECK ((user_id IS NULL) <> (invitation_id IS NULL)),
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
    ADD CONSTRAINT auth_mail_outbox_attempt_nonnegative CHECK (attempt_count >= 0 AND attempt_count <= 20);

ALTER TABLE auth_email_settings
    DROP CONSTRAINT IF EXISTS auth_email_settings_retention_days_check,
    DROP CONSTRAINT IF EXISTS auth_email_settings_auto_retry_count_check,
    DROP COLUMN IF EXISTS retention_days,
    DROP COLUMN IF EXISTS auto_retry_count;
