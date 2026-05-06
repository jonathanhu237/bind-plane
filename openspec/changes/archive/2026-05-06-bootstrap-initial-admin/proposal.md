## Why

Bind Plane currently requires a separate CLI command to create the first admin, which makes a fresh deployment start in an unusable state unless the operator already knows the bootstrap command. The application should be able to initialize its first administrator from deployment configuration on FastAPI startup.

## What Changes

- Add FastAPI startup bootstrap for the initial admin account.
- Load the initial admin username, password, and display name from `.env`/settings.
- Fail FastAPI startup when no admin exists and the initial admin settings are incomplete or invalid.
- Do not bootstrap from the worker process.
- Do not run Alembic migrations automatically during startup.
- Do not synchronize or override users from `.env` after any admin already exists.
- Remove the `create-admin` CLI entry point and document `.env` bootstrap as the only product path for initial admin creation.

## Capabilities

### New Capabilities
- `application-bootstrap`: FastAPI startup requirements for initializing the first admin from environment-backed settings.

### Modified Capabilities
- `ipv4-binding-release`: Clarify that normal accounts are admin-created after the initial admin is bootstrapped, with no public registration or account CLI path.

## Impact

- Affected backend code: settings, FastAPI application startup/lifespan, admin user bootstrap service, CLI entry points, and tests.
- Affected docs/config: `.env.example`, README/local startup instructions, and OpenSpec account requirements.
- No frontend API contract changes, database schema changes, worker behavior changes, or new dependencies are expected.
