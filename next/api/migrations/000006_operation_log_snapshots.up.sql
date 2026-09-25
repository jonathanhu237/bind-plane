ALTER TABLE auth_operation_logs
    ADD COLUMN actor_name text,
    ADD COLUMN actor_email text;

-- Operation history is immutable. These columns preserve the actor identity
-- that was visible when the event happened even if the account is later
-- renamed or removed.
UPDATE auth_operation_logs AS l
SET actor_name = NULLIF(u.name, ''),
    actor_email = NULLIF(u.email, '')
FROM auth_users AS u
WHERE u.id = l.actor_user_id;
