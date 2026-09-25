ALTER TABLE auth_rate_limit_buckets
    DROP CONSTRAINT IF EXISTS auth_rate_limit_namespace,
    DROP CONSTRAINT IF EXISTS auth_rate_limit_kind;

ALTER TABLE auth_rate_limit_buckets
    ADD CONSTRAINT auth_rate_limit_namespace CHECK (
        namespace IN (
            'login',
            'password-reset',
            'password-reset-complete',
            'invitation-accept',
            'invitation-send',
            'test-email',
            'setup'
        )
    ),
    ADD CONSTRAINT auth_rate_limit_kind CHECK (
        bucket_kind IN ('global', 'ip', 'email', 'actor', 'recipient')
    );
