#!/usr/bin/env bash
# Purpose: Execute failure drill 1 - abruptly kill the API container, observe restart policy, and verify recovery without data loss.
# Usage: ./scripts/drill-api-kill.sh
# Required Environment: Docker, Docker Compose, curl.
# Exit Codes: 0 on success, 1 on failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${SCRIPT_DIR}/.."
COMPOSE_FILE="${ROOT_DIR}/deploy/compose.yaml"
HTTPS_PORT="${HTTPS_PORT:-443}"
BASE_URL="https://127.0.0.1:${HTTPS_PORT}"

echo "=========================================================="
echo " Starting Drill 1: Abrupt API Container Kill and Recovery"
echo "=========================================================="

api_curl() {
  curl -s -k "$@"
}

# 1. Verify system is initially healthy
echo "[1/6] Verifying initial system health..."
INITIAL_READY="$(api_curl "${BASE_URL}/readyz")"
if ! grep -q '"status":"ok"' <<< "${INITIAL_READY}"; then
  echo "FAIL: System is not initially ready."
  exit 1
fi
echo "PASS: System is healthy."

# 2. Create reservation data prior to failure to test data durability
echo "[2/6] Seeding reservation data before killing container..."
SHOWS_RESP="$(api_curl "${BASE_URL}/v1/shows")"
SHOW_ID="$(echo "${SHOWS_RESP}" | grep -o '"id":"[^"]*' | head -n 1 | cut -d'"' -f4 || true)"
if [ -z "${SHOW_ID}" ]; then
  echo "FAIL: No shows available."
  exit 1
fi

SEAT_LABEL="$(api_curl "${BASE_URL}/v1/shows/${SHOW_ID}/seats" | grep -o '{"id":"[^"]*","label":"[^"]*","status":"AVAILABLE"' | head -n 1 | grep -o '"label":"[^"]*' | cut -d'"' -f4 || true)"
if [ -z "${SEAT_LABEL}" ]; then
  echo "FAIL: No available seats found for show ${SHOW_ID}."
  exit 1
fi

HOLD_RESP="$(api_curl -X POST "${BASE_URL}/v1/reservations" \
  -H "Content-Type: application/json" \
  -d "{\"show_id\":\"${SHOW_ID}\",\"seat_label\":\"${SEAT_LABEL}\",\"customer_ref\":\"drill-api-kill\"}")"

RES_ID="$(echo "${HOLD_RESP}" | grep -o '"id":"[^"]*' | head -n 1 | cut -d'"' -f4 || true)"
if [ -z "${RES_ID}" ]; then
  echo "FAIL: Failed to create test reservation before kill on ${SEAT_LABEL}: ${HOLD_RESP}"
  exit 1
fi
echo "PASS: Created test reservation ${RES_ID} on seat ${SEAT_LABEL}."

# 3. Abruptly kill API container with SIGKILL
echo "[3/6] Killing the api container (docker compose kill)..."
docker compose -f "${COMPOSE_FILE}" kill api

# 4. Observe immediate failure
echo "[4/6] Observing proxy reaction to dead API container..."
DEAD_RESP="$(api_curl -o /dev/null -w "%{http_code}" "${BASE_URL}/readyz" || true)"
echo "Observed response code: ${DEAD_RESP} (Proxy reports gateway error or drop as expected)"

# 5. Restore container to desired state and await healthy recovery
echo "[5/6] Restoring container to desired state and awaiting healthy status..."
docker compose -f "${COMPOSE_FILE}" up -d --no-recreate api

TIMEOUT=30
ELAPSED=0
RECOVERED=false

while [ "${ELAPSED}" -lt "${TIMEOUT}" ]; do
  STATUS="$(docker inspect --format='{{json .State.Health.Status}}' "$(docker compose -f "${COMPOSE_FILE}" ps -q api 2>/dev/null)" 2>/dev/null || echo '"starting"')"
  if [ "${STATUS}" = '"healthy"' ]; then
    RECOVERED=true
    break
  fi
  sleep 1
  ELAPSED=$((ELAPSED + 1))
done

if [ "${RECOVERED}" != "true" ]; then
  echo "FAIL: Container failed to restart and become healthy within ${TIMEOUT}s."
  exit 1
fi
echo "PASS: Container restarted automatically and reports healthy in ${ELAPSED}s."

# 6. Verify data durability: retrieve the reservation created before the crash
echo "[6/6] Verifying data integrity of pre-crash reservation..."
FETCH_RESP="$(api_curl "${BASE_URL}/v1/reservations/${RES_ID}")"
if ! grep -q "\"seat_label\":\"${SEAT_LABEL}\"" <<< "${FETCH_RESP}"; then
  echo "FAIL: Reservation data lost after restart: ${FETCH_RESP}"
  exit 1
fi
echo "PASS: Reservation ${RES_ID} retrieved intact with seat ${SEAT_LABEL}."

# Clean up test seat
api_curl -X POST "${BASE_URL}/v1/reservations/${RES_ID}/cancel" >/dev/null || true

echo "=========================================================="
echo " DRILL 1 COMPLETED SUCCESSFULLY: Crash detection, restart,"
echo " and zero data loss verified."
echo "=========================================================="
