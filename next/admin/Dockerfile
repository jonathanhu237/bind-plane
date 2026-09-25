FROM node:24.20.0-alpine AS build

WORKDIR /app
COPY . .
RUN corepack enable && if [ -f pnpm-lock.yaml ]; then pnpm install --frozen-lockfile --ignore-scripts; else pnpm install --no-frozen-lockfile --ignore-scripts; fi
RUN pnpm build

FROM caddy:2.10.2-alpine

RUN addgroup -S caddy && adduser -S -G caddy caddy \
  && mkdir -p /srv \
  && chown -R caddy:caddy /config /data /srv /etc/caddy
COPY Caddyfile /etc/caddy/Caddyfile
COPY --from=build /app/dist /srv
RUN chown -R caddy:caddy /etc/caddy /srv
USER caddy

EXPOSE 8080
