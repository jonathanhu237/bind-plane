-- Refuse to downgrade while this migration owns durable user media or a
-- pending security authority. Silently dropping either would invalidate user
-- expectations and cannot be recovered from operation history.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM auth_user_avatars) THEN
        RAISE EXCEPTION 'Cannot downgrade personal account settings while avatars exist';
    END IF;
    IF EXISTS (SELECT 1 FROM auth_email_change_requests) THEN
        RAISE EXCEPTION 'Cannot downgrade personal account settings while email changes are pending';
    END IF;
    IF EXISTS (SELECT 1 FROM auth_mail_outbox WHERE kind IN ('email_change_code', 'email_changed')) THEN
        RAISE EXCEPTION 'Cannot downgrade personal account settings while email-change notifications exist';
    END IF;
    IF EXISTS (SELECT 1 FROM auth_users WHERE locale IS NOT NULL) THEN
        RAISE EXCEPTION 'Cannot downgrade personal account settings while account locales exist';
    END IF;
    IF EXISTS (SELECT 1 FROM auth_rate_limit_buckets WHERE namespace = 'email-change') THEN
        RAISE EXCEPTION 'Cannot downgrade personal account settings while email-change rate-limit state exists';
    END IF;
    IF EXISTS (SELECT 1 FROM auth_mail_outbox WHERE recipient_email IS NOT NULL) THEN
        RAISE EXCEPTION 'Cannot downgrade personal account settings while mail recipient snapshots exist';
    END IF;
    IF EXISTS (SELECT 1 FROM auth_mail_outbox WHERE last_error_code = 'invalid_email_change') THEN
        RAISE EXCEPTION 'Cannot downgrade personal account settings while email-change delivery state exists';
    END IF;
END $$;

ALTER TABLE auth_rate_limit_buckets
    DROP CONSTRAINT IF EXISTS auth_rate_limit_namespace;
ALTER TABLE auth_rate_limit_buckets
    ADD CONSTRAINT auth_rate_limit_namespace CHECK (namespace IN ('login', 'password-reset', 'password-reset-complete', 'setup', 'invitation-accept', 'invitation-send', 'test-email'));

ALTER TABLE auth_mail_outbox
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_kind,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_selector_by_kind,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_error_code,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_recipient_email_check;
ALTER TABLE auth_mail_outbox
    DROP COLUMN email_change_request_id,
    DROP COLUMN email_change_selector,
    DROP COLUMN recipient_email;
ALTER TABLE auth_mail_outbox
    ADD CONSTRAINT auth_mail_outbox_kind CHECK (kind IN ('password_reset', 'password_changed', 'user_invitation')),
    ADD CONSTRAINT auth_mail_outbox_selector_by_kind CHECK (
        (kind = 'password_reset' AND user_id IS NOT NULL AND invitation_id IS NULL AND reset_selector IS NOT NULL AND octet_length(reset_selector) = 16)
        OR (kind = 'password_changed' AND user_id IS NOT NULL AND invitation_id IS NULL AND reset_selector IS NULL)
        OR (kind = 'user_invitation' AND user_id IS NULL AND invitation_id IS NOT NULL AND reset_selector IS NOT NULL AND octet_length(reset_selector) = 16)
    ),
    ADD CONSTRAINT auth_mail_outbox_error_code CHECK (
        last_error_code IS NULL OR last_error_code IN ('temporary', 'permanent', 'expired', 'superseded', 'invalid_reset', 'invalid_invitation', 'dependency')
    );

DROP INDEX IF EXISTS auth_mail_outbox_email_change_idx;
DROP INDEX IF EXISTS auth_email_change_expires_idx;
DROP TABLE auth_email_change_requests;
DROP TABLE auth_user_avatars;
ALTER TABLE auth_users
    DROP CONSTRAINT auth_users_locale_check,
    DROP COLUMN locale;
