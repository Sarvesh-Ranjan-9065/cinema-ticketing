# Architecture and Design

This document describes the software architecture, data modeling, concurrency control, and security design of the cinema reservation system.

---

## 1. System Topology and Container Architecture

The system is deployed as three decoupled container services managed by Docker Compose across isolated internal networks. Only the reverse proxy exposes a network port to the host system.

```mermaid
flowchart TD
    Client["Client / User Agent"]

    subgraph Host["Host Machine"]
        Port8443["Host Port 8443 (HTTPS)"]

        subgraph DockerCompose["Docker Compose Environment"]
            subgraph PublicNetwork["public_net (Internal Bridge Network)"]
                Proxy["Proxy Container (Caddy)\n- TLS Termination (Internal CA)\n- HTTPS Reverse Proxy\n- Static Asset / Route Filtering"]
            end

            subgraph BackendNetwork["backend_net (Internal Bridge Network)"]
                API["API Container (Go cinema-api)\n- Subcommands: serve, healthcheck, migrate, seed\n- Standard Library net/http & log/slog\n- Non-root user (UID 10001)\n- Cap Drop ALL, no-new-privileges"]
                DB["Database Container (PostgreSQL 16)\n- Port 5432 (Internal Only)\n- ACID Transactions\n- Relational Constraints"]
            end

            subgraph Volumes["Named Docker Volumes"]
                VolDB[("db_data\nPersistent PostgreSQL Storage")]
                VolCaddyData[("caddy_data\nTLS Certificates & Internal CA")]
                VolCaddyCfg[("caddy_config\nCaddy Operational State")]
            end

            subgraph Secrets["Docker Secrets (Read-Only Mounts)"]
                SecDBUrl["/run/secrets/database_url"]
                SecDBPass["/run/secrets/postgres_password"]
            end
        end
    end

    Client -->|HTTPS :8443| Port8443
    Port8443 --> Proxy
    Proxy -->|HTTP :8080| API
    API -->|TCP :5432| DB

    DB --- VolDB
    Proxy --- VolCaddyData
    Proxy --- VolCaddyCfg
    API -.-> SecDBUrl
    DB -.-> SecDBPass
```

### Network Isolation Rules
- **No published database port:** PostgreSQL listens on port 5432 exclusively inside `backend_net`. Neither the host machine nor the `public_net` reverse proxy can access the database directly.
- **Single ingress point:** Caddy is the only service that publishes a host port (`8443` mapping to container port `443`). Direct HTTP access to the Go API container is restricted to `public_net`.
- **Inter-service routing:** Containers communicate using Docker internal DNS hostnames (`api` and `db`).

---

## 2. CI/CD and Delivery Pipeline Flow

Every commit and pull request passes through an automated validation and build pipeline before any container image can be published or deployed.

```mermaid
flowchart LR
    DevCommit["Git Commit / PR"] --> CIJob["GitHub Actions CI Pipeline"]

    subgraph CIQualityGates["Quality and Security Gates"]
        G1["gofmt & go vet"]
        G2["golangci-lint (Static Analysis)"]
        G3["govulncheck (Go Vulnerability DB)"]
        G4["gitleaks (Secret Detection)"]
        G5["hadolint (Dockerfile Lint)"]
        G6["shellcheck (Shell Scripts)"]
        G7["docker compose config (Syntax)"]
        G8["Real PostgreSQL Integration Tests (-race)"]
    end

    CIJob --> CIQualityGates
    CIQualityGates --> Build["Multi-Stage Docker Build\n(golang:1.27-alpine -> minimal alpine)"]
    Build --> Scan["Trivy Container Scan\n(Severity: HIGH,CRITICAL)"]
    Scan --> Publish["GHCR Publish\n(Tags: Commit SHA, SemVer)"]
    Publish --> Deploy["Deployment Script (deploy.sh)\n1. Migrations\n2. Seed\n3. Health Verification Gate\n4. Automated Rollback on Error"]
```

---

## 3. Data Model and Relational Schema

The PostgreSQL schema enforces data integrity through foreign keys, check constraints, and partial indexes.

```mermaid
erDiagram
    SHOWS ||--o{ SEATS : "contains"
    SHOWS ||--o{ RESERVATIONS : "has"
    SEATS ||--o{ RESERVATIONS : "reserved by"

    SHOWS {
        uuid id PK
        varchar title
        timestamptz starts_at
        timestamptz created_at
    }

    SEATS {
        uuid id PK
        uuid show_id FK
        varchar seat_number
        timestamptz created_at
    }

    RESERVATIONS {
        uuid id PK
        uuid show_id FK
        varchar seat_number
        varchar customer_reference
        varchar status
        timestamptz expires_at
        timestamptz confirmed_at
        timestamptz cancelled_at
        varchar ticket_id UK
        varchar idempotency_key
        timestamptz created_at
        timestamptz updated_at
    }
```

### Relational Constraints
- `idx_reservations_active_seat`: Partial unique index on `(show_id, seat_number)` where `status IN ('HELD', 'CONFIRMED')`. Prevents concurrent holds or confirmations on the same seat.
- `reservations_status_check`: Check constraint enforcing `status IN ('HELD', 'CONFIRMED', 'CANCELLED', 'EXPIRED')`.
- `reservations_ticket_required`: Check constraint ensuring that whenever `status = 'CONFIRMED'`, `ticket_id IS NOT NULL` and `confirmed_at IS NOT NULL`.
- `idx_reservations_ticket_id`: Unique index ensuring global uniqueness of ticket identifiers.

