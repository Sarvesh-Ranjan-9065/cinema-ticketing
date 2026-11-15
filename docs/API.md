# Cinema Reservation API Specification

This document details the HTTP REST API for the cinema show and seat reservation platform.

## Base URL and Network Access

- Public HTTPS endpoint (proxied through Caddy with local TLS): `https://localhost:443` (or `https://localhost`)
- Direct container port (internal Docker network only): `http://api:8080`

All endpoints return JSON responses (`Content-Type: application/json; charset=utf-8`).

Every response includes an `X-Request-ID` header. If a client supplies an `X-Request-ID` header on the incoming request, the server echoes it; otherwise, the server generates a unique UUID.

---

## Health and Readiness Endpoints

### Liveness Probe

Probes whether the HTTP server process is running. This probe does not query external dependencies.

- **URL:** `GET /healthz`
- **Success Status:** `200 OK`

**Response:**
```json
{
  "status": "ok"
}
```

---

### Readiness Probe

Probes whether the service is ready to accept user traffic by checking connectivity to PostgreSQL.

- **URL:** `GET /readyz`
- **Success Status:** `200 OK`
- **Failure Status:** `503 Service Unavailable`

**Success Response:**
```json
{
  "status": "ok"
}
```

**Failure Response:**
```json
{
  "code": "DEPENDENCY_UNAVAILABLE",
  "message": "database connection check failed",
  "request_id": "62b7194f-4d32-4752-9213-9426f86da12c"
}
```

---

## Shows and Seat Map Endpoints

### List Upcoming Shows

Returns a list of shows scheduled in the future relative to the database clock (`starts_at > now()`).

- **URL:** `GET /v1/shows`
- **Success Status:** `200 OK`

**Response:**
```json
[
  {
    "id": "359d7e0a-f860-4649-bab4-754c52911bdd",
    "title": "Interstellar",
    "hall": "Hall 1 - IMAX",
    "starts_at": "2026-11-15T18:00:00Z",
    "created_at": "2026-11-15T16:00:00Z"
  },
  {
    "id": "7820f4b3-c151-4091-a1b4-2b622c4f6990",
    "title": "Oppenheimer",
    "hall": "Hall 1 - IMAX",
    "starts_at": "2026-11-15T22:00:00Z",
    "created_at": "2026-11-15T16:00:00Z"
  },
  {
    "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "title": "Dune: Part Two",
    "hall": "Hall 2 - Dolby Cinema",
    "starts_at": "2026-11-16T16:00:00Z",
    "created_at": "2026-11-15T16:00:00Z"
  }
]
```

---

### Get Show Details

Returns details for a specific show by its UUID.

- **URL:** `GET /v1/shows/{id}`
- **Path Parameters:**
  - `id` (UUID, required): Show identifier.
- **Success Status:** `200 OK`
- **Error Status:** `400 Bad Request` (invalid UUID format), `404 Not Found` (unknown show).

**Response:**
```json
{
  "id": "359d7e0a-f860-4649-bab4-754c52911bdd",
  "title": "Interstellar",
  "hall": "Hall 1 - IMAX",
  "starts_at": "2026-11-15T18:00:00Z",
  "created_at": "2026-11-15T16:00:00Z"
}
```

---

### Get Show Seat Map

Returns the current status of all seats for a specific show.

- **URL:** `GET /v1/shows/{id}/seats`
- **Path Parameters:**
  - `id` (UUID, required): Show identifier.
- **Success Status:** `200 OK`
- **Error Status:** `400 Bad Request` (invalid UUID format), `404 Not Found` (unknown show).

**Seat Status Values:**
- `AVAILABLE`: Seat has no active hold or confirmed ticket.
- `HELD`: Seat is currently reserved under an active temporary hold (expires within configured TTL).
- `CONFIRMED`: Seat is booked with a confirmed ticket.

