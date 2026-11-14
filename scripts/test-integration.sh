#!/usr/bin/env bash
# Purpose: Execute Phase 3 integration and concurrency tests against a temporary PostgreSQL container.
# Usage: ./scripts/test-integration.sh
# Exit code: 0 on success, non-zero on test failure.

set -euo pipefail

CONTAINER_NAME="cinema-test-postgres"
DB_PORT="5432"
DB_USER="test_user"
DB_PASSWORD="test_password"
DB_NAME="cinema_test_db"
TEST_URL="postgres://${DB_USER}:${DB_PASSWORD}@127.0.0.1:${DB_PORT}/${DB_NAME}?sslmode=disable"

cleanup() {
  echo "Cleaning up temporary test database container..."
  docker stop "${CONTAINER_NAME}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "Starting temporary PostgreSQL 16 container for testing..."
docker run -d --rm \
  --name "${CONTAINER_NAME}" \
  -e POSTGRES_DB="${DB_NAME}" \
  -e POSTGRES_USER="${DB_USER}" \
  -e POSTGRES_PASSWORD="${DB_PASSWORD}" \
  -p "${DB_PORT}:5432" \
  postgres:16-alpine >/dev/null

echo "Waiting for PostgreSQL to be ready..."
until docker exec "${CONTAINER_NAME}" pg_isready -U "${DB_USER}" -d "${DB_NAME}" >/dev/null 2>&1; do
  sleep 0.5
done
echo "PostgreSQL is ready."

echo "Running integration and high-concurrency race tests..."
TEST_DATABASE_URL="${TEST_URL}" go test -v -race -count=1 ./internal/booking/...
