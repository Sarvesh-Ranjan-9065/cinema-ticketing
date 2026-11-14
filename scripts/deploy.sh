#!/usr/bin/env bash
# Purpose: Deploy a versioned image tag, execute migrations, verify health, and rollback automatically on failure.
# Usage: ./scripts/deploy.sh <IMAGE_TAG>
# Required Environment: Docker, Docker Compose, bash.
# Exit Codes: 0 on success, 1 on failure / rollback.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${SCRIPT_DIR}/.."
COMPOSE_FILE="${ROOT_DIR}/deploy/compose.yaml"
STATE_FILE="${ROOT_DIR}/deploy/.current_tag"

if [ $# -lt 1 ]; then
  echo "Usage: $0 <IMAGE_TAG>"
  echo "Example: $0 cinema-api:local"
  exit 1
fi

TARGET_TAG="$1"
PREVIOUS_TAG=""

if [ -f "${STATE_FILE}" ]; then
  PREVIOUS_TAG="$(cat "${STATE_FILE}")"
fi

echo "==> Initiating deployment for image tag: ${TARGET_TAG}"
echo "==> Previous active tag: ${PREVIOUS_TAG:-none}"

# Ensure secrets are initialized
"${SCRIPT_DIR}/init-secrets.sh"

rollback() {
  echo "==> DEPLOYMENT FAILED! Initiating automatic rollback..."
  if [ -n "${PREVIOUS_TAG}" ] && [ "${PREVIOUS_TAG}" != "${TARGET_TAG}" ]; then
    echo "==> Rolling back to previous stable image: ${PREVIOUS_TAG}"
    IMAGE_TAG="${PREVIOUS_TAG}" docker compose -f "${COMPOSE_FILE}" up -d --remove-orphans api
    echo "==> Rollback complete."
  else
    echo "==> No previous tag available for rollback. Inspect logs using: docker compose -f deploy/compose.yaml logs"
  fi
  exit 1
}

# 1. Run migrations with new image tag
echo "[1/5] Running database schema migrations..."
if ! IMAGE_TAG="${TARGET_TAG}" docker compose -f "${COMPOSE_FILE}" run --rm migrate; then
  echo "ERROR: Migrations failed!"
  rollback
fi

# Ensure seed data is loaded
echo "[2/5] Ensuring database seed data is present..."
if ! IMAGE_TAG="${TARGET_TAG}" docker compose -f "${COMPOSE_FILE}" run --rm seed; then
  echo "ERROR: Database seed failed!"
  rollback
fi

# 3. Deploy services
echo "[3/5] Deploying application containers..."
if ! IMAGE_TAG="${TARGET_TAG}" docker compose -f "${COMPOSE_FILE}" up -d --remove-orphans api proxy; then
  echo "ERROR: Failed starting containers!"
  rollback
fi

# 4. Wait for services to reach healthy state
echo "[4/5] Waiting for services to become healthy..."
TIMEOUT=45
ELAPSED=0
HEALTHY=false

while [ "${ELAPSED}" -lt "${TIMEOUT}" ]; do
  PROXY_STATUS="$(docker inspect --format='{{json .State.Health.Status}}' "$(docker compose -f "${COMPOSE_FILE}" ps -q proxy 2>/dev/null)" 2>/dev/null || echo '"starting"')"
  API_STATUS="$(docker inspect --format='{{json .State.Health.Status}}' "$(docker compose -f "${COMPOSE_FILE}" ps -q api 2>/dev/null)" 2>/dev/null || echo '"starting"')"

  if [ "${PROXY_STATUS}" = '"healthy"' ] && [ "${API_STATUS}" = '"healthy"' ]; then
    HEALTHY=true
    break
  fi

  sleep 2
  ELAPSED=$((ELAPSED + 2))
done

if [ "${HEALTHY}" != "true" ]; then
  echo "ERROR: Services failed to report healthy within ${TIMEOUT}s!"
  rollback
fi
echo "Services report healthy."

# 5. Execute smoke tests
echo "[5/5] Executing smoke tests over HTTPS..."
if ! "${SCRIPT_DIR}/smoke.sh"; then
  echo "ERROR: Smoke tests failed!"
  rollback
fi

# Record newly deployed tag
echo "${TARGET_TAG}" > "${STATE_FILE}"
echo "==> DEPLOYMENT COMPLETED SUCCESSFULLY: ${TARGET_TAG}"
