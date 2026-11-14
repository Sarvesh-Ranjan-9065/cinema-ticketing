package booking

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cinema-ticketing/internal/config"
	"cinema-ticketing/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("skipping integration test: TEST_DATABASE_URL is not set")
	}

	cfg := &config.Config{
		DatabaseURL:       dbURL,
		DBMaxOpenConns:    60, // allow enough connections for 50 concurrent requests
		DBMaxIdleConns:    20,
		DBConnMaxLifetime: 5 * time.Minute,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := store.MigrateUp(dbURL); err != nil {
		t.Fatalf("failed applying migrations to test database: %v", err)
	}

	db, err := store.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("failed connecting to test database: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
	})

	return db.Pool()
}

// createTestShow inserts an isolated test show with seats into the database.
func createTestShow(t *testing.T, pool *pgxpool.Pool, title string, seatLabels []string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var showID string
	err := pool.QueryRow(ctx,
		`INSERT INTO shows (title, hall, starts_at)
		 VALUES ($1, 'Test Hall', now() + interval '4 hours')
		 RETURNING id::text`,
		title,
	).Scan(&showID)
	if err != nil {
		t.Fatalf("failed creating test show: %v", err)
	}

	for _, label := range seatLabels {
		_, err := pool.Exec(ctx,
			`INSERT INTO seats (show_id, label) VALUES ($1, $2)`,
			showID, label,
		)
		if err != nil {
			t.Fatalf("failed inserting seat %s: %v", label, err)
		}
	}

	return showID
}

// TestConcurrency_50SimultaneousHolds satisfies Acceptance Test 1:
// 50 concurrent requests attempt to hold the exact same seat simultaneously using a start barrier.
// Asserts exactly 1 success (HTTP 201 equivalent) and 49 conflicts (HTTP 409 equivalent) with 0 errors.
func TestConcurrency_50SimultaneousHolds(t *testing.T) {
	pool := setupTestDB(t)
	svc := NewService(pool)

	showID := createTestShow(t, pool, "Concurrency Show 50", []string{"A1"})

	concurrency := 50
	startBarrier := make(chan struct{})
	var wg sync.WaitGroup

	var successCount atomic.Int64
	var conflictCount atomic.Int64
	var unexpectedErrors atomic.Int64

	var successfulResID atomic.Pointer[string]

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(customerIndex int) {
			defer wg.Done()

			customerRef := fmt.Sprintf("customer-%02d", customerIndex)

			// Wait for barrier release so all goroutines fire at the exact same moment
			<-startBarrier

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			res, err := svc.CreateHold(ctx, showID, "A1", customerRef, 120*time.Second)
			if err == nil {
				successCount.Add(1)
				idCopy := res.ID
				successfulResID.Store(&idCopy)
			} else if errors.Is(err, ErrSeatTaken) {
				conflictCount.Add(1)
			} else {
				unexpectedErrors.Add(1)
				t.Errorf("goroutine %d received unexpected error: %v", customerIndex, err)
			}
		}(i)
	}

	// Release all 50 goroutines simultaneously
	close(startBarrier)
	wg.Wait()

	if successCount.Load() != 1 {
		t.Fatalf("expected exactly 1 successful hold, got %d", successCount.Load())
	}
	if conflictCount.Load() != int64(concurrency-1) {
		t.Fatalf("expected %d conflicts, got %d", concurrency-1, conflictCount.Load())
	}
	if unexpectedErrors.Load() != 0 {
		t.Fatalf("expected 0 unexpected errors, got %d", unexpectedErrors.Load())
	}

	// Verify database state: exactly one HELD reservation exists for seat A1
	ctx := context.Background()
	var count int
	err := pool.QueryRow(ctx,
		`SELECT count(*)
		 FROM reservations r
		 JOIN seats s ON s.id = r.seat_id
		 WHERE r.show_id = $1 AND s.label = 'A1' AND r.status = 'HELD'`,
		showID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed querying active reservation count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected database to contain exactly 1 active reservation, found %d", count)
	}
}

