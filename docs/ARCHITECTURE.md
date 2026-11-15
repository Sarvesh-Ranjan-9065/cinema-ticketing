# Architecture and Design

This document describes the software architecture, data modeling, concurrency control, and security design of the cinema reservation system.

---

## 1. System Topology and Container Architecture

The system is deployed as three decoupled container services managed by Docker Compose across isolated internal bridge networks. Only the reverse proxy exposes network ports to the host system.

```mermaid
flowchart TD
    Client["Client / User Agent"]

    subgraph Host["Host Machine"]
        Port443["Host Port 443 (HTTPS) / Port 80 (HTTP)"]

        subgraph DockerCompose["Docker Compose Environment"]
            subgraph ProxyNetwork["proxy_net (Internal Bridge Network)"]
                Proxy["Proxy Container (Caddy)\n- TLS Termination (Internal CA)\n- HTTPS Reverse Proxy\n- Port 80 Redirect to HTTPS"]
            end

            subgraph DatabaseNetwork["db_net (Internal Bridge Network)"]
                API["API Container (Go cinema-api)\n- Subcommands: serve, healthcheck, migrate, seed\n- Standard Library net/http & log/slog\n- Non-root user (UID 65532)\n- Cap Drop ALL, no-new-privileges, read-only rootfs"]
                DB["Database Container (PostgreSQL 16)\n- Port 5432 (Internal Only)\n- ACID Transactions\n- Relational Constraints"]
            end

            subgraph Volumes["Named Docker Volumes"]
                VolDB[("db_data\nPersistent PostgreSQL Storage")]
                VolCaddyData[("caddy_data\nTLS Certificates & Internal CA")]
                VolCaddyCfg[("caddy_config\nCaddy Operational State")]
            end

            subgraph Secrets["Docker Secrets (Read-Only Mounts)"]
                SecDBUrl["/run/secrets/database_url"]
                SecDBPass["/run/secrets/db_password"]
            end
        end
    end

    Client -->|HTTPS :443| Port443
    Port443 --> Proxy
    Proxy -->|HTTP :8080| API
    API -->|TCP :5432| DB

    DB --- VolDB
    Proxy --- VolCaddyData
    Proxy --- VolCaddyCfg
    API -.-> SecDBUrl
    DB -.-> SecDBPass
```

### Network Isolation Rules
- **No published database port:** PostgreSQL listens on port 5432 exclusively inside `db_net`. Neither the host machine nor the reverse proxy can access the database directly.
- **Single ingress point:** Caddy is the only service that publishes host ports (`443` for HTTPS, `80` for redirect). Direct HTTP access to the Go API container is restricted to `proxy_net`.
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
    CIQualityGates --> Build["Multi-Stage Docker Build\n(golang:1.27-alpine -> distroless static:nonroot)"]
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
        text title
        text hall
        timestamptz starts_at
        timestamptz created_at
    }

    SEATS {
        uuid id PK
        uuid show_id FK
        text label
        timestamptz created_at
    }

    RESERVATIONS {
        uuid id PK
        uuid show_id FK
        uuid seat_id FK
        text customer_ref
        text status
        timestamptz hold_expires_at
        timestamptz confirmed_at
        timestamptz cancelled_at
        text ticket_id UK
        timestamptz created_at
        timestamptz updated_at
    }
