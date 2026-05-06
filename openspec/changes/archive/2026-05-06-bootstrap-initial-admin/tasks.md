## 1. Settings and Bootstrap Service

- [x] 1.1 Add initial admin settings for username, password, and display name
- [x] 1.2 Implement a bootstrap service that detects existing admin users and creates the initial admin when required
- [x] 1.3 Ensure missing settings, short passwords, username collisions, and missing migrations fail with explicit errors

## 2. FastAPI Startup Integration

- [x] 2.1 Wire the bootstrap service into FastAPI startup/lifespan
- [x] 2.2 Ensure worker startup does not run admin bootstrap

## 3. Remove Account CLI Surface

- [x] 3.1 Remove the `create-admin` CLI command and installed console script
- [x] 3.2 Update README and `.env.example` for `.env`-driven initial admin bootstrap

## 4. Verification

- [x] 4.1 Add backend tests for bootstrap creation, existing-admin no-op, missing config, invalid password, username collision, and startup wiring
- [x] 4.2 Run backend tests, project lint, and OpenSpec validation
