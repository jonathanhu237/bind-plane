## Context

`bind-plane` starts from an empty implementation and a documented product boundary: release IPv4 static IP-MAC bindings only. The first implementation must provide a reliable vertical slice for authorized operators while keeping Telnet device execution outside the HTTP request path.

The main uncertainty is switch interaction. The target environment requires Telnet for MVP, and switch vendors/models may differ in prompts, pagination, query output, and release command behavior. The design therefore treats device commands and parsing as data-driven command profiles, with Netmiko evaluated before falling back to lower-level `pexpect`.

## Goals / Non-Goals

**Goals:**

- Provide a single-IP release console for authorized operators.
- Resolve the responsible switch from validated local network data.
- Execute release commands through background jobs, not synchronous HTTP handlers.
- Record before/after state, raw command output, result classification, reason, operator, and force flag.
- Support admin-managed users, credentials, switches/networks, command profiles, imports, and audit review at a minimal MVP level.
- Keep Telnet execution isolated in worker code so future SSH support can reuse the session boundary.

**Non-Goals:**

- Creating IP-MAC bindings.
- IPv6/NDP operations.
- Batch release.
- Approval workflows.
- SSO.
- Public registration or self-service access.
- A full CMDB-style asset management product.

## Decisions

### Use FastAPI API plus a separate worker

The Web API will handle authentication, authorization, validation, release preparation, job creation, and job status reads. A worker will decrypt switch credentials, open Telnet sessions, run commands, parse output, and persist job results.

Alternatives considered:

- Execute Telnet in the request handler: simpler, but request timeouts and partial command execution would make results and audits unreliable.
- Build a full workflow engine: more flexible, but too heavy for MVP.

### Model release as an auditable job

Every command attempt will be represented by a release job with status, phase, operator, target IP, resolved switch, command profile, reason, raw output, structured result, and timestamps. Retrying a failed release creates a new job linked to the original.

Alternatives considered:

- Store only final audit rows: simpler, but loses progress, retry lineage, and failure context.
- Mutate the same job during retry: less data, but hides separate command attempts.

### Resolve switches from local validated network data

Operators will enter only the target IPv4 address in the normal workflow. The system will resolve the responsible switch using enabled switch/network records. Longest-prefix matching can handle nested networks; ambiguous same-prefix matches stop the workflow.

Alternatives considered:

- Ask operators to select a switch: operationally fragile and pushes topology knowledge into the UI.
- Query every switch to find an IP: slow, risky, and unnecessary when local topology data exists.

### Store credentials separately from switches

Switches will reference credential records. Credentials are encrypted at rest and decrypted only by the worker when executing jobs. Application user passwords use strong one-way password hashing.

Alternatives considered:

- Store credentials directly on switch rows: easier imports, but creates duplicated secrets and harder rotation.
- Use one global credential: simpler MVP, but does not match likely device-group differences.

### Use command profiles for commands and parsing

Command profiles will own prompt rules, config-mode commands, single-IP query commands, static binding release commands, pagination handling, success/error patterns, and ARP entry parsing.

Alternatives considered:

- Hard-code vendor behavior: fast initially, but brittle when device models differ.
- Let admins write arbitrary scripts: powerful, but too risky for MVP.

### Evaluate Netmiko first for Telnet

Netmiko will be the first candidate for switch sessions because it already handles many network-device interaction patterns. If it cannot reliably support the target devices, the worker session layer can fall back to a narrower `pexpect` adapter without changing the API workflow.

Alternatives considered:

- Start with raw `pexpect`: maximum control, but more custom prompt/pagination code from day one.
- Require SSH: better security, but not viable for MVP target switches.

## Risks / Trade-offs

- Telnet behavior differs by device model -> Mitigation: run an early Netmiko spike and keep command profiles data-driven.
- Command output parsing may be incomplete -> Mitigation: store raw output and classify uncertain results as `needs_manual_confirmation`.
- Local network data may be stale or ambiguous -> Mitigation: only validated/enabled networks participate in resolution, and ambiguous matches stop normal release.
- Background jobs can get stuck during switch interaction -> Mitigation: use per-phase timeouts, job statuses, and terminal timeout states.
- Credential handling increases security risk -> Mitigation: encrypt credentials at rest, decrypt only in worker execution, and never log secrets.
- Minimal admin views may not cover every asset-editing need -> Mitigation: support import/report workflows first and defer full CMDB-style editing.
