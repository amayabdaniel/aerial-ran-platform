#!/bin/sh
# Run all .up.sql migrations across services in deterministic order.
# Idempotent via a migrations table per schema (only applies new files).

set -eu

SERVICES="iam subscriber esim provision ranctl billing messaging"

echo "[migrate] starting"
echo "[migrate] PGHOST=$PGHOST PGUSER=$PGUSER PGDATABASE=$PGDATABASE"

# Wait for postgres to be reachable
for i in $(seq 1 30); do
  if pg_isready -h "$PGHOST" -U "$PGUSER" -d "$PGDATABASE" >/dev/null 2>&1; then
    break
  fi
  echo "[migrate] waiting for postgres ($i/30)"
  sleep 1
done

for svc in $SERVICES; do
  dir="/migrations/$svc"
  if [ ! -d "$dir" ]; then
    echo "[migrate] skip $svc (no migrations dir)"
    continue
  fi

  schema="$svc"
  # ranctl uses schema 'ranctl'; svc dir matches above

  echo "[migrate] schema=$schema"
  # Ensure migrations tracking table exists
  PGPASSWORD="$PGPASSWORD" psql -h "$PGHOST" -U "$PGUSER" -d "$PGDATABASE" -v ON_ERROR_STOP=1 <<SQL
SET search_path TO $schema, public;
CREATE TABLE IF NOT EXISTS schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
SQL

  for f in $(ls "$dir"/*.up.sql 2>/dev/null | sort); do
    version=$(basename "$f" .up.sql)
    already=$(PGPASSWORD="$PGPASSWORD" psql -h "$PGHOST" -U "$PGUSER" -d "$PGDATABASE" -tA -c \
      "SELECT 1 FROM $schema.schema_migrations WHERE version = '$version'" 2>/dev/null || echo "")
    if [ "$already" = "1" ]; then
      echo "[migrate] $schema/$version (already applied)"
      continue
    fi
    echo "[migrate] $schema/$version"
    PGPASSWORD="$PGPASSWORD" psql -h "$PGHOST" -U "$PGUSER" -d "$PGDATABASE" -v ON_ERROR_STOP=1 \
      -c "SET search_path TO $schema, public;" -f "$f"
    PGPASSWORD="$PGPASSWORD" psql -h "$PGHOST" -U "$PGUSER" -d "$PGDATABASE" -v ON_ERROR_STOP=1 \
      -c "INSERT INTO $schema.schema_migrations(version) VALUES ('$version')"
  done
done

echo "[migrate] done"
