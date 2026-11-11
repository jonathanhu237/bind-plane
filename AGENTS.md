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
- If the final state cannot be determined reliably, record the outcome as unknown (stored as `needs_manual_confirmation`, shown as 结果未知). Nobody confirms it by hand: releasing the address again queries the switch first and settles it either way.

## Non-Goals

Do not add these unless the project owner explicitly changes scope:

- Binding creation
- IPv6/NDP support
- Batch release
- Forced release (sending the release command when the switch has no record of the address)
- Choosing a switch by hand instead of resolving it
- Approval flows
- SSO
- Public registration
- Self-service workflows
- Watching whether switches are up or how they perform (SNMP polling, ping, port checks, CPU, memory, traffic), and sending alert notifications: the team runs Zabbix for these

## Zabbix

The switches are also in Zabbix, and the two must not overlap. Whatever can be learned by polling a switch belongs to Zabbix: whether it is up, its health and ports, their history, and telling people about problems. Bind Plane does the work that needs signing in to a switch or its own data: releasing bindings, backing up and comparing configurations, checking them against compliance rules, reading the release switches' networks, and the switch records, credentials and command profiles these need. Before adding a feature, check that Zabbix does not already do it.

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
4. Resolve the switch from the networks of the switches that handle releases.
5. Create a background release job.
6. Worker connects to the switch over Telnet.
7. Worker queries state before release. If the binding is not static, it stops without sending anything.
8. Worker executes the configured release command.
9. Worker queries state after release.
10. Store structured result, raw output, job status, and audit data.

There is no separate confirmation step: choosing Release starts the job, and the state before release is recorded with it.

An address can also be looked up without releasing it: a lookup job signs in to the address's switch, runs the single-IP query, records the MAC and whether the entry is a static binding, a dynamic entry or missing, and changes nothing. It needs no reason and takes no address lock, so it never holds up a release; who looked up what is kept like a release. A reply the rules cannot read is a failed lookup, never a guess. One release per address may be queued or running at a time. An earlier release with an unknown outcome does not hold the address.

## WeCom Bot

Members can also release from WeCom (企业微信), through a bot that admins set up on the WeCom page (bot ID, secret, connection address; the secret is sealed and never shown). The API keeps one long connection out to WeCom, so the server needs no public address; one bot allows one connection, so the sandbox and production each use their own.

- A member is bound by admins to a Bind Plane user by their WeCom account; that user's roles decide what they may do and the release is recorded as theirs, with its source `wecom`. Unbound members are told to ask an admin.
- An address answers with a card naming the switch and one button per reason; choosing a reason starts the release, as choosing Release does in the console. Only the member who asked may choose, once, within 30 minutes. "查询" with an address looks it up on its switch; a static binding comes back with the reasons, so it can be released from there.
- The result goes back to the same chat; a failed, timed-out or unknown outcome comes with Release again, which creates a new job linked to the original. The console's rules hold: one release per address at a time, same statuses and wording.

Reasons should be quick to select and include a temporary-test option. Ticket IDs are optional for MVP.

## Execution Architecture

Do not execute Telnet commands inside a blocking HTTP request.

Use a background job model:

- Web API handles authentication, authorization, input validation, job creation, and job status reads.
- Worker handles credential decryption, Telnet connection, command execution, output parsing, and result persistence.
- Every real command attempt must have an auditable job record.
- Releasing again after a failed, timed-out or unknown outcome must create a new job linked to the original.

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

Nobody selects a switch by hand. Resolve the switch from local asset and network data.

Only switches that handle releases (the gateways, marked 负责解绑) own networks, and nobody types one in: each network is an interface address in the switch's running configuration. The worker reads them as soon as a switch takes up handling releases, again every day, after each successful backup of it, and when someone presses 刷新网段. Reading networks is a device operation of its own; it does not depend on backups being scheduled.

A read never empties a switch's networks by mistake. It changes nothing unless the configuration ends the way a whole one does (`return` or `end`), and it keeps every network when it finds no interface address, or when a read nobody asked for would remove most of them (someone who asked may accept that). Every change it makes is recorded on the switch with the networks added and removed, and a failed read raises an alert.

If no switch matches, stop; the fix is to mark the address's gateway switch as handling releases, or to check why its networks were not read.

If multiple equally specific matches exist, stop; the fix is in the switches' configuration. Management VLANs that several switches share do this harmlessly, since nobody releases those addresses.

Longest-prefix matching is acceptable for overlapping networks. Ambiguous same-prefix matches must not silently choose one.

## Command Profiles

Command templates and parsing rules belong in command profiles, not hard-coded business logic.

A command profile speaks one vendor's commands, and a vendor can have several profiles (say, one per OS line or login protocol). A switch only uses a profile of its own vendor; the database holds the two together. Choosing a switch's vendor narrows its profiles, and a vendor with one active profile gets it without asking.

A new install starts with a profile for each of the campus's command dialects, so switches can be added straight away: H3C Comware, Huawei VRP (S series), Huawei CE, old Quidway, and Ruijie RGOS. They are ordinary profiles that people edit or copy; nothing marks them. Their commands come from the previous release system; their parsing rules are checked against real replies in `next/worker/testdata/arp_samples.json` (captured read-only, addresses replaced). A new sample of a reply goes into that file, with its addresses and MACs replaced, before a rule changes. The hidden `seed` column records which starter a profile began as, so a migration correcting a starter only touches copies nobody has edited (version 1).

