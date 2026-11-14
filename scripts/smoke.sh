#!/usr/bin/env bash
# Purpose: Execute smoke tests verifying API endpoints and reservation workflow over HTTPS.
# Usage: ./scripts/smoke.sh [BASE_URL]
# Required Environment: curl, jq (or grep/cut).
# Exit Codes: 0 on success, 1 on failure.

set -euo pipefail

HTTPS_PORT="${HTTPS_PORT:-443}"
BASE_URL="${1:-https://127.0.0.1:${HTTPS_PORT}}"

echo "==> Running smoke test against ${BASE_URL}..."

# Helper for curl requests
api_curl() {
  curl -s -k "$@"
}

# 1. Healthz probe
echo "[1/6] Checking /healthz liveness probe..."
HEALTH_RESP="$(api_curl "${BASE_URL}/healthz")"
if ! echo "${HEALTH_RESP}" | grep -q '"status":"ok"'; then
  echo "FAIL: /healthz unexpected response: ${HEALTH_RESP}"
  exit 1
fi
echo "PASS: /healthz is live."

# 2. Readyz probe
echo "[2/6] Checking /readyz readiness probe..."
READY_RESP="$(api_curl "${BASE_URL}/readyz")"
if ! echo "${READY_RESP}" | grep -q '"status":"ok"'; then
  echo "FAIL: /readyz unexpected response: ${READY_RESP}"
  exit 1
fi
echo "PASS: /readyz is ready."

# 3. List shows
echo "[3/6] Fetching /v1/shows..."
SHOWS_RESP="$(api_curl "${BASE_URL}/v1/shows")"
SHOW_ID="$(echo "${SHOWS_RESP}" | grep -o '"id":"[^"]*' | head -n 1 | cut -d'"' -f4 || true)"
if [ -z "${SHOW_ID}" ]; then
  echo "FAIL: /v1/shows returned no shows: ${SHOWS_RESP}"
  exit 1
fi
echo "PASS: Retrieved show ID: ${SHOW_ID}"

# 4. Create hold on seat E5
echo "[4/6] Creating temporary hold on seat E5..."
HOLD_RESP="$(api_curl -X POST "${BASE_URL}/v1/reservations" \
  -H "Content-Type: application/json" \
  -d "{\"show_id\":\"${SHOW_ID}\",\"seat_label\":\"E5\",\"customer_ref\":\"smoke-test-runner\"}")"

RES_ID="$(echo "${HOLD_RESP}" | grep -o '"id":"[^"]*' | head -n 1 | cut -d'"' -f4 || true)"
if [ -z "${RES_ID}" ]; then
  echo "FAIL: Failed to create hold on seat E5: ${HOLD_RESP}"
  exit 1
fi
echo "PASS: Created reservation hold: ${RES_ID}"

# 5. Confirm reservation
echo "[5/6] Confirming reservation..."
CONFIRM_RESP="$(api_curl -X POST "${BASE_URL}/v1/reservations/${RES_ID}/confirm")"
TICKET_ID="$(echo "${CONFIRM_RESP}" | grep -o '"ticket_id":"[^"]*' | head -n 1 | cut -d'"' -f4 || true)"
if [ -z "${TICKET_ID}" ]; then
  echo "FAIL: Failed to confirm reservation: ${CONFIRM_RESP}"
  exit 1
fi
echo "PASS: Confirmed reservation with ticket: ${TICKET_ID}"

# 6. Cancel reservation
echo "[6/6] Cancelling reservation to free seat..."
CANCEL_RESP="$(api_curl -X POST "${BASE_URL}/v1/reservations/${RES_ID}/cancel")"
if ! echo "${CANCEL_RESP}" | grep -q '"status":"CANCELLED"'; then
  echo "FAIL: Failed to cancel reservation: ${CANCEL_RESP}"
  exit 1
fi
echo "PASS: Successfully cancelled reservation and released seat."

echo "==> ALL SMOKE TESTS PASSED SUCCESSFULLY."
