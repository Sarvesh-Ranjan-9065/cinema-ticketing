-- 000001_init.down.sql
-- Revert core cinema schema and constraints.

DROP INDEX IF EXISTS idx_reservations_cleanup;
DROP INDEX IF EXISTS idx_reservations_show_seat;
DROP INDEX IF EXISTS idx_seats_show_id;
DROP INDEX IF EXISTS idx_shows_starts_at;
DROP INDEX IF EXISTS idx_reservations_ticket_id;
DROP INDEX IF EXISTS idx_reservations_active_seat;

DROP TABLE IF EXISTS reservations;
DROP TABLE IF EXISTS seats;
DROP TABLE IF EXISTS shows;
