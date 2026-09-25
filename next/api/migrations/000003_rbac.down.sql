DELETE FROM auth_mail_outbox WHERE kind = 'user_invitation';

ALTER TABLE auth_mail_outbox
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_authority_pair,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_invitation_fk,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_selector_by_kind,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_kind,
    DROP CONSTRAINT IF EXISTS auth_mail_outbox_error_code,
    DROP COLUMN IF EXISTS invitation_id,
    ALTER COLUMN user_id SET NOT NULL,
    ADD CONSTRAINT auth_mail_outbox_kind CHECK (kind IN ('password_reset', 'password_changed')),
    ADD CONSTRAINT auth_mail_outbox_selector_by_kind CHECK (
        (kind = 'password_reset' AND reset_selector IS NOT NULL AND octet_length(reset_selector) = 16)
        OR (kind = 'password_changed' AND reset_selector IS NULL)
    ),
    ADD CONSTRAINT auth_mail_outbox_error_code CHECK (
        last_error_code IS NULL OR last_error_code IN ('temporary', 'permanent', 'expired', 'superseded', 'invalid_reset', 'dependency')
    );

DROP TABLE IF EXISTS auth_invitation_roles;
DROP TABLE IF EXISTS auth_user_invitations;
DROP TABLE IF EXISTS auth_user_roles;
DROP TABLE IF EXISTS auth_role_permissions;
DROP TABLE IF EXISTS auth_roles;
