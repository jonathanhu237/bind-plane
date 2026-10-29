# Bind Plane · Temvia migration preview

Releases and device operations are verified end to end against a simulated Telnet switch, not yet against a real one: validate on a lab switch before relying on it. Keep the legacy application running until explicit cutover. Docker deployment is described in [deploy/README.md](deploy/README.md).

Safety boundary: a dynamic pre-query record is not a static binding and cannot be released. An expired release-command lease becomes `needs_manual_confirmation`; new queries may be recorded, but further release commands for that IP are blocked. No automatic replay or in-app clearance is provided. Before any future manual reconciliation, an administrator must stop the old executor, verify its switch session is gone, inspect independent job evidence and switch state, and arrange a separately audited resolution. Do not just reset the job status to requeue it.

## Device operations

Admins (never Operators) manage the switch inventory and its operations:

- **Devices**: inventory with vendor, model, group, site, protocol (SSH or Telnet) and port; CSV import with a full dry run, which also accepts the Chinese headings of other inventory exports; reachability (a TCP connect to the management port) and backup status per switch.
- **Configuration backups**: the worker signs in, reads the running configuration and stores a new version only when it changed (volatile timestamp lines are ignored). Versions are sealed with AES-GCM under the credential key with a separate label, opened only by the API, and every view or diff is written to the audit log.
- **Schedules**: cron schedules (in `BIND_PLANE_TIMEZONE`, default Asia/Shanghai) for backups or reachability checks over all switches or one group; manual runs from the Devices page. A switch never has two pending operations of one kind. Runs are kept 30 days.
- **Compliance**: baseline rules (`must_contain` / `must_not_contain`, RE2 patterns, scoped by vendor or group) evaluated on each switch's latest backup whenever a backup or rule changes. RE2 runs in linear time, so no pattern can stall the evaluator.
- **Alerts**: unreachable switches, failed backups and critical compliance failures. They resolve themselves when the cause clears; acknowledging records who is handling it. Resolved alerts are kept 180 days.
- **Audit log**: every asset change, import, schedule or rule edit, manual run, configuration read and alert acknowledgement, kept indefinitely.

Device operations only read from switches, so a worker that dies mid-operation fails the item and the next run retries. Release jobs run in their own worker loop and are never delayed by a long backup run.

Asset edits are versioned. A job pins the switch and command-profile revision its pre-query observed: confirming after either changed, or after an auto-resolved address moved to another switch, is refused, and a queued job whose pinned revision no longer applies fails without contacting the switch. Operators query again. A failure before any switch connection (disabled or edited assets, unusable credentials or profile) is a definite `failed`, never `needs_manual_confirmation`, so it does not block later releases. Asset changes record which fields changed; secrets are recorded only as replaced or removed.

English | [简体中文](README.zh-CN.md)

An independent Go API and React admin. The admin uses Vite during development
and a pinned Caddy runtime in the production Compose stack. This source is
yours to change or remove.

## Requirements and verification

For the Compose route: Docker with Compose v2, Make, and Node.js 24 or later
(for the generator and portable secret generation). Start the Docker engine.
On macOS install Make using Xcode Command Line Tools if needed.
The release workflow exercises this route once on an Ubuntu runner with a
fresh Compose project, PostgreSQL volume, and Chromium browser. That is one CI
path, not a general Linux/WSL2 or production compatibility claim. The complete
local first-run route has been tested on macOS; native Windows PowerShell is
outside this route.

## First run

Copy the environment inventory and fill all five secret values before
starting the containers:

```sh
cp .env.example .env
chmod 600 .env
# edit .env and set POSTGRES_PASSWORD, PASSWORD_RESET_TOKEN_KEY, INVITATION_TOKEN_KEY, EMAIL_SETTINGS_ENCRYPTION_KEY and BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY
# node -e "console.log(require('node:crypto').randomBytes(32).toString('base64url'))"  # use the output for PASSWORD_RESET_TOKEN_KEY
# node -e "console.log(require('node:crypto').randomBytes(32).toString('base64url'))"  # generate a separate value for INVITATION_TOKEN_KEY
# node -e "console.log(require('node:crypto').randomBytes(32).toString('base64url'))"  # generate a separate value for EMAIL_SETTINGS_ENCRYPTION_KEY
# node -e "console.log(require('node:crypto').randomBytes(32).toString('base64url'))"  # generate a separate value for BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY
make build
make migrate-up
make up
```

