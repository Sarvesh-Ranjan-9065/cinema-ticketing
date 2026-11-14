#!/usr/bin/env bash
# Purpose: Execute the complete CI validation pipeline locally before pushing to remote.
# Usage: ./scripts/ci-local.sh
# Required Environment: Go, Docker, golangci-lint, bash.
# Exit Codes: 0 on success, 1 on failure of any check.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${SCRIPT_DIR}/.."

cd "${ROOT_DIR}"

echo "======================================================"
echo " Starting Local CI Pipeline Verification"
echo "======================================================"

# 1. Format check
echo "[1/10] Verifying Go code formatting (gofmt)..."
DIFF=$(gofmt -s -d .)
if [ -n "${DIFF}" ]; then
  echo "FAIL: Unformatted Go code detected:"
  echo "${DIFF}"
  exit 1
fi
echo "PASS: Code formatting is clean."

# 2. Go vet
echo "[2/10] Running Go vet..."
go vet ./...
echo "PASS: Go vet passed."

# 3. Golangci-lint
echo "[3/10] Running golangci-lint..."
golangci-lint run ./...
echo "PASS: golangci-lint passed with zero issues."

# 4. Govulncheck
echo "[4/10] Scanning dependencies with govulncheck..."
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
echo "PASS: govulncheck found zero vulnerabilities."

# 5. Secret scan with Gitleaks
echo "[5/10] Scanning repository for secrets (gitleaks)..."
docker run --rm -v "${ROOT_DIR}:/path" zricethezav/gitleaks:latest detect --source="/path" --redact
echo "PASS: gitleaks found zero secret leaks."

# 6. Dockerfile lint with Hadolint
echo "[6/10] Linting Dockerfile (hadolint)..."
docker run --rm -i hadolint/hadolint < Dockerfile
echo "PASS: hadolint passed with zero Dockerfile issues."

# 7. Shell script lint with ShellCheck
echo "[7/10] Linting shell scripts (shellcheck)..."
docker run --rm -v "${ROOT_DIR}:/work" -w /work koalaman/shellcheck:latest scripts/*.sh
echo "PASS: shellcheck passed on all scripts."

# 8. Compose configuration validation
echo "[8/10] Validating Docker Compose configuration..."
docker compose -f deploy/compose.yaml config >/dev/null
echo "PASS: Docker Compose configuration is valid."

# 9. Integration and Concurrency tests with Race Detector
echo "[9/10] Running integration and concurrency tests with race detector..."
./scripts/test-integration.sh
echo "PASS: All integration and race tests passed."

# 10. Container image build and Trivy vulnerability scan
echo "[10/10] Building container image and scanning with Trivy..."
docker build -t cinema-api:ci-local .
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock \
  aquasec/trivy:latest image \
  --severity HIGH,CRITICAL \
  --exit-code 1 \
  cinema-api:ci-local
echo "PASS: Trivy found zero HIGH or CRITICAL vulnerabilities."

echo "======================================================"
echo " LOCAL CI COMPLETED SUCCESSFULLY (ALL 10 CHECKS PASSED)"
echo "======================================================"
