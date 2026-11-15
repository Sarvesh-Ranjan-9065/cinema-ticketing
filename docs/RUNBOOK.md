# Operational Runbook

This runbook outlines operational procedures, failure response workflows, disaster recovery protocols, and troubleshooting steps for the cinema ticketing platform.

---

## 1. Routine Operational Commands

### 1.1 Service Health Probes
```bash
# Check liveness probe (does not hit the database)
curl -k https://localhost/healthz

# Check readiness probe (probes PostgreSQL connectivity)
curl -k https://localhost/readyz
```

### 1.2 Inspect Container Status
```bash
docker compose -f deploy/compose.yaml ps
```

### 1.3 View and Follow Structured Logs
The API emits structured JSON logs via standard output.

```bash
# Stream all API logs in real-time
docker compose -f deploy/compose.yaml logs -f api

# View the last 50 log entries from the proxy
docker compose -f deploy/compose.yaml logs --tail=50 proxy

# View logs from database container
docker compose -f deploy/compose.yaml logs --tail=50 db
```

### 1.4 Restarting Individual Services
```bash
# Restart the API service gracefully
docker compose -f deploy/compose.yaml restart api

# Restart the Caddy reverse proxy
docker compose -f deploy/compose.yaml restart proxy
```

---

## 2. Deployment and Rollback Procedures

### 2.1 Standard Deployment
Deployments are performed using `scripts/deploy.sh`, which runs schema migrations, verifies seed data, applies new container image tags, and gates traffic behind health checks.

```bash
# Deploy a specific versioned image tag
./scripts/deploy.sh cinema-api:<TAG>

# Example: deploy locally built image
./scripts/deploy.sh cinema-api:local
```

### 2.2 Manual Rollback
If a deployment exhibits regressions after passing health checks, execute `scripts/rollback.sh`. The script retrieves the last recorded stable image tag from `deploy/.current_tag` and redeploys it.

```bash
# Automatic rollback to the previous recorded stable release
./scripts/rollback.sh

# Or rollback explicitly to a known image tag
./scripts/rollback.sh cinema-api:<STABLE_TAG>
```

---

## 3. Disaster Recovery and Database Operations

### 3.1 Logical Database Backup
Logical backups use PostgreSQL 16 custom format (`pg_dump -Fc`), verify archive readability, and generate SHA256 checksums in the `backups/` directory.

```bash
# Generate a timestamped backup
./scripts/backup.sh
```

Output format:
- Archive: `backups/cinema_db_YYYYMMDD_HHMMSSZ.dump`
- Checksum: `backups/cinema_db_YYYYMMDD_HHMMSSZ.dump.sha256`

Permissions on backup files are restricted to `0600`. The backup script automatically retains the 5 most recent backups and never deletes the only existing backup.

### 3.2 Database Restore
Restoring a database replaces the target schema and table contents with data from a verified backup archive. Writes are stopped during restore to prevent data inconsistency.

> **Caution:** Database restore is a destructive operation on existing data. An explicit `--confirm` flag is required.

```bash
# Perform restore from a specific dump file
./scripts/restore.sh --confirm backups/cinema_db_YYYYMMDD_HHMMSSZ.dump
```

The restore script automatically:
1. Validates the SHA256 checksum of the backup archive.
2. Stops the API container to prevent concurrent writes.
3. Restores schema, table data, and indexes using `pg_restore --clean --if-exists`.
4. Validates table row counts and partial unique indexes.
5. Restarts application containers and executes HTTPS smoke tests.

---

## 4. Incident Response Workflows

### Incident 1: API Service Unhealthy (502 Bad Gateway)
- **Symptom:** Clients receive HTTP 502 Bad Gateway from the reverse proxy; `/healthz` fails.
- **Immediate Steps:**
  1. Inspect container states:
     ```bash
     docker compose -f deploy/compose.yaml ps
     ```
  2. Inspect API crash logs:
     ```bash
     docker compose -f deploy/compose.yaml logs --tail=100 api
     ```
  3. If container is stopped, verify Docker restart policy:
     ```bash
     docker compose -f deploy/compose.yaml up -d api
     ```

### Incident 2: Database Outage (503 Service Unavailable)
- **Symptom:** `/healthz` returns 200 OK, but `/readyz` and business endpoints return 503 `DEPENDENCY_UNAVAILABLE`.
- **Immediate Steps:**
  1. Inspect database container health:
     ```bash
     docker compose -f deploy/compose.yaml ps db
     ```
  2. Check database engine logs for out-of-memory or disk exhaustion:
     ```bash
     docker compose -f deploy/compose.yaml logs --tail=100 db
     ```
  3. Restart database container if halted:
     ```bash
     docker compose -f deploy/compose.yaml up -d db
     ```
  4. Verify automatic recovery: API automatically re-establishes pool connections once PostgreSQL reports healthy.

### Incident 3: Deployment Failure / Crash Loop
- **Symptom:** `scripts/deploy.sh` reports health timeout during deployment.
- **Immediate Steps:**
  1. The deployment script triggers an automated rollback to the previous tag.
  2. Review migration logs:
     ```bash
     docker compose -f deploy/compose.yaml logs migrate
     ```
  3. If manual intervention is required:
     ```bash
     ./scripts/rollback.sh
     ```

---

## 5. Troubleshooting Matrix

| Symptom | Probable Cause | Diagnostic Check | Resolution |
|---|---|---|---|
| HTTP 502 Bad Gateway on all requests | API container exited or listening on wrong port | `docker compose -f deploy/compose.yaml ps api` | Inspect `docker compose logs api`. Ensure container starts with command `serve` on port 8080. |
| HTTP 503 `DEPENDENCY_UNAVAILABLE` on `/readyz` | PostgreSQL container unreachable or saturating connection pool | `docker compose -f deploy/compose.yaml exec db pg_isready -U cinema_user -d cinema_db` | Verify database container is running. Check connection pool limits (`DB_MAX_OPEN_CONNS`) and database logs. |
| HTTP 409 `SEAT_TAKEN` | Another client created an active hold or confirmed booking on the seat | Query `/v1/shows/{id}/seats` to view seat status | This is standard concurrency protection. Select another available seat or await hold expiry. |
| HTTP 409 `HOLD_EXPIRED` on confirmation | Hold expired prior to confirmation request submission | Compare `hold_expires_at` in reservation with current database time (`SELECT now()`) | Re-hold the seat if still available; client must confirm within the configured hold TTL. |
| Migration failure on startup | Locked migration table or unparseable migration SQL | `docker compose -f deploy/compose.yaml logs migrate` | Verify database accessibility and ensure migration filenames follow sequential numeric versioning. |
| `permission denied` on secret files | Secret file permissions too restrictive for non-root user (UID 65532) | `ls -la deploy/secrets/` | Run `scripts/init-secrets.sh` to apply standard read permissions (`0644`). |
| Missing disk space in Docker volume | Unbounded database logging or excessive dump accumulation | `df -h` and `docker system df` | Purge obsolete test backups from `backups/` or execute `docker system prune`. |
