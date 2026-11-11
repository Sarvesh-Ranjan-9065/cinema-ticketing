package booking

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service provides booking operations and invariants enforcement.
type Service struct {
	pool *pgxpool.Pool
}

// NewService constructs a new booking Service with the provided database connection pool.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// ListShows retrieves upcoming shows ordered by screening start time.
func (s *Service) ListShows(ctx context.Context) ([]Show, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, title, hall, starts_at, created_at
		 FROM shows
		 ORDER BY starts_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed querying shows: %w", err)
	}
	defer rows.Close()

	var shows []Show
	for rows.Next() {
		var sh Show
		if err := rows.Scan(&sh.ID, &sh.Title, &sh.Hall, &sh.StartsAt, &sh.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed scanning show: %w", err)
		}
		shows = append(shows, sh)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during shows iteration: %w", err)
	}

	return shows, nil
}

// GetShow retrieves a single show by its unique identifier.
func (s *Service) GetShow(ctx context.Context, id string) (*Show, error) {
	var sh Show
	err := s.pool.QueryRow(ctx,
		`SELECT id, title, hall, starts_at, created_at
		 FROM shows
		 WHERE id = $1`,
		id,
	).Scan(&sh.ID, &sh.Title, &sh.Hall, &sh.StartsAt, &sh.CreatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed querying show %s: %w", id, err)
	}

	return &sh, nil
}

