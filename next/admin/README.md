# Temvia Admin

The admin is an independent React application built with Vite, TanStack
Router, TanStack Query, React Hook Form, Zod and shadcn/ui. It talks to the Go
API through relative `/api` requests, so the browser and API stay same-origin
in both development and the Caddy production container.

## Development

The first install creates `pnpm-lock.yaml`; keep that lockfile in a consuming
project and let the container use it for frozen installs:

```sh
pnpm install --ignore-scripts
pnpm dev
```

Vite prefers `http://127.0.0.1:5173` and automatically chooses another port if
that port is occupied. Always use the URL Vite prints. Set `API_PORT` when the
Go API is listening on a different loopback port; Vite reads it from the root
`.env`. The API's `APP_PUBLIC_URL` must equal Vite's exact printed origin. If
Vite selects another port, update `APP_PUBLIC_URL`, recreate the API with
`docker compose up -d api`, and use the replacement setup link from its log.
Restarting alone does not reload environment values.

```sh
pnpm lint
pnpm check
pnpm test
pnpm build
pnpm preview --host 127.0.0.1
```

`pnpm preview` is a local build inspection command. Production serving uses
the pinned Caddy image described in [Caddyfile](./Caddyfile), which serves the
SPA, falls back navigation paths to `index.html`, and proxies `/api` to the
private Compose API service.

## Authentication flow

The API prints a temporary setup link while initialization is incomplete. Open
the link in the browser origin configured by `APP_PUBLIC_URL`. The setup
credential starts in the URL fragment and is removed before React renders; it
is kept in memory and sent only in the setup request. Setup creates the first
administrator without a session. Sign in afterwards to create the HttpOnly
session cookie.

Translations are bundled in Simplified Chinese and English. The language menu
stores only the manually selected locale in localStorage; authentication data
is never persisted by the admin. The login page links to `/forgot-password`.
That form intentionally shows the same accepted state for an existing or
unknown email. Reset links use a `#token=` fragment; bootstrap captures and
clears it with `history.replaceState` before React renders, keeping the token
in module memory only. `/reset-password` asks for the new password twice,
shows one invalid/expired-link state, and sends the user back to `/login` after
success without automatically authenticating them. Mail language is selected
by shared System settings and is separate from each administrator's interface
language.

The API's PostgreSQL outbox sends the reset link and a separate password-changed
notification asynchronously over SMTP. SMTP outages do not block the form;
the API retries durable jobs. Configure the shared SMTP connection and default
mail language from System settings; see the root README for Mailpit and
production guidance. The Caddy runtime also sends `Referrer-Policy: no-referrer`
as defense in depth for fragment credentials.

## Access management

The authenticated Users & Access navigation contains separate Users,
Invitations, and Roles routes driven by the current principal's permissions.
`users.read` shows activated users and role badges; `invitations.read` shows
pending and expired invitations; `roles.read` shows the catalog and role
details. The built-in `Super Admin` role is assigned to the first setup account
and cannot be edited or deleted. Super Admins can create non-empty custom
roles, replace a user's complete non-empty role set, and manage invitations.
An invitation manager can only assign roles within its own effective
permissions, and the API remains authoritative for all of these checks,
including direct navigation to a hidden route.

Invitations arrive at `/accept-invitation#token=...`. Bootstrap removes the
fragment before React renders and keeps the one-time authority in module
memory. The acceptance form creates the account without a session; the new
user signs in normally after success. Invitation links use the separate
`INVITATION_TOKEN_KEY` and default to a 72-hour expiry (maximum seven days).

## Container

The multi-stage `Dockerfile` builds with Node 24 and serves only `dist` from
Caddy. The runtime image has no Node toolchain, source files or dependency
tree. Generic Compose publishes Caddy on `ADMIN_PORT` (5173 by default). Keep
`APP_PUBLIC_URL` equal to the browser-visible origin. Public TLS can be owned
by Caddy with an explicit domain configuration or by an external ingress; the
generic template does not assume DNS or certificates.
