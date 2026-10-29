# bind-plane

Web app for authorized network operators to release IPv4 static IP-MAC bindings on switches.

## Local Startup

Copy `.env.example` to `.env` and set `BIND_PLANE_INITIAL_ADMIN_PASSWORD` to a password with at least 8 characters. The first FastAPI startup creates the initial admin when no admin exists; later accounts are managed from the admin UI.

```bash
docker compose up -d postgres redis
uv run alembic upgrade head
make api
make worker
make frontend-dev
```

Open `http://localhost:5173` and sign in with `BIND_PLANE_INITIAL_ADMIN_USERNAME` and `BIND_PLANE_INITIAL_ADMIN_PASSWORD`.

## Temvia migration preview

The independent Temvia-generated Go API, React administration UI, and Python/Netmiko worker are under [`next/`](next/). The legacy application is retained until an explicit cutover. See [`next/TEMVIA_BASELINE.md`](next/TEMVIA_BASELINE.md) for the pinned template source. Generate `next/.env` from `next/.env.example`, set the four Temvia secrets **and** `BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY` to an independent 32-byte base64url key, then run `cd next && make build && make migrate-up && make up`. The Compose stack is isolated from the legacy preview. The initial administrator is created through Temvia's one-time setup link; invite operator/admin users through its administration UI. SMTP/Mailpit is configured in system settings.

Business jobs and evidence live in PostgreSQL, not Temvia's expiring operation history. The worker claims fenced jobs and never automatically replays an uncertain switch command; an explicit retry creates a new linked pre-query. Admins can create, edit, enable and disable assets; every change is versioned and audited, and a release whose switch or command profile changed after its pre-query is refused until the operator queries again. Import, full audit browse and switch end-to-end verification remain incomplete. Do **not** connect production switches or treat this preview as production-ready. The existing environment remains the known working version.