// GetSeatMap retrieves all seats for a show along with their real-time status.
// Holds that have passed hold_expires_at are dynamically evaluated as AVAILABLE using now().
func (s *Service) GetSeatMap(ctx context.Context, showID string) ([]SeatWithStatus, error) {
	// Verify show exists first
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shows WHERE id = $1)`, showID).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("failed checking show existence: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}

	rows, err := s.pool.Query(ctx,
		`SELECT s.id, s.label,
		        COALESCE(
		            (SELECT CASE
		                WHEN r.status = 'CONFIRMED' THEN 'CONFIRMED'
		                WHEN r.status = 'HELD' AND r.hold_expires_at > now() THEN 'HELD'
		                ELSE 'AVAILABLE'
		             END
		             FROM reservations r
		             WHERE r.show_id = s.show_id AND r.seat_id = s.id AND r.status IN ('HELD', 'CONFIRMED')
		             LIMIT 1),
		            'AVAILABLE'
		        ) AS status
		 FROM seats s
		 WHERE s.show_id = $1
		 ORDER BY s.label ASC`,
		showID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed querying seat map for show %s: %w", showID, err)
	}
	defer rows.Close()

	var seats []SeatWithStatus
	for rows.Next() {
		var seat SeatWithStatus
		if err := rows.Scan(&seat.ID, &seat.Label, &seat.Status); err != nil {
			return nil, fmt.Errorf("failed scanning seat status: %w", err)
		}
		seats = append(seats, seat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during seat map iteration: %w", err)
	}

	return seats, nil
}

// CreateHold creates a temporary hold on a specific seat for a show.
// If an existing hold on that seat has expired, it is cleared lazily within the same transaction.
func (s *Service) CreateHold(ctx context.Context, showID, seatLabel, customerRef string, ttl time.Duration) (*Reservation, error) {
	normalizedLabel := strings.ToUpper(strings.TrimSpace(seatLabel))

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed starting hold transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// 1. Verify show exists and has not started yet
	var startsAt time.Time
	err = tx.QueryRow(ctx,
		`SELECT starts_at FROM shows WHERE id = $1`,
		showID,
	).Scan(&startsAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed querying show starts_at: %w", err)
	}

	var hasStarted bool
	err = tx.QueryRow(ctx, `SELECT $1::timestamptz <= now()`, startsAt).Scan(&hasStarted)
	if err != nil {
		return nil, fmt.Errorf("failed comparing show start time: %w", err)
	}
	if hasStarted {
		return nil, ErrShowStarted
	}

	// 2. Resolve seat ID by label
	var seatID string
	err = tx.QueryRow(ctx,
		`SELECT id FROM seats WHERE show_id = $1 AND label = $2`,
		showID, normalizedLabel,
	).Scan(&seatID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed resolving seat %s: %w", normalizedLabel, err)
	}

	// 3. Lazily expire any stale hold on this seat
	_, err = tx.Exec(ctx,
		`UPDATE reservations
		 SET status = 'EXPIRED', updated_at = now()
		 WHERE show_id = $1 AND seat_id = $2 AND status = 'HELD' AND hold_expires_at <= now()`,
		showID, seatID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed expiring stale holds: %w", err)
	}

	// 4. Insert new HELD reservation
	ttlSeconds := int(ttl.Seconds())
	insertQuery := fmt.Sprintf(
		`INSERT INTO reservations (show_id, seat_id, customer_ref, status, hold_expires_at)
		 VALUES ($1, $2, $3, 'HELD', now() + interval '%d seconds')
		 RETURNING id, status, hold_expires_at, created_at, updated_at`,
		ttlSeconds,
	)

	var res Reservation
	res.ShowID = showID
	res.SeatID = seatID
	res.SeatLabel = normalizedLabel
	res.CustomerRef = customerRef

	err = tx.QueryRow(ctx, insertQuery, showID, seatID, customerRef).Scan(
		&res.ID,
		&res.Status,
		&res.HoldExpiresAt,
		&res.CreatedAt,
		&res.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrSeatTaken
		}
		return nil, fmt.Errorf("failed inserting reservation hold: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed committing hold transaction: %w", err)
	}

	return &res, nil
}

// GetReservation retrieves a reservation, evaluating hold expiry dynamically against database now().
func (s *Service) GetReservation(ctx context.Context, id string) (*Reservation, error) {
	var res Reservation
	var isExpired bool

	err := s.pool.QueryRow(ctx,
		`SELECT r.id, r.show_id, r.seat_id, s.label, r.customer_ref, r.status,
		        r.hold_expires_at, r.ticket_id, r.created_at, r.updated_at,
		        r.confirmed_at, r.cancelled_at,
		        (r.hold_expires_at <= now()) AS is_expired
		 FROM reservations r
		 JOIN seats s ON s.id = r.seat_id
		 WHERE r.id = $1`,
		id,
	).Scan(
		&res.ID, &res.ShowID, &res.SeatID, &res.SeatLabel, &res.CustomerRef, &res.Status,
		&res.HoldExpiresAt, &res.TicketID, &res.CreatedAt, &res.UpdatedAt,
		&res.ConfirmedAt, &res.CancelledAt, &isExpired,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed querying reservation %s: %w", id, err)
	}

	if res.Status == StatusHeld && isExpired {
		res.Status = StatusExpired
	}

	return &res, nil
}

// ConfirmReservation confirms a held reservation, generating a unique ticket identifier.
// Operation is idempotent: repeating confirmation on an already confirmed reservation returns the existing ticket.
func (s *Service) ConfirmReservation(ctx context.Context, id string) (*Reservation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed starting confirm transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var res Reservation
	var isExpired bool

	err = tx.QueryRow(ctx,
		`SELECT r.id, r.show_id, r.seat_id, s.label, r.customer_ref, r.status,
		        r.hold_expires_at, r.ticket_id, r.created_at, r.updated_at,
		        r.confirmed_at, r.cancelled_at,
		        (r.hold_expires_at <= now()) AS is_expired
		 FROM reservations r
		 JOIN seats s ON s.id = r.seat_id
		 WHERE r.id = $1
		 FOR UPDATE OF r`,
		id,
	).Scan(
		&res.ID, &res.ShowID, &res.SeatID, &res.SeatLabel, &res.CustomerRef, &res.Status,
		&res.HoldExpiresAt, &res.TicketID, &res.CreatedAt, &res.UpdatedAt,
		&res.ConfirmedAt, &res.CancelledAt, &isExpired,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed querying reservation %s for update: %w", id, err)
	}

	// Idempotency: if already confirmed, return current confirmed reservation
	if res.Status == StatusConfirmed {
		return &res, nil
	}

	if res.Status == StatusCancelled {
		return nil, fmt.Errorf("%w: cannot confirm a cancelled reservation", ErrInvalidTransition)
	}

	if res.Status == StatusExpired || isExpired {
		_, _ = tx.Exec(ctx, `UPDATE reservations SET status = 'EXPIRED', updated_at = now() WHERE id = $1`, id)
		_ = tx.Commit(ctx)
		return nil, ErrHoldExpired
	}

	// Generate ticket identifier and confirm
	ticketID, err := GenerateTicketID()
	if err != nil {
		return nil, err
	}

	err = tx.QueryRow(ctx,
		`UPDATE reservations
		 SET status = 'CONFIRMED', ticket_id = $2, confirmed_at = now(), updated_at = now()
		 WHERE id = $1
		 RETURNING ticket_id, confirmed_at, updated_at`,
		id, ticketID,
	).Scan(&res.TicketID, &res.ConfirmedAt, &res.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed updating reservation confirmation: %w", err)
	}

	res.Status = StatusConfirmed

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed committing confirmation: %w", err)
	}

	return &res, nil
}

// CancelReservation marks a held or confirmed reservation as CANCELLED, releasing the seat.
// Operation is idempotent: cancelling an already cancelled reservation succeeds without alteration.
func (s *Service) CancelReservation(ctx context.Context, id string) (*Reservation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed starting cancel transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var res Reservation
	var isExpired bool

	err = tx.QueryRow(ctx,
		`SELECT r.id, r.show_id, r.seat_id, s.label, r.customer_ref, r.status,
		        r.hold_expires_at, r.ticket_id, r.created_at, r.updated_at,
		        r.confirmed_at, r.cancelled_at,
		        (r.hold_expires_at <= now()) AS is_expired
		 FROM reservations r
		 JOIN seats s ON s.id = r.seat_id
		 WHERE r.id = $1
		 FOR UPDATE OF r`,
		id,
	).Scan(
		&res.ID, &res.ShowID, &res.SeatID, &res.SeatLabel, &res.CustomerRef, &res.Status,
		&res.HoldExpiresAt, &res.TicketID, &res.CreatedAt, &res.UpdatedAt,
		&res.ConfirmedAt, &res.CancelledAt, &isExpired,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed querying reservation %s for cancel: %w", id, err)
	}

	// Idempotency: if already cancelled, return as-is
	if res.Status == StatusCancelled {
		return &res, nil
	}

	if res.Status == StatusExpired || (res.Status == StatusHeld && isExpired) {
		_, _ = tx.Exec(ctx, `UPDATE reservations SET status = 'EXPIRED', updated_at = now() WHERE id = $1`, id)
		_ = tx.Commit(ctx)
		return nil, fmt.Errorf("%w: cannot cancel an expired reservation", ErrInvalidTransition)
	}

	err = tx.QueryRow(ctx,
		`UPDATE reservations
		 SET status = 'CANCELLED', cancelled_at = now(), updated_at = now()
		 WHERE id = $1
		 RETURNING cancelled_at, updated_at`,
		id,
	).Scan(&res.CancelledAt, &res.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed updating reservation cancellation: %w", err)
	}

	res.Status = StatusCancelled

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed committing cancellation: %w", err)
	}

	return &res, nil
}

// CleanupExpiredHolds transitions holds whose expiry has elapsed to EXPIRED.
// Correctness does not depend on this background task; it runs periodically to tidy rows.
func (s *Service) CleanupExpiredHolds(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE reservations
		 SET status = 'EXPIRED', updated_at = now()
		 WHERE status = 'HELD' AND hold_expires_at <= now()`,
	)
	if err != nil {
		return 0, fmt.Errorf("failed cleaning up expired holds: %w", err)
	}

	return tag.RowsAffected(), nil
}
