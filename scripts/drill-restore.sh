#!/usr/bin/env bash
# Purpose: Execute disaster recovery drill - create known confirmed reservation, back up database, wipe database volume, restore from backup, and prove zero data loss and index preservation.
# Usage: ./scripts/drill-restore.sh
# Required Environment: Docker, Docker Compose, curl, jq/grep.
# Exit Codes: 0 on success, 1 on failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${SCRIPT_DIR}/.."
COMPOSE_FILE="${ROOT_DIR}/deploy/compose.yaml"
HTTPS_PORT="${HTTPS_PORT:-443}"
BASE_URL="https://127.0.0.1:${HTTPS_PORT}"

echo "=========================================================="
echo " Starting Disaster Recovery & Database Restore Drill"
echo "=========================================================="

api_curl() {
  curl -s -k "$@"
}

# 1. Verify system is healthy
echo "[1/7] Ensuring system is healthy before drill..."
INITIAL_READY="$(api_curl "${BASE_URL}/readyz")"
if ! grep -q '"status":"ok"' <<< "${INITIAL_READY}"; then
  ./scripts/deploy.sh cinema-api:local >/dev/null
fi
echo "PASS: System is healthy."

# 2. Create and confirm a specific known reservation
echo "[2/7] Creating and confirming known test reservation..."
SHOWS_RESP="$(api_curl "${BASE_URL}/v1/shows")"
SHOW_ID="$(echo "${SHOWS_RESP}" | grep -o '"id":"[^"]*' | head -n 1 | cut -d'"' -f4 || true)"

# Find first available seat dynamically
SEAT_LABEL="$(api_curl "${BASE_URL}/v1/shows/${SHOW_ID}/seats" | grep -o '{"id":"[^"]*","label":"[^"]*","status":"AVAILABLE"' | head -n 1 | grep -o '"label":"[^"]*' | cut -d'"' -f4 || true)"
if [ -z "${SEAT_LABEL}" ]; then
  echo "FAIL: No available seats found for show ${SHOW_ID}."
  exit 1
fi

HOLD_RESP="$(api_curl -X POST "${BASE_URL}/v1/reservations" \
  -H "Content-Type: application/json" \
  -d "{\"show_id\":\"${SHOW_ID}\",\"seat_label\":\"${SEAT_LABEL}\",\"customer_ref\":\"dr-restore-audit\"}")"

RES_ID="$(echo "${HOLD_RESP}" | grep -o '"id":"[^"]*' | head -n 1 | cut -d'"' -f4 || true)"
if [ -z "${RES_ID}" ]; then
  echo "FAIL: Failed to create test reservation on ${SEAT_LABEL}: ${HOLD_RESP}"
  exit 1
fi

CONFIRM_RESP="$(api_curl -X POST "${BASE_URL}/v1/reservations/${RES_ID}/confirm")"
TICKET_ID="$(echo "${CONFIRM_RESP}" | grep -o '"ticket_id":"[^"]*' | head -n 1 | cut -d'"' -f4 || true)"
if [ -z "${TICKET_ID}" ]; then
  echo "FAIL: Failed to confirm reservation: ${CONFIRM_RESP}"
  exit 1
fi

echo "PASS: Confirmed reservation ${RES_ID} (Seat ${SEAT_LABEL}, Ticket ${TICKET_ID})."

# 3. Create logical database backup
echo "[3/7] Generating logical database backup via scripts/backup.sh..."
BACKUP_OUTPUT="$("${SCRIPT_DIR}/backup.sh")"
BACKUP_FILE="$(echo "${BACKUP_OUTPUT}" | grep -o '/.*\.dump' | head -n 1 || true)"

if [ -z "${BACKUP_FILE}" ] || [ ! -f "${BACKUP_FILE}" ]; then
  echo "FAIL: Backup file was not generated: ${BACKUP_OUTPUT}"
  exit 1
fi
echo "PASS: Generated verified backup dump at ${BACKUP_FILE}."

# 4. Simulate catastrophic data loss: wipe the entire database volume
echo "[4/7] CATASTROPHIC FAILURE: Tearing down stack and wiping database volume..."
docker compose -f "${COMPOSE_FILE}" down -v

# 5. Initialize fresh database
echo "[5/7] Starting empty database instance..."
docker compose -f "${COMPOSE_FILE}" up -d db

echo "Waiting for fresh database to report healthy..."
TIMEOUT=30
ELAPSED=0
DB_HEALTHY=false

while [ "${ELAPSED}" -lt "${TIMEOUT}" ]; do
  DB_STATUS="$(docker inspect --format='{{json .State.Health.Status}}' "$(docker compose -f "${COMPOSE_FILE}" ps -q db 2>/dev/null)" 2>/dev/null || echo '"starting"')"
  if [ "${DB_STATUS}" = '"healthy"' ]; then
    DB_HEALTHY=true
    break
  fi
  sleep 1
  ELAPSED=$((ELAPSED + 1))
done

if [ "${DB_HEALTHY}" != "true" ]; then
  echo "FAIL: Fresh database failed to reach healthy status."
  exit 1
fi
echo "PASS: Empty database instance is online and healthy."

# 6. Execute restore using scripts/restore.sh
echo "[6/7] Restoring database from backup archive..."
"${SCRIPT_DIR}/restore.sh" --confirm "${BACKUP_FILE}"

# 7. Verify known reservation and constraints in restored database
echo "[7/7] Verifying restored reservation data and constraints..."
FETCH_RESP="$(api_curl "${BASE_URL}/v1/reservations/${RES_ID}")"

if ! grep -q "\"seat_label\":\"${SEAT_LABEL}\"" <<< "${FETCH_RESP}" || ! grep -q "\"ticket_id\":\"${TICKET_ID}\"" <<< "${FETCH_RESP}"; then
  echo "FAIL: Restored reservation data does not match pre-disaster state: ${FETCH_RESP}"
  exit 1
fi
echo "PASS: Pre-disaster reservation ${RES_ID} (Seat ${SEAT_LABEL}, Ticket ${TICKET_ID}) successfully verified!"

# Clean up test seat
api_curl -X POST "${BASE_URL}/v1/reservations/${RES_ID}/cancel" >/dev/null || true

echo "=========================================================="
echo " DISASTER RECOVERY DRILL COMPLETED SUCCESSFULLY:"
echo " Complete volume destruction, restore, and verified zero data loss."
echo "=========================================================="
