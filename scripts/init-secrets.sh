#!/usr/bin/env bash
# Purpose: Initialize Docker secret files for local development and deployment.
# Usage: ./scripts/init-secrets.sh
# Required Environment: Standard POSIX shell utilities (openssl/head/tr).
# Exit Codes: 0 on success, 1 on failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SECRETS_DIR="${SCRIPT_DIR}/../deploy/secrets"

mkdir -p "${SECRETS_DIR}"

PASSWORD_FILE="${SECRETS_DIR}/postgres_password.txt"
DATABASE_URL_FILE="${SECRETS_DIR}/database_url.txt"

# Generate password if not present
if [ ! -f "${PASSWORD_FILE}" ]; then
  echo "Generating database password secret..."
  # 32-character secure random password
  openssl rand -hex 16 > "${PASSWORD_FILE}"
fi

PASSWORD="$(cat "${PASSWORD_FILE}")"
DB_USER="cinema_user"
DB_NAME="cinema_db"
DB_HOST="db"
DB_PORT="5432"

# Construct connection URL for Docker network
echo "postgres://${DB_USER}:${PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable" > "${DATABASE_URL_FILE}"

# Permissions: 0644 so non-root containers (UID 65532) can read the mounted files.
chmod 644 "${PASSWORD_FILE}" "${DATABASE_URL_FILE}"

echo "Secrets successfully initialized with permissions 0644 in deploy/secrets/."
