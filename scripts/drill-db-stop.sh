#!/usr/bin/env bash
# Purpose: Execute failure drill 2 - stop the database, verify liveness decoupling and readiness degradation, restore database, and verify automatic recovery.
# Usage: ./scripts/drill-db-stop.sh
# Required Environment: Docker, Docker Compose, curl.
# Exit Codes: 0 on success, 1 on failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${SCRIPT_DIR}/.."
COMPOSE_FILE="${ROOT_DIR}/deploy/compose.yaml"
HTTPS_PORT="${HTTPS_PORT:-443}"
BASE_URL="https://127.0.0.1:${HTTPS_PORT}"

echo "=========================================================="
echo " Starting Drill 2: Database Outage and Automatic Recovery"
echo "=========================================================="

api_curl() {
  curl -s -k "$@"
}

# 1. Verify initial health
echo "[1/6] Verifying initial system health..."
INITIAL_READY="$(api_curl "${BASE_URL}/readyz")"
if ! grep -q '"status":"ok"' <<< "${INITIAL_READY}"; then
  echo "FAIL: System is not initially ready."
  exit 1
fi
echo "PASS: System is healthy and ready."

# 2. Stop the database container
echo "[2/6] Stopping the PostgreSQL database container..."
docker compose -f "${COMPOSE_FILE}" stop db

# 3. Verify liveness remains UP (process is alive)
echo "[3/6] Verifying /healthz liveness probe during database outage..."
HEALTH_CODE="$(api_curl -o /dev/null -w "%{http_code}" "${BASE_URL}/healthz")"
if [ "${HEALTH_CODE}" != "200" ]; then
  echo "FAIL: Liveness probe failed (${HEALTH_CODE}) during DB outage; liveness must remain 200."
  exit 1
fi
echo "PASS: Liveness /healthz returned 200 OK (process decoupled from DB availability)."

# 4. Verify readiness reports 503 Service Unavailable
echo "[4/6] Verifying /readyz and domain endpoints report 503..."
READY_CODE="$(api_curl -o /dev/null -w "%{http_code}" "${BASE_URL}/readyz")"
if [ "${READY_CODE}" != "503" ]; then
  echo "FAIL: Readiness probe returned ${READY_CODE}; expected 503 during DB outage."
  exit 1
fi
echo "PASS: Readiness /readyz returned 503 (readiness correctly flipped to unready)."

SHOWS_CODE="$(api_curl -o /dev/null -w "%{http_code}" "${BASE_URL}/v1/shows")"
if [ "${SHOWS_CODE}" != "503" ]; then
  echo "FAIL: /v1/shows returned ${SHOWS_CODE}; expected 503 during DB outage."
  exit 1
fi
echo "PASS: /v1/shows returned 503 cleanly without crashing API."

# 5. Restore the database
echo "[5/6] Restoring the database container..."
docker compose -f "${COMPOSE_FILE}" start db

echo "Waiting for database container to report healthy..."
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
  echo "FAIL: Database failed to reach healthy status within ${TIMEOUT}s."
  exit 1
fi
echo "PASS: Database is healthy."

# 6. Verify automatic readiness recovery without restarting API
echo "[6/6] Verifying automatic service recovery and readiness restoration..."
ELAPSED=0
API_READY=false

while [ "${ELAPSED}" -lt "${TIMEOUT}" ]; do
  READY_RESP="$(api_curl "${BASE_URL}/readyz")"
  if grep -q '"status":"ok"' <<< "${READY_RESP}"; then
    API_READY=true
    break
  fi
  sleep 1
  ELAPSED=$((ELAPSED + 1))
done

if [ "${API_READY}" != "true" ]; then
  echo "FAIL: API failed to recover automatically to ready state."
  exit 1
fi
echo "PASS: API automatically restored readiness (/readyz 200 OK) without container restart."

# Execute smoke test to ensure full operation
"${SCRIPT_DIR}/smoke.sh" >/dev/null
echo "PASS: Smoke tests executed successfully after database recovery."

echo "=========================================================="
echo " DRILL 2 COMPLETED SUCCESSFULLY: Liveness decoupled,"
echo " readiness degraded, and automatic recovery proven."
echo "=========================================================="
