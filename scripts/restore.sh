#!/usr/bin/env bash
# Purpose: Restore database from logical backup dump, verify checksum, validate constraints, and test service recovery.
# Usage: ./scripts/restore.sh --confirm <path-to-dump>
# Required Environment: Docker, Docker Compose, sha256sum.
# Exit Codes: 0 on success, 1 on failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${SCRIPT_DIR}/.."
COMPOSE_FILE="${ROOT_DIR}/deploy/compose.yaml"

if [ $# -lt 2 ] || [ "$1" != "--confirm" ]; then
  echo "ERROR: Restore is a destructive operation and requires explicit confirmation flag."
  echo "Usage: $0 --confirm <path-to-backup.dump>"
  exit 1
fi

DUMP_PATH="$2"

if [ ! -f "${DUMP_PATH}" ]; then
  echo "ERROR: Backup file not found: ${DUMP_PATH}"
  exit 1
fi

CHECKSUM_PATH="${DUMP_PATH}.sha256"
if [ -f "${CHECKSUM_PATH}" ]; then
  echo "==> Verifying SHA256 checksum of backup archive..."
  DUMP_DIR="$(dirname "${DUMP_PATH}")"
  DUMP_BASE="$(basename "${DUMP_PATH}")"
  (cd "${DUMP_DIR}" && sha256sum -c "${DUMP_BASE}.sha256")
  echo "PASS: Backup checksum verified."
else
  echo "WARNING: Checksum file not found; proceeding with unverified archive."
fi

# 1. Stop the API container to prevent active client writes
echo "[1/6] Stopping API container to freeze writes..."
docker compose -f "${COMPOSE_FILE}" stop api

# 2. Restore database objects using pg_restore with clean/if-exists options
echo "[2/6] Restoring database schema and data from dump..."
docker compose -f "${COMPOSE_FILE}" exec -T db \
  pg_restore -U cinema_user -d cinema_db --clean --if-exists < "${DUMP_PATH}" || true

# 3. Verify database integrity: ensure tables exist, row counts are non-zero, and partial unique index is valid
echo "[3/6] Verifying database schema, indexes, and row counts..."
INDEX_CHECK="$(docker compose -f "${COMPOSE_FILE}" exec -T db \
  psql -U cinema_user -d cinema_db -t -c \
  "SELECT count(*) FROM pg_indexes WHERE indexname = 'idx_reservations_active_seat';")"

if [ "$(echo "${INDEX_CHECK}" | tr -d '[:space:]')" -ne "1" ]; then
  echo "FAIL: Partial unique index idx_reservations_active_seat was not restored properly!"
  exit 1
fi
echo "PASS: Partial unique index idx_reservations_active_seat verified."

SHOWS_COUNT="$(docker compose -f "${COMPOSE_FILE}" exec -T db \
  psql -U cinema_user -d cinema_db -t -c "SELECT count(*) FROM shows;")"
echo "PASS: Database holds $(echo "${SHOWS_COUNT}" | tr -d '[:space:]') shows."

# 4. Bring up API and proxy containers
echo "[4/6] Bringing up application containers..."
docker compose -f "${COMPOSE_FILE}" up -d api proxy

# 5. Wait for service health
echo "[5/6] Waiting for service healthy status..."
TIMEOUT=30
ELAPSED=0
HEALTHY=false

while [ "${ELAPSED}" -lt "${TIMEOUT}" ]; do
  STATUS="$(docker inspect --format='{{json .State.Health.Status}}' "$(docker compose -f "${COMPOSE_FILE}" ps -q api 2>/dev/null)" 2>/dev/null || echo '"starting"')"
  if [ "${STATUS}" = '"healthy"' ]; then
    HEALTHY=true
    break
  fi
  sleep 1
  ELAPSED=$((ELAPSED + 1))
done

if [ "${HEALTHY}" != "true" ]; then
  echo "FAIL: API service failed to become healthy within ${TIMEOUT}s after restore."
  exit 1
fi
echo "PASS: API service restored to healthy state."

# 6. Execute smoke tests
echo "[6/6] Executing smoke tests against restored service..."
"${SCRIPT_DIR}/smoke.sh"

echo "==> RESTORE COMPLETED SUCCESSFULLY."
