#!/bin/sh
set -eu

exec migrate \
  -path=/migrations \
  -database="postgres://${PGUSER}@${PGHOST}:${PGPORT}/${PGDATABASE}?sslmode=${PGSSLMODE}" \
  "$@"