```

### Relational Constraints
- `idx_reservations_active_seat`: Partial unique index on `(show_id, seat_id)` where `status IN ('HELD', 'CONFIRMED')`. Prevents concurrent holds or confirmations on the same seat.
- `seats_show_id_label_unique`: Unique constraint on `(show_id, label)` ensuring no duplicate seat labels exist within a single show.
- `check_status`: Check constraint enforcing `status IN ('HELD', 'CONFIRMED', 'CANCELLED', 'EXPIRED')`.
- `check_confirmed_ticket`: Check constraint ensuring that whenever `status = 'CONFIRMED'`, `ticket_id IS NOT NULL`.
- `idx_reservations_ticket_id`: Unique index ensuring global uniqueness of ticket identifiers (`WHERE ticket_id IS NOT NULL`).

---

## 4. Concurrency Control and Hold Lifecycle

The reservation lifecycle consists of four distinct states: `HELD`, `CONFIRMED`, `CANCELLED`, and `EXPIRED`.

```mermaid
stateDiagram-v2
    [*] --> AVAILABLE : Initial State
    AVAILABLE --> HELD : POST /v1/reservations\n(Hold duration: 120s)
    HELD --> CONFIRMED : POST /v1/reservations/{id}/confirm\n(within hold TTL)
    HELD --> CANCELLED : POST /v1/reservations/{id}/cancel\n(Immediate seat release)
    HELD --> EXPIRED : Expiry (Database now() > hold_expires_at)\nSeat dynamically AVAILABLE
    CONFIRMED --> CANCELLED : POST /v1/reservations/{id}/cancel\n(Immediate seat release)
    EXPIRED --> AVAILABLE : Stale hold lazily marked EXPIRED on next hold
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
    ClientA->>+API: POST /v1/reservations {show_id, seat_label: "C4", customer_ref}
    ClientB->>+API: POST /v1/reservations {show_id, seat_label: "C4", customer_ref}

    API->>DB: BEGIN Transaction
    API->>DB: Resolve seat_id and verify show starts_at > now()
    API->>DB: Lazily expire stale holds on seat (status='EXPIRED')
    API->>DB: INSERT INTO reservations (show_id, seat_id, customer_ref, status, hold_expires_at)\nVALUES (S1, SeatID, 'cust', 'HELD', now() + 120s)

    alt Client A inserts first
        DB-->>API: INSERT 1 (Success)
        API->>DB: COMMIT
        API-->>ClientA: 201 Created (Reservation record, hold_expires_at)
    else Client B attempts duplicate active hold
        DB-->>API: ERROR: duplicate key value violates unique constraint "idx_reservations_active_seat"
        API->>DB: ROLLBACK
        API-->>ClientB: 409 Conflict (SEAT_TAKEN)
    end
```

### Guarantees Enforced at Database Level
1. **Clock Source Independence:** Hold expiration is evaluated using `now()` on the PostgreSQL database server. Application server clock drift has zero effect on reservation validity.
2. **Zero Dependency on Background Workers:** An expired hold (`now() >= hold_expires_at`) is recognized dynamically during queries (`GetSeatMap` evaluates it as `AVAILABLE`) and updated lazily in `CreateHold`. The background cleanup worker runs solely to tidy stale rows.
3. **Cryptographic Ticket Uniqueness:** Confirmed reservations generate an unguessable 64-bit random ticket string formatted as `TKT-<16 lowercase hex chars>` derived from `crypto/rand`.
4. **Idempotency:**
   - Confirming an already confirmed reservation returns the existing ticket identifier with HTTP 200.
   - Cancelling an already cancelled reservation returns HTTP 200 without side effects.
   - Cancelling an expired reservation is rejected with HTTP 409 `INVALID_STATE_TRANSITION`.

---

## 5. Security Architecture and Hardening

The application runtime is hardened across container, operating system, and network boundaries:

1. **Non-Root Execution:** The application container runs under UID `65532:65532` (`nonroot` from distroless static). The container never acquires root privileges.
2. **Capability Dropping:** Docker Compose specifies `cap_drop: [ALL]` and `security_opt: [no-new-privileges:true]` on all application containers.
3. **Secret Files:** Sensitive credentials are supplied via Docker secrets or bind-mounted files (`DATABASE_URL_FILE`), read directly from disk into application memory without appearing in environment variables or container inspection output.
4. **Minimal Runtime Image:** The production image is built from `gcr.io/distroless/static:nonroot`, containing only the compiled Go binary, CA certificates, and standard non-privileged user definitions. Shells, package managers, and development utilities are excluded.
5. **TLS Encryption:** Inbound client connections are terminated over HTTPS using Caddy. Traffic between Caddy and the Go API runs over internal Docker bridge networks (`proxy_net`, `db_net`).

---

## 6. Key Decisions and Trade-Offs

| Decision | Alternative Considered | Rationale and Trade-off |
|---|---|---|
| Go standard library `net/http` | Gin, Echo, Fiber | Eliminates third-party router vulnerabilities; standard library method routing provides readable, zero-dependency routing. |
| PostgreSQL Partial Unique Index | Redis Distributed Locks | Eliminates cache-database split-brain risk. ACID transactions and indexes guarantee absolute consistency without an extra data store. |
| Database Clock (`now()`) | Application System Clock | Application servers may experience clock skew. The database clock provides a single monotonic source of truth for hold expiration. |
| Versioned SQL Migrations (`golang-migrate`) | Manual SQL Scripts or ORM Auto-sync | Versioned `.up.sql` and `.down.sql` migrations provide deterministic schema evolution with repeatable deployment and rollback verification. |
| Caddy Reverse Proxy | Nginx / Envoy | Built-in automatic TLS certificate management, simpler declarative configuration, and native health checking. |