**Response:**
```json
[
  {
    "id": "e4f0a28b-1234-5678-90ab-cdef12345678",
    "label": "A1",
    "status": "AVAILABLE"
  },
  {
    "id": "d1c3b4a5-2345-6789-01bc-def123456789",
    "label": "A2",
    "status": "HELD"
  },
  {
    "id": "f7e8d9c0-3456-7890-12cd-ef1234567890",
    "label": "A3",
    "status": "CONFIRMED"
  }
]
```

---

## Reservation Endpoints

### Create Reservation Hold

Places a temporary hold on a single seat for a specified show. The hold remains active for the configured duration (default: 120 seconds) calculated strictly against the database clock (`now() + interval`).

- **URL:** `POST /v1/reservations`
- **Headers:**
  - `Content-Type: application/json` (required)
- **Request Body Limits:** Maximum 64 KB. Unknown JSON fields are disallowed.
- **Success Status:** `201 Created`
- **Error Status:**
  - `400 Bad Request`: Invalid UUID, invalid seat label pattern (e.g. `A1` through `E5`), missing customer reference, or malformed JSON.
  - `404 Not Found`: Show or seat does not exist.
  - `409 Conflict`: Seat is currently held or confirmed (`SEAT_TAKEN`), or show already started (`SHOW_ALREADY_STARTED`).
  - `413 Payload Too Large`: Body exceeds 64 KB (`PAYLOAD_TOO_LARGE`).
  - `415 Unsupported Media Type`: Missing or non-JSON Content-Type (`UNSUPPORTED_MEDIA_TYPE`).

**Request Body:**
```json
{
  "show_id": "359d7e0a-f860-4649-bab4-754c52911bdd",
  "seat_label": "C4",
  "customer_ref": "user_demo_101"
}
```

**Response:**
```json
{
  "id": "09ada17c-ebba-4038-9669-6741b4dc7195",
  "show_id": "359d7e0a-f860-4649-bab4-754c52911bdd",
  "seat_id": "8fa8d10b-1f81-42cb-b70d-fdf33e3dcf8c",
  "seat_label": "C4",
  "customer_ref": "user_demo_101",
  "status": "HELD",
  "hold_expires_at": "2026-11-15T16:02:00Z",
  "created_at": "2026-11-15T16:00:00Z",
  "updated_at": "2026-11-15T16:00:00Z"
}
```

---

### Get Reservation Details

Retrieves details for a specific reservation. Hold expiration is evaluated dynamically against the current database clock (`now()`).

- **URL:** `GET /v1/reservations/{id}`
- **Path Parameters:**
  - `id` (UUID, required): Reservation identifier.
- **Success Status:** `200 OK`
- **Error Status:** `400 Bad Request` (invalid UUID format), `404 Not Found` (reservation does not exist).

**Response:**
```json
{
  "id": "09ada17c-ebba-4038-9669-6741b4dc7195",
  "show_id": "359d7e0a-f860-4649-bab4-754c52911bdd",
  "seat_id": "8fa8d10b-1f81-42cb-b70d-fdf33e3dcf8c",
  "seat_label": "C4",
  "customer_ref": "user_demo_101",
  "status": "HELD",
  "hold_expires_at": "2026-11-15T16:02:00Z",
  "created_at": "2026-11-15T16:00:00Z",
  "updated_at": "2026-11-15T16:00:00Z"
}
```

---

### Confirm Reservation

Confirms an active reservation hold into a finalized booking and generates a cryptographically random ticket identifier (`TKT-<16 lowercase hex>`).

- **URL:** `POST /v1/reservations/{id}/confirm`
- **Path Parameters:**
  - `id` (UUID, required): Reservation identifier.
- **Success Status:** `200 OK`
- **Error Status:**
  - `400 Bad Request`: Invalid UUID format.
  - `404 Not Found`: Reservation identifier does not exist.
  - `409 Conflict`: Hold has expired (`HOLD_EXPIRED`), or reservation was already cancelled (`INVALID_STATE_TRANSITION`).

