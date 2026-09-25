FROM migrate/migrate:v4.19.1

# Copy every migration in both directions so adding a numbered migration does
# not require maintaining a second, easy-to-miss Dockerfile allowlist.
COPY *.sql /migrations/
COPY migrate-entrypoint.sh /usr/local/bin/migrate-entrypoint.sh

ENTRYPOINT ["sh", "/usr/local/bin/migrate-entrypoint.sh"]
