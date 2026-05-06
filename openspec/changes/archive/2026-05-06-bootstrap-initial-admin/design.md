## Context

Bind Plane accounts are stored in PostgreSQL and passwords are stored as one-way hashes. Today the only path for creating the first admin is the `bind-plane create-admin` CLI. That leaves a fresh deployment unable to log in unless the operator already knows to run the CLI.

The project owner wants the product path to be simpler: the first FastAPI startup should create the initial admin from `.env` settings, and all later account management should happen through the web admin UI. The initial admin password is treated as the actual initial password, not a forced-change temporary password.

## Goals / Non-Goals

**Goals:**
- Add settings for initial admin username, password, and display name.
- Create the first admin during FastAPI startup when no admin exists.
- Fail FastAPI startup when no admin exists and bootstrap settings are missing or invalid.
- Leave existing admins untouched once any admin exists.
- Remove the account creation CLI from the installed product surface.
- Update `.env.example`, README, and tests for the new startup path.

**Non-Goals:**
- Do not bootstrap users from the worker process.
- Do not automatically run Alembic migrations from FastAPI startup.
- Do not use `.env` as a recurring user synchronization source.
- Do not add password recovery, reset CLI commands, public registration, SSO, or self-service flows.
- Do not add special multi-instance first-start coordination for MVP.

## Decisions

### Decision: Bootstrap only when no admin exists
FastAPI startup will check whether any active or inactive user has the `admin` role. If at least one admin exists, startup will not read or apply the initial admin settings beyond normal settings validation.

Alternative considered: synchronize the configured username/password on every startup. That would turn `.env` into a hidden password reset mechanism and conflict with the database being the source of truth after bootstrap.

### Decision: Missing bootstrap config fails startup
If no admin exists, startup requires `BIND_PLANE_INITIAL_ADMIN_USERNAME` and `BIND_PLANE_INITIAL_ADMIN_PASSWORD`. Missing or invalid values fail startup with an explicit error.

Alternative considered: warn and continue in development. The product owner rejected this because a running application without an admin is not useful.

### Decision: Initial password is not forced to change
The bootstrapped admin will be created with `must_change_password = false`. The `.env` password is the real initial password and must be treated as sensitive deployment configuration.

Alternative considered: force password change on first login. The product owner chose to keep `.env` as the official initial password.

### Decision: Password validation matches user APIs
The bootstrap password will use the same minimum length as user create/reset payloads: at least 8 characters. If the broader password policy changes later, bootstrap validation should change with the user schema policy.

### Decision: Username collision fails if no admin exists
If no admin exists but the configured username already belongs to a non-admin user, startup fails instead of upgrading that user or choosing a different username.

Alternative considered: automatically promote the existing user. That is an implicit privilege escalation path and makes recovery harder to reason about.

### Decision: FastAPI only
Bootstrap is attached to FastAPI startup. The worker will not create or check admins because it is a background command executor, not the login/control plane.

### Decision: No automatic migration
Startup assumes migrations have already been applied. If the user tables are missing, startup should fail and point operators toward `uv run alembic upgrade head`.

### Decision: Remove account CLI
The `bind-plane create-admin` CLI and installed script entry point will be removed. If the only admin password is lost, MVP recovery is database maintenance or rebuilding the local environment.

## Risks / Trade-offs

- [Risk] A forgotten initial admin password has no app-level recovery path. -> This is accepted for MVP; administrators can recover through direct database maintenance or environment rebuild.
- [Risk] Startup failure can block local development if `.env` is missing. -> `.env.example` and README will make the required bootstrap values explicit.
- [Risk] Bootstrapping at startup can fail before API docs are visible. -> Error messages and tests should make missing configuration clear.
- [Risk] Removing the CLI eliminates a convenient local escape hatch. -> This matches the desired product surface and keeps account creation in one path.

## Migration Plan

1. Add initial admin settings and validation helpers.
2. Add a backend bootstrap service that checks for existing admins and creates the initial admin when required.
3. Hook the bootstrap service into FastAPI startup/lifespan after settings are loaded.
4. Remove the account creation CLI entry point.
5. Update `.env.example` and README startup instructions.
6. Add tests for bootstrap behavior and FastAPI startup wiring.
7. Run backend/frontend tests, lint, OpenSpec validation, and required subagent verification.

Rollback strategy: revert the change commit. Existing admin rows created by bootstrap can remain valid user records.

## Open Questions

No open product questions remain. The project owner selected the strict startup behavior, `.env` variable names, password semantics, FastAPI-only scope, no auto-migration, no concurrency handling, and CLI removal.
