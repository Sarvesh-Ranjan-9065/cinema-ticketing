#!/usr/bin/env bash
# Purpose: Rollback deployed application to a designated or previously recorded image tag.
# Usage: ./scripts/rollback.sh [IMAGE_TAG]
# Required Environment: Docker, Docker Compose, bash.
# Exit Codes: 0 on success, 1 on failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${SCRIPT_DIR}/.."
COMPOSE_FILE="${ROOT_DIR}/deploy/compose.yaml"
STATE_FILE="${ROOT_DIR}/deploy/.current_tag"

ROLLBACK_TAG="${1:-}"

if [ -z "${ROLLBACK_TAG}" ]; then
  if [ -f "${STATE_FILE}" ]; then
    ROLLBACK_TAG="$(cat "${STATE_FILE}")"
  else
    echo "ERROR: No target rollback image specified and no previous tag recorded in deploy/.current_tag"
    exit 1
  fi
fi

echo "==> Initiating rollback to image: ${ROLLBACK_TAG}"

# Ensure secrets are present
"${SCRIPT_DIR}/init-secrets.sh"

# Redeploy services with specified tag
IMAGE_TAG="${ROLLBACK_TAG}" docker compose -f "${COMPOSE_FILE}" up -d --remove-orphans api proxy

echo "==> Waiting for service health after rollback..."
TIMEOUT=30
ELAPSED=0
HEALTHY=false

while [ "${ELAPSED}" -lt "${TIMEOUT}" ]; do
  API_STATUS="$(docker inspect --format='{{json .State.Health.Status}}' "$(docker compose -f "${COMPOSE_FILE}" ps -q api 2>/dev/null)" 2>/dev/null || echo '"starting"')"
  PROXY_STATUS="$(docker inspect --format='{{json .State.Health.Status}}' "$(docker compose -f "${COMPOSE_FILE}" ps -q proxy 2>/dev/null)" 2>/dev/null || echo '"starting"')"

  if [ "${API_STATUS}" = '"healthy"' ] && [ "${PROXY_STATUS}" = '"healthy"' ]; then
    HEALTHY=true
    break
  fi

  sleep 2
  ELAPSED=$((ELAPSED + 2))
done

if [ "${HEALTHY}" != "true" ]; then
  echo "ERROR: Services failed to recover after rollback!"
  exit 1
fi

echo "==> Executing smoke tests on rolled back version..."
"${SCRIPT_DIR}/smoke.sh"

echo "${ROLLBACK_TAG}" > "${STATE_FILE}"
echo "==> ROLLBACK COMPLETED SUCCESSFULLY to ${ROLLBACK_TAG}."
