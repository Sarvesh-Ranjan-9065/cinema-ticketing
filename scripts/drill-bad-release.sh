#!/usr/bin/env bash
# Purpose: Execute failure drill 3 - deploy a broken image tag, verify health gate rejection, and verify automatic rollback to previous stable tag.
# Usage: ./scripts/drill-bad-release.sh
# Required Environment: Docker, Docker Compose, bash.
# Exit Codes: 0 on success, 1 on failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${SCRIPT_DIR}/.."
STATE_FILE="${ROOT_DIR}/deploy/.current_tag"
COMPOSE_FILE="${ROOT_DIR}/deploy/compose.yaml"

echo "=========================================================="
echo " Starting Drill 3: Broken Release & Automatic Rollback"
echo "=========================================================="

# 1. Establish current stable tag
if [ ! -f "${STATE_FILE}" ]; then
  echo "Recording initial stable tag..."
  ./scripts/deploy.sh cinema-api:local >/dev/null
fi
STABLE_TAG="$(cat "${STATE_FILE}")"
echo "Current stable tag: ${STABLE_TAG}"

# 2. Create deliberately broken image tag (using an image that cannot run cinema-api migrate/serve)
BROKEN_TAG="cinema-api:broken-test-tag"
echo "[1/4] Tagging deliberately broken image (${BROKEN_TAG})..."
docker tag alpine:latest "${BROKEN_TAG}"

cleanup() {
  echo "Cleaning up temporary broken image tag..."
  docker rmi "${BROKEN_TAG}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# 3. Attempt to deploy broken image
echo "[2/4] Attempting to deploy broken release with scripts/deploy.sh..."
DEPLOY_FAILED=false

# We expect deploy.sh to fail with exit code 1
if ! "${SCRIPT_DIR}/deploy.sh" "${BROKEN_TAG}"; then
  DEPLOY_FAILED=true
fi

if [ "${DEPLOY_FAILED}" != "true" ]; then
  echo "FAIL: deploy.sh succeeded unexpectedly with a broken image tag!"
  exit 1
fi
echo "PASS: deploy.sh detected broken release and triggered automatic rollback."

# 4. Verify system has rolled back to stable tag
echo "[3/4] Verifying active container image matches stable tag..."
ACTIVE_IMAGE="$(docker inspect --format='{{.Config.Image}}' "$(docker compose -f "${COMPOSE_FILE}" ps -q api)")"
if [ "${ACTIVE_IMAGE}" != "${STABLE_TAG}" ]; then
  echo "FAIL: Expected active image to be ${STABLE_TAG}, but found ${ACTIVE_IMAGE}"
  exit 1
fi
echo "PASS: Active image is confirmed restored to stable tag: ${ACTIVE_IMAGE}."

# 5. Run smoke tests on restored stack
echo "[4/4] Executing smoke tests on restored stack..."
"${SCRIPT_DIR}/smoke.sh"
echo "PASS: System is fully operational following automatic rollback."

echo "=========================================================="
echo " DRILL 3 COMPLETED SUCCESSFULLY: Bad release rejected,"
echo " health gate tripped, and automatic rollback succeeded."
echo "=========================================================="
