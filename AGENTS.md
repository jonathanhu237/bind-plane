# AGENTS.md

Project instructions for agents and maintainers working on `bind-plane`.

## Scope

`bind-plane` releases IPv4 static IP-MAC bindings only.

Core semantics:

- The user enters one IPv4 address.
- The system resolves the responsible switch from local asset and network data.
- The operation deletes the static binding for that IP.
- The release is successful when the static binding is gone.
- A dynamic ARP entry after release can still be a successful result.
- If the final state cannot be determined reliably, store and display `needs_manual_confirmation`.

## Non-Goals

Do not add these unless the project owner explicitly changes scope:

- Binding creation
- IPv6/NDP support
- Batch release
- Approval flows
- SSO
- Public registration
- Self-service workflows

## Roles

Start with two roles:

- `operator`: can run the normal release workflow.
- `admin`: can also manage users, switch assets, credentials, command profiles, imports, and audit logs.

Accounts are created by admins. Do not implement open registration. Do not integrate SSO for MVP.

## Normal Workflow

The normal operator flow is:

1. Login.
2. Enter one IPv4 address.
3. Require a reason selection.
4. Resolve the switch from enabled and validated network data.
5. Create a background release job.
6. Worker connects to the switch over Telnet.
7. Worker queries state before release. If the binding is not static, it stops without sending anything.
8. Worker executes the configured release command.
9. Worker queries state after release.
10. Store structured result, raw output, job status, and audit data.

There is no separate confirmation step: choosing Release starts the job, and the state before release is recorded with it. One release per address may be queued or running at a time, and none while an earlier one needs manual confirmation.

Reasons should be quick to select and include a temporary-test option. Ticket IDs are optional for MVP.

## Forced Release

If the query before release finds no record, the release stops. An `admin` may instead start a forced release, which runs the release command even when no record is found. It is allowed only after automatic switch resolution or explicit switch selection.

Forced releases must require confirmation before the job is created, require a reason, store `force = true`, preserve raw before/after output, and be clearly marked in job details and audit logs.

## Execution Architecture

Do not execute Telnet commands inside a blocking HTTP request.

Use a background job model:

- Web API handles authentication, authorization, input validation, job creation, and job status reads.
- Worker handles credential decryption, Telnet connection, command execution, output parsing, and result persistence.
- Every real command attempt must have an auditable job record.
- Retrying a failed job must create a new job linked to the original.

Telnet is the MVP protocol because the initial target switches require it. Keep the session layer protocol-aware so SSH can be added later, but do not require SSH for MVP.

Evaluate Netmiko first for Telnet sessions. Use lower-level `pexpect` only if Netmiko cannot cover the required device interactions reliably.

## Data Model Concepts

Keep these concepts separate:

- User and role
- Switch asset
- Network/CIDR owned by a switch
- Credential
- Command profile
- Release job
- Audit log
- Import batch/report

Switches reference credentials and command profiles. Do not store plaintext credentials on switch records.

Credentials must be encrypted at rest. App user passwords must use a strong one-way hash such as Argon2id or bcrypt. Never treat base64 or any reversible encoding as password protection.

## Switch Resolution

Operators should not manually select switches in the normal flow. Resolve the switch from local asset and network data.

Only enabled and validated network data may participate in production resolution.

If no switch matches, stop the normal workflow.

If multiple equally specific matches exist, stop and require admin data correction.

Longest-prefix matching is acceptable for overlapping networks. Ambiguous same-prefix matches must not silently choose one.

## Command Profiles

Command templates and parsing rules belong in command profiles, not hard-coded business logic.

Command profiles should support:

- Login and prompt matching rules
- Enter/exit config commands
- Single-IP ARP/binding query command
- Static binding release command
- Pagination handling
- Error patterns
- Success patterns
- IP, MAC, and entry-type parsing rules
- Static/dynamic/unknown classification values

If the output after a release command cannot be parsed confidently, store the raw output and mark the result as needing manual confirmation. If the state before release cannot be parsed, send nothing and fail the job: the binding is untouched, so the address must not be held as an uncertain release.

## Audit

Every release job must record enough information to answer:

- Who initiated it
- When it started and finished
- Which IP was targeted
- Which switch was resolved or selected
- Whether it was forced
- Which reason was selected
- What the before and after states were
- Which command profile was used
- Whether it succeeded, failed, timed out, or needs manual confirmation
- Raw before/after/command output needed for troubleshooting

Records name who acted: release jobs, asset events and device tasks keep the actor's name and email as they were at the time.

Never log plaintext credentials, decrypted passwords, sensitive environment variables, or full connection strings.

## UI Direction

Everyone lands on the home page after login. It shows where things stand and starts no operations; it is not a marketing page or a generic dashboard:

- Releases whose outcome needs manual confirmation come first, then recent jobs (the operator's own; everyone's for admins).
- Admins also see the device operations summary there; there is no separate overview page.
- A "New release" (发起解绑) button in its header opens the release console, so the release workflow stays one click away.

Every release starts in the release console; do not add a second release form elsewhere. It is a focused page for one job at a time: address and reason, then the result. The job it shows is part of the URL, so a reload or a link from the history returns to it. Admin force release lives here. Release history is its own page, with filters and pagination.

Navigation stays small:

- Home
- Release: release console, release history
- Device operations (admins): devices, schedules, compliance, alerts, audit log, asset settings (credentials, command profiles, switches, networks)
- User and system administration

The Chinese interface calls the action 解绑 everywhere: 解绑 IP in the sidebar, 发起解绑 on the buttons that open the console, 解绑 on the button that starts one, 解绑控制台, 解绑记录, 强制解绑. Never mix in 释放. The English interface and the code keep "release".

Release statuses say what happened on the switch. In Chinese they are 排队中, 进行中, 已解绑 (the static binding was deleted and is gone), 无静态绑定 (only a dynamic entry, so nothing was sent), 无记录 (the switch has no record of the address, so nothing was sent; an admin may force a release), 失败, 超时 and 需人工核实 (a release command may have run, but the outcome is unknown). 确认 means only a person confirming something, never a status.

Build a clear, restrained operator-focused tool. Do not block the main release workflow on a polished full admin system.

## Preferred Stack

Unless the project owner changes direction, assume:

- FastAPI backend
- PostgreSQL
- Redis with RQ or Dramatiq
- React/Vite frontend
- Worker-isolated Telnet sessions, with Netmiko evaluated first

Before adding framework-specific conventions, inspect the repository and follow any implementation style already established.