---

## 4. Concurrency Control and Hold Lifecycle

The reservation lifecycle consists of three distinct states: `HELD`, `CONFIRMED`, and `CANCELLED` (or `EXPIRED`).

```mermaid
stateDiagram-v2
    [*] --> AVAILABLE : Initial State
    AVAILABLE --> HELD : POST /shows/{id}/reservations\n(Hold duration: 120s)
    HELD --> CONFIRMED : POST /reservations/{id}/confirm\n(within hold TTL)
    HELD --> CANCELLED : POST /reservations/{id}/cancel\n(Immediate seat release)
    HELD --> AVAILABLE : Expiry (Database now() > expires_at)\nSeat automatically available
    CONFIRMED --> CANCELLED : POST /reservations/{id}/cancel\n(Immediate seat release)
    CANCELLED --> AVAILABLE : Seat immediately re-reservable
```

### Concurrent Hold Request Resolution

When multiple clients attempt to reserve the same seat simultaneously, PostgreSQL transaction isolation and the partial unique index guarantee that exactly one transaction succeeds.

```mermaid
sequenceDiagram
    autonumber
    actor ClientA as Client A
    actor ClientB as Client B
    participant API as API Server
    participant DB as PostgreSQL 16

    Note over ClientA,ClientB: Simultaneous requests for Show S1, Seat C4
    ClientA->>+API: POST /shows/S1/reservations {seat: "C4"}
    ClientB->>+API: POST /shows/S1/reservations {seat: "C4"}

    API->>DB: BEGIN Transaction
    API->>DB: Check seat existence and show timing
    API->>DB: INSERT INTO reservations (show_id, seat_number, status, expires_at)\nVALUES (S1, 'C4', 'HELD', now() + 120s)

    alt Client A inserts first
        DB-->>API: INSERT 1 (Success)
        API->>DB: COMMIT
        API-->>ClientA: 201 Created (Hold ID, expires_at)
    else Client B attempts duplicate active hold
        DB-->>API: ERROR: duplicate key value violates unique constraint "idx_reservations_active_seat"
        API->>DB: ROLLBACK
        API-->>ClientB: 409 Conflict (SEAT_ALREADY_RESERVED)
    end
```

### Guarantees Enforced at Database Level
1. **Clock Source Independence:** Hold expiration is evaluated using `now()` on the PostgreSQL database server. Application server clock drift has zero effect on reservation validity.
2. **Zero Dependency on Background Workers:** An expired hold (`now() > expires_at`) is recognized as expired immediately during read and write queries. The database does not require a background sweeper to maintain consistency.
3. **Cryptographic Ticket Uniqueness:** Confirmed reservations generate a 64-bit random ticket string formatted as `TKT-<16 hex chars>` derived from `crypto/rand`. In the event of a theoretical collision, a unique constraint violation triggers a retry.
4. **Idempotency:**
   - Repeating a hold request with an identical `Idempotency-Key` returns the original reservation record.
   - Confirming an already confirmed reservation returns the existing ticket identifier with HTTP 200.
   - Cancelling an already cancelled reservation returns HTTP 200 without side effects.

---

## 5. Security Architecture and Hardening

The application runtime is hardened across container, operating system, and network boundaries:

1. **Non-Root Execution:** The application container runs under UID `10001` (`appuser`). The container never acquires root privileges.
2. **Capability Dropping:** Docker Compose specifies `cap_drop: [ALL]` and `security_opt: [no-new-privileges:true]` on all application containers.
3. **Secret Files:** Sensitive credentials are supplied via Docker secrets or bind-mounted files (`DATABASE_URL_FILE`), read directly from disk into application memory without appearing in environment variables or container inspection output.
4. **Minimal Runtime Image:** The production image contains only the compiled Go binary, CA certificates, and standard non-privileged user definitions. Shells, package managers, and development utilities are excluded.
5. **TLS Encryption:** Inbound client connections are terminated over HTTPS using Caddy. Traffic between Caddy and the Go API runs over an internal Docker bridge network.

---

## 6. Key Decisions and Trade-Offs

| Decision | Alternative Considered | Rationale and Trade-off |
|---|---|---|
| Go standard library `net/http` | Gin, Echo, Fiber | Eliminates third-party router vulnerabilities; standard library method routing (`GET /v1/shows/{id}`) provides readable, zero-dependency routing. |
| PostgreSQL Partial Unique Index | Redis Distributed Locks | Eliminates cache-database split-brain risk. ACID transactions and indexes guarantee absolute consistency without an extra data store. |
| Database Clock (`now()`) | Application System Clock | Application servers may experience clock skew. The database clock provides a single monotonic source of truth for hold expiration. |
| Additive Migrations Only | Up/Down Rollback Migrations | Down-migrations risk accidental data loss during production rollbacks. Additive migrations allow code rollbacks without schema rollbacks. |
| Caddy Reverse Proxy | Nginx / Envoy | Built-in automatic TLS certificate management, simpler declarative configuration, and native health checking. |
