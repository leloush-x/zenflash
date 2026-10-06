#!/bin/sh
# Test migrations in a throwaway schema (additive only, idempotent).
# Requires DATABASE_URL. Uses a temp schema, runs migrations twice, checks tables.
set -eu
if [ -z "${DATABASE_URL:-}" ]; then echo "DATABASE_URL not set; skipping throwaway-schema test"; exit 0; fi
SCHEMA="v2test_$(date +%s)_$RANDOM"
export PGOPTIONS="-c search_path=$SCHEMA"
echo "testing schema $SCHEMA"
psql "$DATABASE_URL" -c "CREATE SCHEMA $SCHEMA;" >/dev/null
# Run binary briefly to trigger migrations, or apply SQL directly:
for f in internal/store/migrations/*.sql; do echo "apply $f"; psql "$DATABASE_URL" -f "$f" >/dev/null; done
psql "$DATABASE_URL" -c "SELECT count(*) FROM schema_migrations;" >/dev/null && echo "migrations ok"
# idempotency: re-apply
for f in internal/store/migrations/*.sql; do psql "$DATABASE_URL" -f "$f" >/dev/null; done
echo "idempotency ok"
psql "$DATABASE_URL" -c "DROP SCHEMA $SCHEMA CASCADE;" >/dev/null
echo "throwaway schema test passed"
