#!/usr/bin/env bash
# Purpose: Create a consistent logical database backup in custom format, generate checksum, verify archive, and apply retention.
# Usage: ./scripts/backup.sh [BACKUP_DIR]
# Required Environment: Docker, Docker Compose, sha256sum.
# Exit Codes: 0 on success, 1 on failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${SCRIPT_DIR}/.."
COMPOSE_FILE="${ROOT_DIR}/deploy/compose.yaml"
BACKUP_DIR="${1:-${ROOT_DIR}/backups}"

mkdir -p "${BACKUP_DIR}"

TIMESTAMP="$(date -u +"%Y%m%d_%H%M%SZ")"
BACKUP_FILE="${BACKUP_DIR}/cinema_db_${TIMESTAMP}.dump"
CHECKSUM_FILE="${BACKUP_FILE}.sha256"

echo "==> Creating logical PostgreSQL backup: ${BACKUP_FILE}..."

# Execute pg_dump directly inside the database container to match PostgreSQL engine version exactly
docker compose -f "${COMPOSE_FILE}" exec -T db \
  pg_dump -U cinema_user -d cinema_db -Fc > "${BACKUP_FILE}"

# Restrictive file permissions (owner read-only)
chmod 600 "${BACKUP_FILE}"

# Generate SHA256 checksum
(cd "${BACKUP_DIR}" && sha256sum "$(basename "${BACKUP_FILE}")" > "$(basename "${CHECKSUM_FILE}")")
chmod 600 "${CHECKSUM_FILE}"
echo "==> Generated SHA256 checksum: $(cat "${CHECKSUM_FILE}")"

# Verify backup integrity by listing the archive contents with pg_restore
echo "==> Verifying archive integrity with pg_restore -l..."
docker compose -f "${COMPOSE_FILE}" exec -T db \
  pg_restore -l < "${BACKUP_FILE}" >/dev/null

echo "==> Backup verified successfully."

# Retention policy: retain the latest 5 backups, never delete if only 1 exists
MAX_BACKUPS=5
BACKUP_COUNT="$(find "${BACKUP_DIR}" -maxdepth 1 -name "cinema_db_*.dump" | wc -l)"

if [ "${BACKUP_COUNT}" -gt "${MAX_BACKUPS}" ]; then
  echo "==> Applying retention: pruning backups older than latest ${MAX_BACKUPS}..."
  find "${BACKUP_DIR}" -maxdepth 1 -name "cinema_db_*.dump" | sort | head -n -"${MAX_BACKUPS}" | while read -r old_dump; do
    echo "Pruning old backup: ${old_dump}"
    rm -f "${old_dump}" "${old_dump}.sha256"
  done
fi

echo "==> BACKUP COMPLETED: ${BACKUP_FILE}"
