ALTER TABLE auth_rate_limit_buckets
    DROP CONSTRAINT IF EXISTS auth_rate_limit_namespace,
    DROP CONSTRAINT IF EXISTS auth_rate_limit_kind;

DELETE FROM auth_rate_limit_buckets
WHERE namespace NOT IN ('login', 'password-reset')
   OR bucket_kind NOT IN ('global', 'email');

ALTER TABLE auth_rate_limit_buckets
    ADD CONSTRAINT auth_rate_limit_namespace CHECK (namespace IN ('login', 'password-reset')),
    ADD CONSTRAINT auth_rate_limit_kind CHECK (bucket_kind IN ('global', 'email'));