A fresh PostgreSQL volume may take over a minute to initialize on a slow host.
The generated Compose file gives its real `pg_isready` healthcheck a bounded
90-second startup grace, then keeps the normal 2-second interval, 5-second
probe timeout, and 15-failure limit. `make migrate-up` waits for that readiness
and fails if PostgreSQL never becomes healthy; it does not bypass durability or
report a dummy success.

Run the Node command separately for each token key and use a different output
for every secret. The same command also produces a suitable random value for the
PostgreSQL password; do not reuse a value across variables or commit `.env`.

The API prints a temporary setup link in its logs while initialization is
incomplete. Open that link in the browser-visible admin origin. The token is
kept in the URL fragment only until the admin removes it before rendering; it
is sent only in the setup request body. Setup creates no session, so sign in
after the administrator form succeeds.

```sh
docker compose logs api
```

Open `http://localhost:5173` after the first setup, or use the URL printed by
`pnpm dev` when developing the admin separately. The browser always calls the
relative `/api` path through Vite or Caddy, so `APP_PUBLIC_URL` must match the
origin in the address bar exactly.

PostgreSQL stores accounts, sessions, and rate-limit state. Valid sessions survive
an API or database restart; expired and revoked state is checked at request time
and reclaimed by a bounded background cleanup. `make down` does not remove the
PostgreSQL volume.

The API has basic abuse protection enabled by default without Redis. Login uses
separate source-IP, canonical-email, and global token buckets; password-reset
requests use their own source-IP, email, and global buckets. Password-reset
completion, invitation acceptance, and first setup have independent source-IP
buckets. Invitation create/resend shares actor and recipient buckets, while test
mail has separate global, actor, and recipient buckets. A source bucket is
charged for a reached anonymous attempt; object or global denial does not drain
an accepted-operation bucket. Successful login clears only its email bucket.
All buckets refill automatically and are retained for a finite period; repeated
rejections do not extend the token recovery schedule. A real quota denial is
HTTP 429 (`rate_limited`); an unavailable or timed-out limiter is HTTP 503
(`dependency_unavailable`) and fails closed without clearing an existing session
cookie. These limits protect one API instance and are not DDoS or SMTP-provider
quota protection.

The related `*_RATE_LIMIT_*` environment variables in `.env.example` expose
capacity and refill interval for every bucket. Values must be positive, intervals
must be at least 1ms, and the calculated retention must fit the database
representation; invalid values stop startup. Configure `TRUSTED_PROXY_CIDRS`
only with the immediate proxy networks. Direct peers are used when it is empty,
and malformed or untrusted forwarding headers cannot change the source identity.
Run the bundled migrations, including the abuse-protection forward migration,
before starting the API. For a public deployment, put a maintained edge proxy
or WAF in front of the API for connection, request-body, and broad traffic
controls; that is additional protection, not a prerequisite for these buckets.

The shipped small-admin defaults are:

| Operation | Buckets (capacity; one-token refill; sustained refill) |
| --- | --- |
| Login | IP 30; 6s; 10/min · email 5; 1m; 1/min · global 60; 6s; 10/min |
| Password-reset request | IP 30; 6s; 10/min · email 3; 20m; 3/hour · global 10; 6s; 10/min |
| Password-reset completion | IP 20; 1m; 1/min |
| Invitation acceptance | IP 30; 1m; 1/min |
| First setup | IP 10; 1m; 1/min |
| Invitation create/resend | actor 20; 1m; 1/min · recipient 3; 20m; 3/hour |
| Test email | global 20; 1h; 1/hour · actor 5; 1h; 1/hour · recipient 3; 20m; 3/hour |

Capacities are burst tokens, not a promise of a fixed request count. Refill is
continuous in discrete interval steps and a bucket never exceeds capacity.
Changing a capacity clips existing excess tokens on the next request; changing
an interval applies the new recovery and finite-retention schedule then.