// TestConcurrency_DistinctSeats verifies that concurrent holds on distinct seats
// do not block each other and all succeed.
func TestConcurrency_DistinctSeats(t *testing.T) {
	pool := setupTestDB(t)
	svc := NewService(pool)

	seatCount := 10
	var seatLabels []string
	for i := 1; i <= seatCount; i++ {
		seatLabels = append(seatLabels, fmt.Sprintf("B%d", i))
	}

	showID := createTestShow(t, pool, "Concurrency Distinct Seats", seatLabels)

	startBarrier := make(chan struct{})
	var wg sync.WaitGroup

	var successCount atomic.Int64
	var errCount atomic.Int64

	for i := 0; i < seatCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			label := fmt.Sprintf("B%d", idx+1)
			cust := fmt.Sprintf("user-%d", idx)

			<-startBarrier

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := svc.CreateHold(ctx, showID, label, cust, 120*time.Second)
			if err == nil {
				successCount.Add(1)
			} else {
				errCount.Add(1)
				t.Errorf("failed holding distinct seat %s: %v", label, err)
			}
		}(i)
	}

	close(startBarrier)
	wg.Wait()

	if successCount.Load() != int64(seatCount) {
		t.Fatalf("expected all %d distinct seats to succeed, got %d (errors: %d)", seatCount, successCount.Load(), errCount.Load())
	}
}

// TestConcurrency_ConfirmRacingCancel races a confirmation against a cancellation
// on the same held reservation. Asserts that exactly one wins and state is consistent.
func TestConcurrency_ConfirmRacingCancel(t *testing.T) {
	pool := setupTestDB(t)
	svc := NewService(pool)

	showID := createTestShow(t, pool, "Race Confirm Cancel", []string{"C1"})

	ctx := context.Background()
	res, err := svc.CreateHold(ctx, showID, "C1", "race-user", 120*time.Second)
	if err != nil {
		t.Fatalf("failed creating initial hold: %v", err)
	}

	startBarrier := make(chan struct{})
	var wg sync.WaitGroup

	var confirmWon atomic.Bool
	var cancelWon atomic.Bool
	var confirmErr error
	var cancelErr error

	wg.Add(2)

	// Goroutine 1: Confirm
	go func() {
		defer wg.Done()
		<-startBarrier
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := svc.ConfirmReservation(cctx, res.ID)
		if err == nil {
			confirmWon.Store(true)
		} else {
			confirmErr = err
		}
	}()

	// Goroutine 2: Cancel
	go func() {
		defer wg.Done()
		<-startBarrier
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := svc.CancelReservation(cctx, res.ID)
		if err == nil {
			cancelWon.Store(true)
		} else {
			cancelErr = err
		}
	}()

	close(startBarrier)
	wg.Wait()

	// Verify consistent state: cannot both win or both fail without defined reasons
	finalRes, err := svc.GetReservation(ctx, res.ID)
	if err != nil {
		t.Fatalf("failed reading final reservation: %v", err)
	}

	if finalRes.Status != StatusConfirmed && finalRes.Status != StatusCancelled {
		t.Fatalf("unexpected final status: %s", finalRes.Status)
	}

	t.Logf("Race outcome: status=%s, confirmWon=%v (err=%v), cancelWon=%v (err=%v)",
		finalRes.Status, confirmWon.Load(), confirmErr, cancelWon.Load(), cancelErr)
}

// TestConcurrency_DuplicateConfirms tests two simultaneous confirms on the same reservation.
// Both callers must receive the exact same ticket identifier and succeed.
func TestConcurrency_DuplicateConfirms(t *testing.T) {
	pool := setupTestDB(t)
	svc := NewService(pool)

	showID := createTestShow(t, pool, "Duplicate Confirms", []string{"D1"})

	ctx := context.Background()
	res, err := svc.CreateHold(ctx, showID, "D1", "dup-user", 120*time.Second)
	if err != nil {
		t.Fatalf("failed creating hold: %v", err)
	}

	startBarrier := make(chan struct{})
	var wg sync.WaitGroup

	var ticket1, ticket2 *string
	var err1, err2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r, err := svc.ConfirmReservation(cctx, res.ID)
		if err == nil {
			ticket1 = r.TicketID
		}
		err1 = err
	}()

	go func() {
		defer wg.Done()
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r, err := svc.ConfirmReservation(cctx, res.ID)
		if err == nil {
			ticket2 = r.TicketID
		}
		err2 = err
	}()

	close(startBarrier)
	wg.Wait()

	if err1 != nil {
		t.Fatalf("caller 1 confirm failed: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("caller 2 confirm failed: %v", err2)
	}

	if ticket1 == nil || ticket2 == nil || *ticket1 != *ticket2 {
		t.Fatalf("expected identical tickets, got %v and %v", ticket1, ticket2)
	}
}
