# Cinema Show and Seat Reservation API

A cinema show and seat reservation API designed for deterministic operations: containerization, CI/CD, local TLS termination, structured observability, automated backup/restore/rollback, and disaster recovery.

The primary design requirement is zero double-booking under high concurrency, enforced through PostgreSQL partial unique indexes and database-clock-governed hold expirations.

---

## Architecture Summary

The platform consists of three decoupled services orchestrated by Docker Compose across isolated bridge networks:

- **Reverse Proxy (`proxy`):** Caddy terminates HTTPS using an internal certificate authority, enforces host port isolation, and reverse-proxies inbound requests to the API.
- **Application API (`api`):** A Go service built exclusively using the Go standard library (`net/http`, `log/slog`, `database/sql` with `pgx/v5`). Operates as a single multi-call binary with subcommands: `serve`, `healthcheck`, `migrate`, and `seed`.
- **Database (`db`):** PostgreSQL 16 enforcing relational constraints, transactional isolation, and partial unique indexes.

Detailed architecture diagrams, sequence flows, and trade-off rationales are documented in [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

```
                      +-------------------+
                      |   Client / curl   |
                      +---------+---------+
                                | HTTPS :8443
                                v
                   [ public_net bridge network ]
                   +---------------------------+
                   |       Caddy Proxy         |
                   | (Internal CA, TLS Term)   |
                   +-------------+-------------+
                                 | HTTP :8080
                                 v
                  [ backend_net bridge network ]
                  +----------------------------+
                  |       Cinema API (Go)      |
                  |  (Non-root UID 10001,      |
                  |   Cap drop ALL)            |
                  +--------------+-------------+
                                 | TCP :5432
                                 v
                  +----------------------------+
                  |     PostgreSQL 16 DB       |
                  | (No published host ports)  |
                  +--------------+-------------+
                                 |
                          [ Named Volume ]
                          (db_data on host)
```

---

## Core Invariants

1. **Zero Double-Booking:** At most one active reservation (`HELD` or `CONFIRMED`) can exist per seat per show. Enforced by PostgreSQL partial unique index `idx_reservations_active_seat`.
2. **Database Clock Authority:** Hold expiration decisions are governed strictly by database server time (`now()`), preventing application server clock drift from affecting validity.
3. **No Background Dependency:** Expired holds are recognized immediately during queries; correctness does not depend on background cleaner workers.
4. **Cryptographic Ticket Uniqueness:** Confirmed reservations generate an unguessable 64-bit random ticket identifier (`TKT-<16 hex>`) backed by a unique index and check constraint.
5. **Immediate Seat Release:** Cancelling a hold or confirmed ticket releases the seat immediately for other users.

---

## Prerequisites

- **Docker:** Engine version 24.0 or newer
- **Docker Compose:** Version 2.20 or newer
- **Bash:** Version 4.0 or newer
- **curl:** Supporting HTTPS requests
- **Go (Optional):** Version 1.24 or newer (required only for building or testing outside containers)

---

## Setup and Quickstart

### 1. Clone Repository and Initialize Secrets
```bash
git clone <repository-url>
cd cinema-ticketing

# Generate local file-based Docker secrets with secure permissions (0600 / 0644)
./scripts/init-secrets.sh
```

### 2. Build and Deploy
```bash
# Build production container image locally
docker build -t cinema-api:local .

# Deploy the stack with automated migrations, seed, and health check gate
./scripts/deploy.sh cinema-api:local
```

### 3. Verify Deployment
```bash
# Execute end-to-end HTTPS smoke tests through the Caddy proxy
./scripts/smoke.sh
```

---

## Port Mappings and Networking

| Service | Container Port | Host Port | Network | Purpose |
|---|---|---|---|---|
| `proxy` | 443 | 8443 | `public_net` | HTTPS public API ingress with local TLS termination. |
| `api` | 8080 | Not published | `public_net`, `backend_net` | Internal HTTP application server. |
| `db` | 5432 | Not published | `backend_net` | Internal PostgreSQL relational database. |

---

## Configuration

Configuration values are defined in `.env` (or inherited from environment variables) and can be overridden via Docker secrets.

| Environment Variable | Default Value | Description |
|---|---|---|
| `HTTP_HOST` | `0.0.0.0` | Bind address for API HTTP server. |
| `HTTP_PORT` | `8080` | Port for API HTTP server. |
| `HOST_PORT` | `8443` | Host port exposed by Caddy reverse proxy for HTTPS traffic. |
| `LOG_LEVEL` | `INFO` | Logging verbosity (`DEBUG`, `INFO`, `WARN`, `ERROR`). |
| `HOLD_TTL` | `120s` | Duration for temporary seat holds. |
| `DATABASE_URL` | *(empty)* | PostgreSQL connection URL string. |
| `DATABASE_URL_FILE` | `/run/secrets/database_url` | Path to secret file containing PostgreSQL connection string. |
| `DB_MAX_OPEN_CONNS` | `25` | Maximum open connections in database connection pool. |
| `DB_MAX_IDLE_CONNS` | `10` | Maximum idle connections in database connection pool. |
| `DB_CONN_MAX_LIFETIME` | `15m` | Maximum connection lifetime in connection pool. |

---

## Testing

### Unit and Race Tests
```bash
# Run unit tests
make test

# Run race detector across all packages
make test-race
```

### Integration Concurrency Test
Runs against a real PostgreSQL 16 database instance, firing 50 concurrent requests simultaneously at the same seat using a synchronization barrier:
```bash
make test-integration
```

### Local CI Pipeline
Executes all 10 CI validation steps locally (formatting, vet, lint, dependency vulnerabilities, secret leak detection, Dockerfile lint, shellcheck, Compose validation, integration tests, and container vulnerability scan):
```bash
make ci-local
```

---

## Operations and Disaster Recovery

Comprehensive operational playbooks and incident response procedures are in [`docs/RUNBOOK.md`](docs/RUNBOOK.md).

### Deploying a Release
```bash
./scripts/deploy.sh cinema-api:<TAG>
```

### Rolling Back a Release
```bash
# Rollback to the previous stable release recorded in deploy/.current_tag
./scripts/rollback.sh
```

### Backup and Restore
```bash
# Create a logical database backup (PostgreSQL custom format dump with SHA256 checksum)
./scripts/backup.sh

# Restore from a verified backup (requires explicit confirmation flag)
./scripts/restore.sh --confirm backups/cinema_db_YYYYMMDD_HHMMSSZ.dump
```

### Automated End-to-End Demonstration
Runs a complete operational walkthrough including health checks, HTTPS smoke tests, log inspection, container crash recovery, and disaster recovery restore:
```bash
make demo
```

---

## API Reference Summary

Full request/response examples and error code definitions are in [`docs/API.md`](docs/API.md).

| Method | Endpoint | Description | Status Codes |
|---|---|---|---|
| `GET` | `/healthz` | Process liveness probe. | `200` |
| `GET` | `/readyz` | Database connectivity readiness probe. | `200`, `503` |
| `GET` | `/v1/shows` | List upcoming shows scheduled after `now()`. | `200` |
| `GET` | `/v1/shows/{id}/seats` | Seat map with statuses (`AVAILABLE`, `HELD`, `CONFIRMED`). | `200`, `404` |
| `POST` | `/v1/shows/{id}/reservations` | Create temporary 120-second hold on a single seat. | `201`, `400`, `404`, `409` |
| `POST` | `/v1/reservations/{id}/confirm` | Confirm hold into ticket (idempotent). | `200`, `404`, `409` |
| `POST` | `/v1/reservations/{id}/cancel` | Cancel reservation and release seat immediately. | `200`, `404` |

---

## Acceptance Criteria and Evidence Index

Raw execution transcripts verifying each acceptance criteria are stored in `docs/evidence/`:

| Acceptance Test | Requirement | Evidence Log |
|---|---|---|
| Acceptance 1 | Concurrency: simultaneous reservation attempts cannot both confirm; race detector clean. | [`docs/evidence/acceptance-1-concurrency.txt`](docs/evidence/acceptance-1-concurrency.txt) |
| Acceptance 2 | Deployment: clean environment builds and starts with migrations, seeds, and passing HTTPS smoke tests. | [`docs/evidence/acceptance-2-deploy.txt`](docs/evidence/acceptance-2-deploy.txt) |
| Acceptance 3 | CI Pipeline: local and GitHub Actions checks run formatting, linting, security scans, tests, and build. | [`docs/evidence/acceptance-3-ci.txt`](docs/evidence/acceptance-3-ci.txt) |
| Acceptance 4 | Observability and Failure Drills: process crash recovery, database outage handling, and bad release rollback. | [`docs/evidence/acceptance-4-drills.txt`](docs/evidence/acceptance-4-drills.txt) |
| Acceptance 5 | Disaster Recovery: database volume destruction, logical backup restore, constraint verification, and rollback. | [`docs/evidence/acceptance-5-restore.txt`](docs/evidence/acceptance-5-restore.txt) |

See [`docs/evidence/README.md`](docs/evidence/README.md) for detailed descriptions.

---

## Known Limitations and Assumptions

- **Single Seat per Reservation:** The API deliberately supports reserving one seat per transaction. Multi-seat shopping carts are out of scope.
- **Fixed Hall Geometry:** Each show is modeled with a standard 50-seat grid (rows A through E, numbers 1 through 10). Dynamic auditorium geometry is not supported.
- **Simulated Identity and Billing:** User accounts, authentication headers, and payment gateway interactions are omitted by specification; customer references are arbitrary strings supplied by the client.
- **Local TLS Certificate:** Caddy issues certificates via its internal CA. Clients connecting via `curl` must supply `-k` or trust the local root certificate generated in the `caddy_data` volume.

---
