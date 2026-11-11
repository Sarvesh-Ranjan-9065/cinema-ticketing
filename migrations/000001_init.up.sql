-- 000001_init.up.sql
-- Core schema for cinema shows, seats, and reservation invariants.

CREATE TABLE IF NOT EXISTS shows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    hall TEXT NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS seats (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    show_id UUID NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT seats_show_id_label_unique UNIQUE (show_id, label)
);

CREATE TABLE IF NOT EXISTS reservations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    show_id UUID NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    seat_id UUID NOT NULL REFERENCES seats(id) ON DELETE CASCADE,
    customer_ref TEXT NOT NULL,
    status TEXT NOT NULL,
    hold_expires_at TIMESTAMPTZ NOT NULL,
    ticket_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    confirmed_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    CONSTRAINT check_status CHECK (status IN ('HELD', 'CONFIRMED', 'CANCELLED', 'EXPIRED')),
    CONSTRAINT check_confirmed_ticket CHECK (status != 'CONFIRMED' OR ticket_id IS NOT NULL)
);

-- Core Invariant 1: At most one active reservation (HELD or CONFIRMED) per seat per show.
CREATE UNIQUE INDEX IF NOT EXISTS idx_reservations_active_seat
    ON reservations (show_id, seat_id)
    WHERE status IN ('HELD', 'CONFIRMED');

-- Core Invariant 2: A confirmed reservation always has a globally unique ticket identifier.
CREATE UNIQUE INDEX IF NOT EXISTS idx_reservations_ticket_id
    ON reservations (ticket_id)
    WHERE ticket_id IS NOT NULL;

-- Supporting indexes for query performance
CREATE INDEX IF NOT EXISTS idx_shows_starts_at ON shows (starts_at);
CREATE INDEX IF NOT EXISTS idx_seats_show_id ON seats (show_id);
CREATE INDEX IF NOT EXISTS idx_reservations_show_seat ON reservations (show_id, seat_id);
CREATE INDEX IF NOT EXISTS idx_reservations_cleanup ON reservations (hold_expires_at) WHERE status = 'HELD';
