## Why

Network operators need a controlled web workflow for releasing IPv4 static IP-MAC bindings without manually logging into switches for each operation. This change establishes the first usable vertical slice: resolve the responsible switch, run the release as an auditable background job, and report the before/after state.

## What Changes

- Add a single-IP IPv4 static binding release workflow for authorized users.
- Add local username/password authentication with admin-created accounts.
- Add operator/admin authorization boundaries for normal and forced release paths.
- Add switch resolution from validated local switch and network data.
- Add background release jobs so Telnet command execution does not block HTTP requests.
- Add command profiles for switch-specific query, release, prompt, pagination, and parsing rules.
- Add audit records containing operator, target IP, switch, reason, force flag, job status, and raw before/after output.
- Add minimal admin paths for user, credential, switch/network import, command profile, and audit management.

## Capabilities

### New Capabilities

- `ipv4-binding-release`: Single IPv4 static IP-MAC binding release workflow, including switch resolution, confirmation, background execution, result classification, forced release rules, and auditability.

### Modified Capabilities

- None.

## Impact

- New backend API surface for authentication, release preparation, release jobs, job status, and admin data management.
- New persistence model for users, roles, credentials, switches, networks, command profiles, release jobs, imports, and audit logs.
- New worker process for credential decryption, Netmiko/Telnet switch sessions, command execution, parsing, and result persistence.
- New frontend release console and minimal admin views.
- New dependencies are expected for FastAPI, PostgreSQL access, password hashing, credential encryption, background jobs, and Netmiko.
