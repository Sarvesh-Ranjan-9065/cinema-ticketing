# Cinema Reservation API Specification

This document details the HTTP REST API for the cinema show and seat reservation platform.

## Base URL and Network Access

- Public HTTPS endpoint (proxied through Caddy with local TLS): `https://localhost:8443`
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
{
  "shows": [
    {
      "id": "359d7e0a-f860-4649-bab4-754c52911bdd",
      "title": "Interstellar",
      "starts_at": "2026-11-15T18:00:00Z",
      "total_seats": 50,
      "available_seats": 48
    },
    {
      "id": "7820f4b3-c151-4091-a1b4-2b622c4f6990",
      "title": "Inception",
      "starts_at": "2026-11-15T21:00:00Z",
      "total_seats": 50,
      "available_seats": 50
    }
  ]
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
{
  "show_id": "359d7e0a-f860-4649-bab4-754c52911bdd",
  "seats": [
    {
      "seat_number": "A1",
      "status": "AVAILABLE"
    },
    {
      "seat_number": "A2",
      "status": "HELD"
    },
    {
      "seat_number": "A3",
      "status": "CONFIRMED"
    }
  ]
}
```

---

## Reservation Endpoints

### Create Reservation Hold

Places a temporary hold on a single seat for a specified show. The hold remains active for the configured duration (default: 120 seconds) calculated strictly against the database clock (`now() + interval`).

- **URL:** `POST /v1/shows/{id}/reservations`
- **Headers:**
  - `Content-Type: application/json` (required)
  - `Idempotency-Key: <string>` (optional, 1 to 64 ASCII characters)
- **Path Parameters:**
  - `id` (UUID, required): Show identifier.
- **Success Status:** `201 Created`
- **Error Status:**
  - `400 Bad Request`: Invalid seat label format, missing customer reference, or show already started.
  - `404 Not Found`: Show does not exist.
  - `409 Conflict`: Seat is currently held by another active hold or confirmed.

**Request Body:**
```json
{
  "seat_number": "C4",
  "customer_reference": "user_demo_101"
}
```

**Response:**
```json
{
  "id": "09ada17c-ebba-4038-9669-6741b4dc7195",
  "show_id": "359d7e0a-f860-4649-bab4-754c52911bdd",
  "seat_number": "C4",
  "status": "HELD",
  "customer_reference": "user_demo_101",
  "expires_at": "2026-11-14T12:02:00Z",
  "created_at": "2026-11-14T12:00:00Z"
}
```

---

### Confirm Reservation

Confirms an active reservation hold into a finalized booking and generates a cryptographically random ticket identifier.

- **URL:** `POST /v1/reservations/{id}/confirm`
- **Headers:**
  - `Content-Type: application/json` (optional)
  - `Idempotency-Key: <string>` (optional, 1 to 64 ASCII characters)
- **Path Parameters:**
  - `id` (UUID, required): Reservation identifier.
- **Success Status:** `200 OK`
- **Error Status:**
  - `404 Not Found`: Reservation identifier does not exist.
  - `409 Conflict`: Hold has expired (`HOLD_EXPIRED`) or was already cancelled (`RESERVATION_CANCELLED`).

**Idempotency Behavior:**
- Confirming an already confirmed reservation returns the existing ticket identifier and `200 OK` without error.

**Response:**
```json
{
  "id": "09ada17c-ebba-4038-9669-6741b4dc7195",
  "show_id": "359d7e0a-f860-4649-bab4-754c52911bdd",
  "seat_number": "C4",
  "status": "CONFIRMED",
  "customer_reference": "user_demo_101",
  "ticket_id": "TKT-6e53368e84c01cce",
  "created_at": "2026-11-14T12:00:00Z",
  "confirmed_at": "2026-11-14T12:00:45Z"
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
  - `404 Not Found`: Reservation identifier does not exist.

**Idempotency Behavior:**
- Calling cancel on an already cancelled reservation returns `200 OK` without altering state.
- Calling cancel on an expired reservation returns `200 OK` and marks the record `CANCELLED`.
- Ticket identifiers are never reused following cancellation.

**Response:**
```json
{
  "id": "09ada17c-ebba-4038-9669-6741b4dc7195",
  "status": "CANCELLED"
}
```

---

## Standard Error Response Format

All error responses return a standardized JSON object containing a machine-readable code, a human-readable explanation, and the associated request identifier.

```json
{
  "code": "SEAT_ALREADY_RESERVED",
  "message": "seat is already reserved or held",
  "request_id": "8fa8d10b-1f81-42cb-b70d-fdf33e3dcf8c"
}
```

### Error Code Catalog

| HTTP Status | Code | Meaning |
|---|---|---|
| `400 Bad Request` | `INVALID_REQUEST` | Malformed JSON body, missing required field, or invalid parameter type. |
| `400 Bad Request` | `INVALID_SEAT_NUMBER` | Seat label does not match standard grid format (e.g., `A1` through `E10`). |
| `400 Bad Request` | `SHOW_ALREADY_STARTED` | The requested show has already started based on database time. |
| `404 Not Found` | `NOT_FOUND` | The requested URL route does not exist. |
| `404 Not Found` | `SHOW_NOT_FOUND` | The specified show UUID does not exist. |
| `404 Not Found` | `RESERVATION_NOT_FOUND` | The specified reservation UUID does not exist. |
| `405 Method Not Allowed` | `METHOD_NOT_ALLOWED` | HTTP method is not permitted on the route. |
| `409 Conflict` | `SEAT_ALREADY_RESERVED` | Another customer holds or has confirmed the seat. |
| `409 Conflict` | `HOLD_EXPIRED` | The hold period has expired; seat cannot be confirmed. |
| `409 Conflict` | `RESERVATION_CANCELLED` | The reservation was previously cancelled. |
| `413 Payload Too Large` | `BODY_TOO_LARGE` | Request payload exceeds maximum allowed size (1 MB). |
| `415 Unsupported Media Type` | `UNSUPPORTED_MEDIA_TYPE` | Content-Type header is not `application/json`. |
| `500 Internal Server Error` | `INTERNAL_ERROR` | Unexpected internal server error. Diagnostic details are logged internally. |
| `503 Service Unavailable` | `DEPENDENCY_UNAVAILABLE` | Database connection unreachable or timed out. |
