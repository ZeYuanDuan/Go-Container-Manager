#!/usr/bin/env bash
# Clean test environment: remove aidms Docker containers, reset PG, clear file storage.
# Run before test-api to ensure clean state; run after to restore.
# Usage: ./scripts/clean_test_env.sh (from go-container-manager/)

set -e
cd "$(dirname "$0")/.."

# Load .env
set -a
[ -f .env ] && . ./.env
set +a

STORAGE="${STORAGE_ROOT:-./storage/uploads}"

echo "--- Cleaning test environment ---"
echo "Note: Stop the server first to avoid PG lock waits (migrate down blocks on active connections)."
echo ""

# 1. Remove aidms workload containers (exclude aidms-postgres, aidms-server)
echo "Removing aidms workload containers..."
count=0
for name in $(docker ps -a --format '{{.Names}}' 2>/dev/null | grep '^aidms-' || true); do
  [[ "$name" == "aidms-postgres" || "$name" == "aidms-server" ]] && continue
  docker rm -f "$name" 2>/dev/null && count=$((count+1))
done
echo "  Removed $count aidms workload container(s)"

# 2. Reset PostgreSQL (migrate down to 0, then up)
if [[ -z "$DATABASE_URL" ]]; then
  echo "DATABASE_URL not set, skipping PG reset"
else
  echo "Resetting PostgreSQL..."
  set +e
  for _ in {1..10}; do
    out=$(migrate -path migrations -database "$DATABASE_URL" down 6 2>&1)
    if [[ "$out" == *"Dirty database"* ]]; then
      ver=$(echo "$out" | grep -oE 'version [0-9]+' | grep -oE '[0-9]+' | head -1)
      echo "  Fixing dirty state (version $ver)..."
      migrate -path migrations -database "$DATABASE_URL" force "${ver:-0}" 2>/dev/null || true
    else
      break
    fi
  done
  migrate -path migrations -database "$DATABASE_URL" up
  set -e
  echo "  PG reset complete"
fi

# 3. Clear file storage (physical files)
if [[ -d "$STORAGE" ]]; then
  echo "Clearing storage: $STORAGE"
  rm -rf "${STORAGE:?}"/*
  echo "  Storage cleared"
else
  echo "  Storage dir not found, skipping"
fi

echo "--- Clean complete ---"
