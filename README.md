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
