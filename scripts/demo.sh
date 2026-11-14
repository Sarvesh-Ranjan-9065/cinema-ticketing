#!/usr/bin/env bash
# Purpose: Execute end-to-end operational demonstration (pipeline check, deployed HTTPS smoke, logs, failure drill, restore drill).
# Usage: ./scripts/demo.sh
# Required Environment: Docker, Docker Compose, curl, bash.
# Exit Codes: 0 on success, 1 on failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${SCRIPT_DIR}/.."
COMPOSE_FILE="${ROOT_DIR}/deploy/compose.yaml"

echo "================================================================================"
echo "Cinema Reservation API - Automated Demonstration"
echo "================================================================================"

# 1. Ensure secrets exist
echo ""
echo "[Step 1/5] Checking secrets and environment..."
"${SCRIPT_DIR}/init-secrets.sh"

# 2. Verify containers are running
echo ""
echo "[Step 2/5] Checking container status..."
docker compose -f "${COMPOSE_FILE}" ps

# 3. Test HTTPS endpoints and seat reservation flow
echo ""
echo "[Step 3/5] Executing HTTPS smoke tests against proxy..."
"${SCRIPT_DIR}/smoke.sh"

# 4. Show structured JSON logging from the API
echo ""
echo "[Step 4/5] Inspecting recent structured JSON logs from API..."
docker compose -f "${COMPOSE_FILE}" logs --tail=10 api

# 5. Run API crash and automatic recovery drill
echo ""
echo "[Step 5/5] Running operational failure drills..."
echo "--- Running Drill 1: API process kill and automatic restart ---"
"${SCRIPT_DIR}/drill-api-kill.sh"

echo ""
echo "--- Running Drill 2: Database backup, volume destruction, and disaster recovery ---"
"${SCRIPT_DIR}/drill-restore.sh"

echo ""
echo "================================================================================"
echo "Demonstration completed successfully."
echo "================================================================================"
