# Deploying Bind Plane with Docker

One host runs PostgreSQL, the API, the worker and the admin gateway (Caddy).
Only the gateway is published: HTTPS on `BIND_PLANE_HTTPS_PORT` (8443 by default).
PostgreSQL and the API stay on a private Docker network. The worker connects
out to the switches, so the host must be able to reach their management
addresses.

Images are built on a build host and copied to the server. The server needs
Docker with Compose v2, but no access to Docker Hub or language registries.

## Build and ship the images

On the build host, from the repository root:

```sh
TAG=2026.10.30-1
docker build -t bind-plane-api:$TAG next/api
docker build -t bind-plane-migrate:$TAG next/api/migrations
docker build -t bind-plane-admin:$TAG next/admin
docker build -t bind-plane-worker:$TAG -f next/worker/Dockerfile .
docker save bind-plane-api:$TAG bind-plane-migrate:$TAG bind-plane-admin:$TAG bind-plane-worker:$TAG postgres:18.6-trixie \
  | gzip | ssh deploy-host 'gunzip | docker load'
```

## First installation

On the server, in the deployment folder (for example `/srv/ecnc/bind-plane`),
place `compose.yaml` and `Caddyfile` from this directory, then:

```sh
cp env.example .env && chmod 600 .env   # fill every value; one fresh key per variable
docker compose --profile tools run --rm migrate
docker compose up -d
docker compose logs api | grep 'initial setup link'
```

Open the setup link within 30 minutes and create the first administrator. It
becomes Super Admin; invite operators and admins from Users & Access, and give
them the bind-plane **Operator** or **Admin** role. Only Admin (and Super Admin)
can use device operations; Operators see the release console and nothing else.

## The certificate

Caddy issues the certificate from its own CA, so browsers warn until that CA
is trusted. `BIND_PLANE_TLS_HOST` must equal the host in `APP_PUBLIC_URL`:
browsers send no SNI to an IP address, and behind Docker's port mapping Caddy
would otherwise not know which certificate to present. Export the CA and
install it on the admin workstations:

```sh
docker compose cp admin:/data/caddy/pki/authorities/local/root.crt ./bind-plane-root.crt
```

The CA lives in the `caddy-data` volume; keep the volume and the certificate
stays the same. To use a real certificate, replace `tls internal` in the
Caddyfile, then add HSTS.

## Backups

Back up three things, separately:

- the database: `docker compose exec -T postgres pg_dump -U bind_plane bind_plane > bind-plane.sql`
- `.env`, above all `BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY`: without it, stored
  switch credentials and configuration backups cannot be decrypted
- the `caddy-data` volume, if browsers already trust its CA

## Upgrades

Build and load the new tag, then on the server:

```sh
docker compose exec -T postgres pg_dump -U bind_plane bind_plane > before-upgrade.sql
sed -i 's/^BIND_PLANE_TAG=.*/BIND_PLANE_TAG=<new tag>/' .env
docker compose --profile tools run --rm migrate
docker compose up -d
```

Remove images of older tags with `docker image rm` once the new one runs.

## Testing against a simulated switch

`compose.e2e.yaml` adds a fake Huawei-style Telnet switch (from the worker
image) for end-to-end checks of backups and releases. Never deploy it.
