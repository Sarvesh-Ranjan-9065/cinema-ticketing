# Evidence Index

This directory contains raw execution output and proof artifacts demonstrating that the cinema ticketing system meets all five operational acceptance criteria defined in the project plan.

Each file contains unedited command transcripts produced directly by the verification scripts and test runners.

## Acceptance Criteria Mapping

| Acceptance Test | Requirement | Evidence File | Script / Command |
|---|---|---|---|
| Acceptance 1 | Concurrency: simultaneous reservation attempts on the same seat cannot both confirm; race detector clean. | [`acceptance-1-concurrency.txt`](./acceptance-1-concurrency.txt) | `make test-race` (`go test -v -race ./...`) |
| Acceptance 2 | Deployment: clean environment builds and starts with migrations, seeds, and passing HTTPS smoke tests. | [`acceptance-2-deploy.txt`](./acceptance-2-deploy.txt) | `./scripts/deploy.sh cinema-api:local` |
| Acceptance 3 | CI Pipeline: local and GitHub Actions checks run formatting, linting, security scans, tests, and build. | [`acceptance-3-ci.txt`](./acceptance-3-ci.txt) | `./scripts/ci-local.sh` and `.github/workflows/ci.yml` |
| Acceptance 4 | Observability and Failure Drills: process crash recovery, database outage handling, and bad release rollback. | [`acceptance-4-drills.txt`](./acceptance-4-drills.txt) | `./scripts/drill-api-kill.sh`, `./scripts/drill-db-stop.sh`, `./scripts/drill-bad-release.sh` |
| Acceptance 5 | Disaster Recovery: database volume destruction, logical backup restore, constraint verification, and rollback. | [`acceptance-5-restore.txt`](./acceptance-5-restore.txt) | `./scripts/drill-restore.sh` |

## Evidence Descriptions

### Acceptance 1: Concurrency and Race Safety
- File: [`acceptance-1-concurrency.txt`](./acceptance-1-concurrency.txt)
- What it proves:
  - Unit tests verify domain validation, hold expiration logic, cancellation rules, and HTTP error mappings.
  - Integration test runs against real PostgreSQL 16 with `-race` enabled.
  - Start-barrier concurrency test launches 50 concurrent requests competing for the exact same seat on the same show. Exactly 1 request succeeds in creating the hold (HTTP 201), and the other 49 receive a conflict (HTTP 409 `SEAT_ALREADY_RESERVED`).
  - Zero data races detected by Go race detector.

### Acceptance 2: Clean Environment Deployment
- File: [`acceptance-2-deploy.txt`](./acceptance-2-deploy.txt)
- What it proves:
  - Secrets initialization via `scripts/init-secrets.sh` with secure file permissions (0600 / 0644).
  - Database schema migrations executed transactionally using `golang-migrate`.
  - Database seed executed idempotently to populate upcoming shows and seats.
  - Multi-container Docker Compose stack (`db`, `api`, `proxy`) starts cleanly.
  - Health checks verify service availability before traffic is routed.
  - HTTPS smoke tests execute end-to-end user journeys through the Caddy TLS reverse proxy.

### Acceptance 3: CI Pipeline and Security Scans
- File: [`acceptance-3-ci.txt`](./acceptance-3-ci.txt)
- What it proves:
  - All 10 local verification steps pass:
    1. Code formatting check (`gofmt -s`)
    2. Static analysis (`go vet`)
    3. Linter (`golangci-lint run`) with 0 issues
    4. Vulnerability scanning (`govulncheck`) with 0 vulnerabilities
    5. Secret scanning (`gitleaks`) with 0 leaked credentials
    6. Dockerfile linting (`hadolint`) with 0 violations
    7. Shell script linting (`shellcheck`) with 0 warnings
    8. Compose configuration validation (`docker compose config`)
    9. Real PostgreSQL integration race tests
    10. Docker image build and vulnerability scan (`trivy image`) with 0 vulnerabilities
  - GitHub Actions workflow (`.github/workflows/ci.yml`) runs the same validation suite in CI and records the image digest in the job summary upon publish.

### Acceptance 4: Observability and Failure Drills
- File: [`acceptance-4-drills.txt`](./acceptance-4-drills.txt)
- What it proves:
  - **Drill 1 (API Crash Recovery):** Simulates abrupt process death (`docker compose kill api`). The Caddy proxy returns HTTP 502 Bad Gateway during downtime. Docker restart policy brings the container back up within seconds. Data created before the crash is verified intact.
  - **Drill 2 (Database Outage Handling):** Database container is stopped. The `/healthz` liveness probe remains HTTP 200 (process is alive), while `/readyz` and business endpoints return HTTP 503 (`DEPENDENCY_UNAVAILABLE`). Once the database is restarted, the API automatically reconnects and resumes serving traffic without restarting the API process.
  - **Drill 3 (Bad Release Rollback):** Deploys a deliberately broken container image tag (`cinema-api:broken-release`). The deployment health gate detects probe failures, aborts deployment, and automatically rolls back to the previous stable release tag, restoring normal service.

### Acceptance 5: Backup, Disaster Recovery, and Data Integrity
- File: [`acceptance-5-restore.txt`](./acceptance-5-restore.txt)
- What it proves:
  - A known reservation is created and confirmed in the database.
  - Logical backup is taken using `scripts/backup.sh` producing a PostgreSQL 16 custom format dump (`.dump`) with SHA256 checksum verification.
  - Catastrophic failure is simulated by stopping containers and wiping the persistent database volume (`docker compose down -v`).
  - An empty database volume is started.
  - Database is restored using `scripts/restore.sh --confirm <BACKUP_FILE>`.
  - Verification checks confirm that table rows, foreign keys, and the partial unique index `idx_reservations_active_seat` are fully restored.
  - The pre-disaster reservation is queried and verified intact.
  - Automated smoke test passes after restore.