`SHUTDOWN_TIMEOUT` is the single graceful-shutdown budget. It defaults to 30s,
is read by the API, and is also used as Compose's `stop_grace_period`; the
budget starts at the first SIGINT/SIGTERM and includes a small internal exit
reserve. The API stops accepting new HTTP requests, drains in-flight requests
and already claimed mail in parallel, cancels maintenance, and closes the
database only after those workers finish. A timeout or a second termination
signal exits nonzero; invalid or non-positive values are rejected at startup.

Password recovery is handled by the API's PostgreSQL transactional outbox.
The request endpoint only commits reset state and returns; the in-process mail
dispatcher claims jobs with a short lease and sends them over SMTP afterwards.
There is no message broker or separate worker. Delivery is at-least-once: a
process interruption after SMTP accepts a message can produce a duplicate, and
the stable Message-ID plus idempotent reset authority make that safe. Temporary
failures retry with bounded exponential full-jitter backoff; expired jobs and
old sent/canceled/dead rows are cleaned automatically.

`make up` enables the development-only Mailpit profile. Inspect reset and
security-notification messages at [http://127.0.0.1:8025](http://127.0.0.1:8025)
(or the `MAILPIT_UI_PORT` you configure). Mailpit SMTP is private to the
Compose network on `mailpit:1025`; it is never published to the host. A normal
`docker compose up` without `--profile development` does not start Mailpit.
The API does not wait for SMTP readiness, so a provider outage leaves durable
outbox work to retry.

In production, configure SMTP security (`starttls` or `tls`), a real sender
address, and paired credentials when required by the provider in the System
settings page. System settings use one required, language-neutral System name;
the same name appears in the interface, browser title, and system emails
regardless of interface or email language. Keep `EMAIL_SETTINGS_ENCRYPTION_KEY`
stable and back it up separately from PostgreSQL; it protects the stored SMTP
password. Keep
`PASSWORD_RESET_TOKEN_KEY` stable across restarts. Deliberate key rotation
invalidates pending reset-mail jobs (already delivered links remain consumable
because PostgreSQL stores their digest); affected users can request a fresh
link. Keep `INVITATION_TOKEN_KEY` stable as well; it is intentionally separate
from the reset key. Invitation links expire after 72 hours by default and may
be configured with `INVITATION_LINK_TTL` up to seven days. Run all current migrations before deploying this API. For rollback, back up
the database and inspect each affected migration before choosing the matching
API version; blindly applying one down migration is not a general rollback plan.

For an application upgrade, use this stop-and-back-up sequence. Keep the
PostgreSQL service running while the API is stopped, and run each command only
after the previous one succeeds:

```sh
(
set -eu
umask 077

# 1. Stop the API before changing its image or schema.
docker compose stop api

# 2. Use a private, persistent directory outside the project, not /tmp.
backup_dir="$(mktemp -d "${HOME:?}/temvia-backup.XXXXXX")"
chmod 700 "$backup_dir"
backup_file="$backup_dir/temvia-$(date -u +%Y%m%dT%H%M%SZ).sql"
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB"' > "$backup_file.partial"
chmod 600 "$backup_file.partial"
mv "$backup_file.partial" "$backup_file"
printf 'Backup written to %s\n' "$backup_file"

# 3. Build fresh API, admin, and migration images from the updated source.
make build

# 4. Apply all forward migrations explicitly. Failure stops this subshell.
make migrate-up

# 5. Start the rebuilt services only after migration succeeds.
docker compose up -d api admin
)
```

The subshell stops on any failed step without closing your interactive shell.
A `.partial` dump indicates a failed backup and must not be used for restore.
Keep successful backups on persistent storage and copy them to your protected
off-host backup storage; the example does not automate backup retention.

Back up `.env`, `EMAIL_SETTINGS_ENCRYPTION_KEY`, `PASSWORD_RESET_TOKEN_KEY`,
and `INVITATION_TOKEN_KEY` separately in a protected secret store before the
upgrade; never commit those values or place them in a public backup. A failed
build or migration must stop the procedure: inspect the error and restore the
database and matching images according to your rollback plan rather than
starting a schema-incompatible API. Do not use `docker compose down -v`; the
PostgreSQL volume contains the application data.

## Deactivate, reactivate, and delete users

The user list includes active and disabled accounts and supports status filtering.
User read access permits viewing users; user write access permits deactivating,
reactivating, or deleting any other user, including Super Admins. Existing role-grant
restrictions remain unchanged. You cannot deactivate or delete yourself, and
lifecycle actions and role changes must leave at least one active Super Admin.
The API enforces these rules transactionally and requires the displayed user version;
after a conflict, reload the user before confirming again.

Deactivation immediately invalidates all existing sessions and blocks sign-in and
password recovery while retaining the email, password, and assigned roles. Roles may
still be edited and remain in use while assigned to disabled users. Reactivation
requires a fresh sign-in; old sessions and reset links never become valid again.
Previously signed-in users receive a deactivation message when their session is
checked. Anonymous sign-in and recovery responses do not disclose account status.

Deletion requires individual confirmation and the target email. It is irreversible
and does not require prior deactivation. Credentials, sessions, reset authority, and
role assignments are removed. Associated records retain the original name, email,
and user ID, with a Deleted user indicator. Operation history follows its existing
retention policy; minimal historical identity records contain no credentials.
Account deletion is not a promise to erase all personal information. The email may
be invited again, but the new user gets a new identity and no previous credentials,
roles, or historical attribution.

Invitations remain valid when their creator is disabled or deleted, subject to their
existing expiry and authorization rules. New business modules should explicitly
retain necessary associations rather than cascade-delete business records.

Apply all migrations before starting the new API. Downgrading the lifecycle migration
refuses while disabled users or deleted identities exist, rather than restoring
access or silently losing history. Plan rollback against actual data; do not clear
data to bypass these protections.

## Operation history

Administrators with the `operation-logs.read` permission can open Operation
history from the authenticated navigation. The page supports time, actor UUID,
action, result, object type, and object ID filters, cursor pagination, and a
localized detail view. Successful and failed in-scope actions each produce one
best-effort result record; a storage outage can leave a gap. History is retained
for 180 days by default. Super Admins or users with `settings.write` can change
the value between 1 and 3650 days in System settings; cleanup runs hourly and
uses a bounded database operation.

`Source IP` records the direct peer address by default. In a reverse-proxy
deployment, set `TRUSTED_PROXY_CIDRS` to the proxy's immediate peer network so
the API can safely use its `X-Forwarded-For` or `X-Real-IP` value. Headers from
untrusted peers are ignored. Never include a public client network in this
setting.

## Requirements

- Go 1.27 or later for `api/`.
- Node.js 24 or later and pnpm 11.24.0 for `admin/`.

## API

```sh
cd api
go run ./cmd/server
```

`GET http://127.0.0.1:8080/health` returns `{"status":"ok"}`. Set `HTTP_ADDR`
to override the listener; it defaults to local access only.

When running the API outside Compose, export the values in `.env` into the
process environment and use hosts reachable from the host machine. The Go API
reads process environment variables only; it does not parse `.env` files.

```sh
go test ./...
go vet ./...
go build -o bin/server ./cmd/server
# Benchmark the fixed Argon2id profile on the deployment target before release.
go test -bench='Benchmark(Hasher|Verifier)$' -benchtime=1x ./internal/auth/adapter/password
```

The recovery API accepts `POST /api/auth/password-reset/request` with an email
and returns the same `202 {"status":"accepted"}`
for known and unknown accounts. The reset email opens
`/reset-password#token=...`; the browser removes the fragment before React
mounts. Completion accepts the token and a policy-compliant password,
returns `204`, clears any presented session cookie, invalidates all old
sessions, and requires an explicit login with the new password.

After setup, the first administrator is assigned to the immutable `Super
Admin` role. The Users, Invitations, and Roles pages are grouped under the
authenticated Users & Access navigation. The live permission catalog uses
independent `resource.read` and `resource.write` grants for users, roles,
invitations, and settings. Feature combinations (for example, invitation
creation plus `roles.read`) are shown in the role editor and validated by the
API. Super Admins retain full access, while delegated managers may create,
resend, renew, or revoke only invitations whose roles fit their own effective
permissions.
Custom roles must retain at least one permission, users and invitations at
least one role, and a role cannot be deleted while assigned to a user or
invitation. The API refuses any change that would leave zero usable Super
Admin accounts.

Invitations are separate from activated users. A one-time link opens
`/accept-invitation#token=...`; the admin removes the fragment before render,
acceptance creates the account and assignments atomically, and the recipient
must explicitly log in afterwards.

## Admin

In another terminal, from the project directory:

```sh
cd admin
pnpm install --ignore-scripts
pnpm dev
```

Open the URL printed by Vite (by default http://127.0.0.1:5173). If that port is
in use, Vite automatically tries the next available port. The first install
creates `admin/pnpm-lock.yaml`; keep it in version control.

The API's `APP_PUBLIC_URL` must equal that exact printed origin, including the
selected port. If Vite falls back to another port, update `APP_PUBLIC_URL` in
the root `.env`, recreate the API with `docker compose up -d api`, and use the
new setup link from its log. Restarting alone does not reload environment values.
The Vite proxy reads `API_PORT` from the same root `.env`.

```sh
pnpm lint
pnpm check
pnpm build
pnpm preview --host 127.0.0.1
```

`pnpm lint` runs Oxlint; `pnpm check` runs TypeScript; `pnpm test` runs unit and
component tests. The preview command only inspects a local build. Production
serving is provided by the Caddy image in `make up`, with `/api` proxied to the
private API and navigation paths falling back to `index.html`.

Each application owns its dependencies and configuration. There is no root
workspace or runtime dependency on Temvia, and no dependencies are installed by
the generator. The generic Compose file does not assume public DNS or TLS;
configure direct Caddy TLS or an external ingress and keep `APP_PUBLIC_URL`
equal to the public HTTPS origin. Nginx can replace Caddy if it preserves the
same exact API proxy and SPA fallback behavior.

### Online users

The **System monitoring → Online users** page lists users with at least one valid
sign-in session, grouped by account. A session remains online while it is valid
even when its browser page is closed or it has had no recent activity. The page
shows the user's name, email, valid session count, and last activity. The list
refreshes every 15 seconds; open authenticated pages check their session every
30 seconds. Browsers may throttle these checks when suspended.

Grant `online-users.read` to view the list and `online-users.write` to force sign
users out. The write permission can force sign out any user, including a Super
Admin or the current user; the read permission remains independent. Force sign
out revokes all
existing sessions using the PostgreSQL account version, including inactive
devices; users may sign in again. Revoked sessions are denied on their next
request, and open pages show a localized expiry message before returning to
login after a session check. Successful and failed force sign-out actions appear
in operation history.

`GET /api/online-users` returns `{ users: [{ id, name, email, lastSeenAt,
sessionCount }] }`. `POST /api/online-users/{id}/kick` requires a same-origin
request and returns 204. Session credentials are never exposed in these APIs.

## Production deployment

Use `APP_ENV=production` and set `APP_PUBLIC_URL` to the public HTTPS origin.
Provide TLS through an ingress or a customized Caddy configuration; the supplied
Compose gateway only listens on loopback HTTP. Keep backend and database ports
private. Run `make build`, `make migrate-up`, then
`docker compose up -d api admin` without the development profile. Do not use
`make up` for production, because it enables Mailpit. Configure real SMTP through
System settings. For local Mailpit use host `mailpit`, port `1025`, security
`none`, no credentials, and a test sender in System settings.

If a host port is occupied, change its mapping in `.env`; when changing
`ADMIN_PORT`, change `APP_PUBLIC_URL` to the matching browser origin too.
For frontend development, stop the Compose admin (`docker compose stop admin`)
and follow the Admin section. Recreate the API after changing `.env` using
`docker compose up -d api`; restarting alone does not reload environment values.
The API must retain the same public origin used by your browser.

Back up PostgreSQL and the encryption/signing keys separately. Avoid `docker
compose down -v` unless you intend to remove the project's database volume.

## License and updates

The supplied Temvia code is MIT licensed; retain LICENSE and third-party
notices in `admin/UPSTREAM.md`. You own your added code. This project does not
automatically receive template upgrades. Consult
[Temvia releases](https://github.com/jonathanhu237/temvia/releases) and apply
relevant fixes yourself. Public feedback goes to
[Issues](https://github.com/jonathanhu237/temvia/issues); private vulnerability
reporting is described in
[SECURITY.md](https://github.com/jonathanhu237/temvia/blob/main/SECURITY.md).
Maintenance is best effort with no fixed response time or old-version support.