**Idempotency Behavior:**
- Confirming an already confirmed reservation returns the existing ticket identifier and `200 OK` without error.

**Response:**
```json
{
  "id": "09ada17c-ebba-4038-9669-6741b4dc7195",
  "show_id": "359d7e0a-f860-4649-bab4-754c52911bdd",
  "seat_id": "8fa8d10b-1f81-42cb-b70d-fdf33e3dcf8c",
  "seat_label": "C4",
  "customer_ref": "user_demo_101",
  "status": "CONFIRMED",
  "ticket_id": "TKT-6e53368e84c01cce",
  "hold_expires_at": "2026-11-15T16:02:00Z",
  "created_at": "2026-11-15T16:00:00Z",
  "updated_at": "2026-11-15T16:00:45Z",
  "confirmed_at": "2026-11-15T16:00:45Z"
}
```

---

### Cancel Reservation

Cancels an active hold or confirmed reservation, immediately freeing the seat for other users.

- **URL:** `POST /v1/reservations/{id}/cancel`
- **Path Parameters:**
  - `id` (UUID, required): Reservation identifier.
- **Success Status:** `200 OK`
- **Error Status:**
  - `400 Bad Request`: Invalid UUID format.
  - `404 Not Found`: Reservation identifier does not exist.
  - `409 Conflict`: Cannot cancel an expired reservation (`INVALID_STATE_TRANSITION`).

**Idempotency Behavior:**
- Calling cancel on an already cancelled reservation returns `200 OK` without altering state.
- Ticket identifiers are never reused following cancellation.

**Response:**
```json
{
  "id": "09ada17c-ebba-4038-9669-6741b4dc7195",
  "show_id": "359d7e0a-f860-4649-bab4-754c52911bdd",
  "seat_id": "8fa8d10b-1f81-42cb-b70d-fdf33e3dcf8c",
  "seat_label": "C4",
  "customer_ref": "user_demo_101",
  "status": "CANCELLED",
  "hold_expires_at": "2026-11-15T16:02:00Z",
  "created_at": "2026-11-15T16:00:00Z",
  "updated_at": "2026-11-15T16:01:10Z",
  "cancelled_at": "2026-11-15T16:01:10Z"
}
```

---

## Standard Error Response Format

All error responses return a standardized JSON object containing a machine-readable code, a human-readable explanation, and the associated request identifier.

```json
{
  "code": "SEAT_TAKEN",
  "message": "seat is already reserved or held",
  "request_id": "8fa8d10b-1f81-42cb-b70d-fdf33e3dcf8c"
}
```

### Error Code Catalog

| HTTP Status | Code | Meaning |
|---|---|---|
| `400 Bad Request` | `INVALID_INPUT` | Malformed JSON body, invalid UUID, invalid seat label pattern, or missing customer reference. |
| `404 Not Found` | `NOT_FOUND` | The requested show, seat, reservation, or URL route does not exist. |
| `409 Conflict` | `SEAT_TAKEN` | Another customer holds or has confirmed the seat. |
| `409 Conflict` | `HOLD_EXPIRED` | The hold period has expired; seat cannot be confirmed. |
| `409 Conflict` | `INVALID_STATE_TRANSITION` | Disallowed state transition (e.g. confirming a cancelled reservation, or cancelling an expired hold). |
| `409 Conflict` | `SHOW_ALREADY_STARTED` | The requested show has already started based on database time. |
| `413 Payload Too Large` | `PAYLOAD_TOO_LARGE` | Request payload exceeds maximum allowed size (64 KB). |
| `415 Unsupported Media Type` | `UNSUPPORTED_MEDIA_TYPE` | Content-Type header is not `application/json`. |
| `500 Internal Server Error` | `INTERNAL_ERROR` | Unexpected internal server error. Diagnostic details are logged internally. |
| `503 Service Unavailable` | `DEPENDENCY_UNAVAILABLE` | Database connection unreachable, timed out, or connection pool saturated. |