When a reply names the address on a line the rules cannot read, the state is unknown, not missing: nothing is sent before release, and nothing is declared released after it.

Command profiles should support:

- Login and prompt matching rules
- Enter/exit config commands
- Single-IP ARP/binding query command
- Static binding release command
- Pagination handling
- Error patterns
- Success patterns
- IP, MAC, and entry-type parsing rules
- Where a configuration names its software version, when the built-in patterns (Huawei, H3C, Ruijie, Cisco) miss it (`parser_rules.os_version_patterns`); each backup and network read records it on the switch
- Static/dynamic/unknown classification values

If the output after a release command cannot be parsed confidently, store the raw output and record the outcome as unknown. If the state before release cannot be parsed, send nothing and fail the job: the binding is untouched, so the outcome is known.

## Audit

Every release job must record enough information to answer:

- Who initiated it
- When it started and finished
- Which IP was targeted
- Which switch was resolved or selected
- Which reason was selected
- What the before and after states were
- Which command profile was used
- Whether it succeeded, failed, timed out, or ended with an unknown outcome
- Raw before/after/command output needed for troubleshooting

Records name who acted: release jobs, asset events and device tasks keep the actor's name and email as they were at the time. Releases forced before forced release was removed keep `force = true` and their mark.

## Device Management

The devices page is the switch inventory that releases, backups, schedules, compliance and alerts all draw on. Device operations only read from switches; releasing a binding stays the one change Bind Plane makes.

- Switches are disabled, never deleted: release jobs, configuration versions and the audit log keep pointing at them.
- The connection test signs in once and out again and sends no command; a port that does not answer is a failed connection, not an alert. Schedules run only backups, never sign-in tests.
- Alerts are the problems only Bind Plane knows about: failed backups, compliance failures and failed network reads.
- Configurations can be viewed, compared and downloaded (one version, or the newest version of many switches as a zip); each of these is recorded in the switch's history before the configuration leaves. Configuration search looks through the newest version of every switch without signing in to any, and records each search with its text.
- A backup is overdue when the last successful one is more than seven days old. A release switch has no usable network until its networks have been read.
- Imports match existing switches by management IP; in update mode only filled-in cells change anything. An export imports back.
- A group that a schedule or compliance rule targets cannot be deleted, since the rule would widen to every switch.

Never log plaintext credentials, decrypted passwords, sensitive environment variables, or full connection strings.

## UI Direction

Everyone lands on the home page after login. It shows where things stand and starts no operations; it is not a marketing page or a generic dashboard:

- Releases whose outcome is unknown, and that no later release of the same address has followed, come first, each with a Release again (重新解绑) button; then recent jobs (the operator's own; everyone's for admins).
- Admins also see the device operations summary there; there is no separate overview page.
- A "New release" (发起解绑) button in its header opens the release console, so the release workflow stays one click away.

Every release starts in the release console or the WeCom bot; do not add another release form elsewhere. It is a focused page for one job at a time: address and reason, then the result; Query looks the address up instead, and a static binding it finds can be put back into the form to release. The job it shows is part of the URL, so a reload or a link from the history returns to it. Release history is its own page, with filters and pagination, listing releases or lookups. It only finds jobs: opening one shows it in the console, where Release again is.

Navigation stays small:

- Home
- Release: release console, release history
- Device operations (admins), what is used day to day: devices, configuration search, schedules, compliance, alerts, audit log
- Asset settings (admins), what is set up once and rarely changed: credentials, command profiles, networks, the WeCom bot. Switches and their groups live on the devices page. The networks page only shows the networks read from the release switches and looks an address up, saying which switch a release of it would go to, by the same rule releases use.
- User and system administration

The Chinese interface calls the action 解绑 everywhere: 解绑 IP in the sidebar, 发起解绑 on the buttons that open the console, 解绑 on the button that starts one, 重新解绑 on the button that releases an address again, 解绑控制台, 解绑记录. Never mix in 释放. The English interface and the code keep "release".

Release statuses say what happened on the switch. In Chinese they are 排队中, 进行中, 已解绑 (the static binding was deleted and is gone), 无绑定 (only a dynamic entry, so nothing was sent; 绑定 always means a static binding, and a dynamic entry is a 动态记录), 查无此 IP (the switch has no record of the address, so nothing was sent; check the address and its network's switch and command profile; not 无记录, which on the release history, 解绑记录, reads as "this row has no record"), 失败, 超时 and 结果未知 (a release command may have run, but whether the binding is gone is unknown; releasing again checks the switch first). 确认 means only a person confirming something, never a status.

Build a clear, restrained operator-focused tool. Do not block the main release workflow on a polished full admin system.

## Preferred Stack

Unless the project owner changes direction, assume:

- FastAPI backend
- PostgreSQL
- Redis with RQ or Dramatiq
- React/Vite frontend
- Worker-isolated Telnet sessions, with Netmiko evaluated first

Before adding framework-specific conventions, inspect the repository and follow any implementation style already established.
