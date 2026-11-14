package booking

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestIntegration_HoldConfirmCancelFlow exercises the full lifecycle against PostgreSQL.
func TestIntegration_HoldConfirmCancelFlow(t *testing.T) {
	pool := setupTestDB(t)
	svc := NewService(pool)

	showID := createTestShow(t, pool, "Integration Full Flow", []string{"E1", "E2"})

	ctx := context.Background()

	// 1. Initial Seat Map: Both seats AVAILABLE
	seats, err := svc.GetSeatMap(ctx, showID)
	if err != nil {
		t.Fatalf("failed fetching seat map: %v", err)
	}
	if len(seats) != 2 || seats[0].Status != SeatAvailable || seats[1].Status != SeatAvailable {
		t.Fatalf("expected all seats AVAILABLE, got: %+v", seats)
	}

	// 2. Hold Seat E1
	res, err := svc.CreateHold(ctx, showID, "E1", "test-customer", 120*time.Second)
	if err != nil {
		t.Fatalf("failed creating hold: %v", err)
	}
	if res.Status != StatusHeld {
		t.Fatalf("expected status HELD, got %s", res.Status)
	}

	// 3. Seat Map shows E1 HELD, E2 AVAILABLE
	seats, err = svc.GetSeatMap(ctx, showID)
	if err != nil {
		t.Fatalf("failed fetching seat map: %v", err)
	}
	for _, s := range seats {
		if s.Label == "E1" && s.Status != SeatHeld {
			t.Errorf("expected E1 to be HELD, got %s", s.Status)
		}
		if s.Label == "E2" && s.Status != SeatAvailable {
			t.Errorf("expected E2 to be AVAILABLE, got %s", s.Status)
		}
	}

	// 4. Confirm Reservation E1
	confirmedRes, err := svc.ConfirmReservation(ctx, res.ID)
	if err != nil {
		t.Fatalf("failed confirming reservation: %v", err)
	}
	if confirmedRes.Status != StatusConfirmed || confirmedRes.TicketID == nil {
		t.Fatalf("expected status CONFIRMED with ticket, got: %+v", confirmedRes)
	}

	// 5. Seat Map shows E1 CONFIRMED
	seats, err = svc.GetSeatMap(ctx, showID)
	if err != nil {
		t.Fatalf("failed fetching seat map: %v", err)
	}
	for _, s := range seats {
		if s.Label == "E1" && s.Status != SeatConfirmed {
			t.Errorf("expected E1 to be CONFIRMED, got %s", s.Status)
		}
	}

	// 6. Cancel Reservation E1
	cancelledRes, err := svc.CancelReservation(ctx, res.ID)
	if err != nil {
		t.Fatalf("failed cancelling reservation: %v", err)
	}
	if cancelledRes.Status != StatusCancelled {
		t.Fatalf("expected status CANCELLED, got %s", cancelledRes.Status)
	}

	// 7. Seat Map immediately shows E1 AVAILABLE again
	seats, err = svc.GetSeatMap(ctx, showID)
	if err != nil {
		t.Fatalf("failed fetching seat map: %v", err)
	}
	for _, s := range seats {
		if s.Label == "E1" && s.Status != SeatAvailable {
			t.Errorf("expected E1 to be AVAILABLE after cancel, got %s", s.Status)
		}
	}

	// 8. New customer can now hold E1
	newRes, err := svc.CreateHold(ctx, showID, "E1", "new-customer", 120*time.Second)
	if err != nil {
		t.Fatalf("expected new hold on released seat to succeed, got: %v", err)
	}
	if newRes.Status != StatusHeld {
		t.Fatalf("expected new hold status HELD, got %s", newRes.Status)
	}
}

// TestIntegration_ExpiredHoldBehavior verifies that an expired hold frees the seat dynamically,
// rejects confirmation attempts, and allows new holds to overwrite it without running cleanup.
func TestIntegration_ExpiredHoldBehavior(t *testing.T) {
	pool := setupTestDB(t)
	svc := NewService(pool)

	showID := createTestShow(t, pool, "Integration Expired Hold", []string{"F1"})

	ctx := context.Background()

	// Create hold with very short TTL
	res, err := svc.CreateHold(ctx, showID, "F1", "expiring-cust", 10*time.Second)
	if err != nil {
		t.Fatalf("failed creating hold: %v", err)
	}

	// Deliberately set hold_expires_at in the past to simulate time expiry without sleeping
	_, err = pool.Exec(ctx,
		`UPDATE reservations SET hold_expires_at = now() - interval '5 seconds' WHERE id = $1`,
		res.ID,
	)
	if err != nil {
		t.Fatalf("failed expiring hold in database: %v", err)
	}

	// 1. Seat Map should immediately evaluate expired hold as AVAILABLE
	seats, err := svc.GetSeatMap(ctx, showID)
	if err != nil {
		t.Fatalf("failed fetching seat map: %v", err)
	}
	if len(seats) != 1 || seats[0].Status != SeatAvailable {
		t.Fatalf("expected seat F1 to be AVAILABLE after expiry, got %s", seats[0].Status)
	}

	// 2. Original holder attempting to confirm must be rejected with ErrHoldExpired
	_, err = svc.ConfirmReservation(ctx, res.ID)
	if !errors.Is(err, ErrHoldExpired) {
		t.Fatalf("expected ErrHoldExpired, got: %v", err)
	}

	// 3. New customer can immediately hold seat F1 (lazy expiry takes care of the stale hold)
	newRes, err := svc.CreateHold(ctx, showID, "F1", "second-cust", 120*time.Second)
	if err != nil {
		t.Fatalf("expected new hold to succeed over expired hold, got: %v", err)
	}
	if newRes.Status != StatusHeld {
		t.Fatalf("expected status HELD, got %s", newRes.Status)
	}
}

// TestIntegration_ShowAlreadyStarted verifies that holds cannot be placed on past shows.
func TestIntegration_ShowAlreadyStarted(t *testing.T) {
	pool := setupTestDB(t)
	svc := NewService(pool)

	ctx := context.Background()

	// Insert show in the past
	var pastShowID string
	err := pool.QueryRow(ctx,
		`INSERT INTO shows (title, hall, starts_at)
		 VALUES ('Past Movie', 'Hall 1', now() - interval '1 hour')
		 RETURNING id::text`,
	).Scan(&pastShowID)
	if err != nil {
		t.Fatalf("failed creating past show: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO seats (show_id, label) VALUES ($1, 'A1')`, pastShowID)
	if err != nil {
		t.Fatalf("failed inserting seat: %v", err)
	}

	_, err = svc.CreateHold(ctx, pastShowID, "A1", "customer", 120*time.Second)
	if !errors.Is(err, ErrShowStarted) {
		t.Fatalf("expected ErrShowStarted, got: %v", err)
	}
}
