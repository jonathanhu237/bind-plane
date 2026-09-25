-- A downgrade cannot represent retained creators of live invitations. Refuse
-- rather than delete invitations or resurrect accounts to satisfy the old FK.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM auth_deleted_user_identities) THEN
        RAISE EXCEPTION 'Cannot downgrade user lifecycle after account deletion without losing historical identities';
    END IF;
    IF EXISTS (SELECT 1 FROM auth_user_invitations WHERE created_by IS NULL) THEN
        RAISE EXCEPTION 'Cannot downgrade user lifecycle while invitations have deleted creators';
    END IF;
    IF EXISTS (SELECT 1 FROM auth_users WHERE disabled_at IS NOT NULL) THEN
        RAISE EXCEPTION 'Cannot downgrade user lifecycle while users are disabled';
    END IF;
END $$;
DELETE FROM auth_sessions WHERE disabled_revoked;
ALTER TABLE auth_sessions DROP COLUMN disabled_revoked;
ALTER TABLE auth_user_invitations DROP CONSTRAINT auth_user_invitations_created_by_fkey,
    DROP COLUMN created_by_name, DROP COLUMN created_by_email, DROP COLUMN created_by_user_key,
    ALTER COLUMN created_by SET NOT NULL,
    ADD CONSTRAINT auth_user_invitations_created_by_fkey FOREIGN KEY (created_by) REFERENCES auth_users(id) ON DELETE RESTRICT;
UPDATE auth_operation_logs l SET actor_user_id=NULL WHERE actor_user_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM auth_users u WHERE u.id=l.actor_user_id);
ALTER TABLE auth_operation_logs ADD CONSTRAINT auth_operation_logs_actor_user_id_fkey FOREIGN KEY (actor_user_id) REFERENCES auth_users(id) ON DELETE SET NULL;
DROP TABLE auth_deleted_user_identities;
DROP INDEX auth_users_disabled_idx;
ALTER TABLE auth_users DROP COLUMN disabled_at;
