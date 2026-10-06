#!/bin/sh
# Fail if staged diff contains a DATABASE_URL value or other secrets.
set -eu
if git diff --cached -- internal/store cmd internal/config internal/gateway | grep -Ei 'postgres(ql)?://[^[:space:"'\'']+:[^[:space:"'\'']+@' >/dev/null 2>&1; then
  echo "refusing commit: staged diff appears to contain a Postgres URL with credentials" >&2
  exit 1
fi
if git diff --cached | grep -E 'DATABASE_URL=.+://' | grep -v 'DATABASE_URL=$' | grep -v 'DATABASE_URL is' | grep -v 'DATABASE_URL must' >/dev/null 2>&1; then
  echo "warning: staged diff mentions DATABASE_URL value; verify no secret is committed" >&2
  git diff --cached | grep -E 'DATABASE_URL' >&2 || true
  exit 1
fi
echo "secret check passed"
