## 1. Project Foundation

- [x] 1.1 Create backend, frontend, worker, and shared configuration structure for the MVP stack
- [x] 1.2 Add runtime dependencies for FastAPI, PostgreSQL access, Redis jobs, password hashing, credential encryption, and Netmiko
- [x] 1.3 Add local development configuration for API, database, Redis, worker, and frontend
- [x] 1.4 Add baseline test, lint, and formatting commands

## 2. Persistence Model

- [x] 2.1 Implement user and role persistence with admin-created accounts
- [x] 2.2 Implement encrypted credential persistence separate from switch assets
- [x] 2.3 Implement switch asset and network/CIDR persistence with enabled and validation states
- [x] 2.4 Implement command profile persistence for prompts, commands, pagination, success/error patterns, and parsing rules
- [x] 2.5 Implement release job, job event, retry linkage, and audit persistence
- [x] 2.6 Implement import batch/report persistence for switch and network data

## 3. Authentication and Authorization

- [x] 3.1 Implement local username/password login with strong password hashing
- [x] 3.2 Implement session or token handling for API requests
- [x] 3.3 Enforce `operator` and `admin` permissions on release and admin routes
- [x] 3.4 Add admin user creation and password reset flow

## 4. Switch Resolution and Release Preparation

- [x] 4.1 Implement IPv4-only target validation that rejects IPv6, invalid addresses, and multiple targets
- [x] 4.2 Implement switch resolution using enabled validated networks and longest-prefix matching
- [x] 4.3 Stop normal release preparation when no switch matches or ambiguous same-prefix matches exist
- [x] 4.4 Implement pre-release query orchestration that records static, dynamic, missing, and unknown states
- [x] 4.5 Require reason selection and confirmation before job creation
- [x] 4.6 Implement admin-only forced release preparation for missing pre-query records

## 5. Netmiko and Worker Execution

- [x] 5.1 Spike Netmiko Telnet login, prompt detection, paging, config mode, query, and release behavior against representative switches
- [x] 5.2 Implement a switch session abstraction around Netmiko with timeout and error handling
- [x] 5.3 Implement command profile rendering and command output parsing
- [x] 5.4 Implement worker job execution phases for connect, query-before, release, query-after, classify, and persist
- [x] 5.5 Ensure worker decrypts credentials only during execution and never logs plaintext secrets
- [x] 5.6 Implement terminal job states for success, failure, timeout, and `needs_manual_confirmation`

## 6. Release Job API

- [x] 6.1 Implement release preparation API returning resolved switch and pre-query state
- [x] 6.2 Implement release job creation API returning a queued job identifier
- [x] 6.3 Implement job status API with phase, status, structured result, and permitted output
- [x] 6.4 Implement retry API that creates a new job linked to the original failed job
- [x] 6.5 Implement audit read API with admin and operator visibility rules

## 7. Minimal Admin APIs

- [x] 7.1 Implement credential management APIs for admin users
- [x] 7.2 Implement command profile management APIs for admin users
- [x] 7.3 Implement switch and network import APIs with validation reporting
- [x] 7.4 Implement user management APIs for admin users

## 8. Frontend

- [x] 8.1 Build login screen and authenticated app shell
- [x] 8.2 Build release console for IPv4 input, resolved switch display, pre-query state, reason selection, and confirmation
- [x] 8.3 Build job detail/status view with structured result and permitted raw output
- [x] 8.4 Build job history view for operators and admins
- [x] 8.5 Build minimal admin views for users, credentials, switch/network imports, command profiles, and audit logs

## 9. Verification

- [x] 9.1 Add unit tests for IPv4 validation and switch resolution edge cases
- [x] 9.2 Add unit tests for command profile parsing and result classification
- [x] 9.3 Add API tests for authentication, authorization, release preparation, forced release, job creation, retry, and audit visibility
- [x] 9.4 Add worker tests with mocked Netmiko sessions for success, failure, timeout, dynamic-after-release, and unparsable-output paths
- [x] 9.5 Add frontend workflow tests for the release console and job status flow
- [x] 9.6 Run OpenSpec validation and the project test suite
