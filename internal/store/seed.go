package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// SeedShow defines show parameters with start time relative to database clock.
type SeedShow struct {
	Title         string
	Hall          string
	OffsetFromNow time.Duration
}

// DefaultSeedShows defines standard demonstration shows scheduled in the future.
var DefaultSeedShows = []SeedShow{
	{Title: "Interstellar", Hall: "Hall 1 - IMAX", OffsetFromNow: 2 * time.Hour},
	{Title: "Oppenheimer", Hall: "Hall 1 - IMAX", OffsetFromNow: 6 * time.Hour},
	{Title: "Dune: Part Two", Hall: "Hall 2 - Dolby Cinema", OffsetFromNow: 24 * time.Hour},
}

// Seed inserts standard shows and 25-seat grids (A1-E5) idempotently into the database.
func (d *DB) Seed(ctx context.Context) error {
	if d == nil || d.pool == nil {
		return fmt.Errorf("database pool is not initialized")
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin seed transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	rows := []string{"A", "B", "C", "D", "E"}
	seatsPerRow := 5

	for _, s := range DefaultSeedShows {
		var showID string
		// Check if show with same title and hall already exists
		err := tx.QueryRow(ctx,
			`SELECT id::text FROM shows WHERE title = $1 AND hall = $2 LIMIT 1`,
			s.Title, s.Hall,
		).Scan(&showID)

		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("failed checking show existence: %w", err)
		}

		if errors.Is(err, pgx.ErrNoRows) {
			// Insert new show with future starts_at relative to database now()
			offsetSeconds := int(s.OffsetFromNow.Seconds())
			insertShowQuery := fmt.Sprintf(
				`INSERT INTO shows (title, hall, starts_at)
				 VALUES ($1, $2, now() + interval '%d seconds')
				 RETURNING id::text`,
				offsetSeconds,
			)

			if err := tx.QueryRow(ctx, insertShowQuery, s.Title, s.Hall).Scan(&showID); err != nil {
				return fmt.Errorf("failed to insert seed show %q: %w", s.Title, err)
			}
		}

		// Insert 25-seat grid (A1 through E5) idempotently
		for _, row := range rows {
			for num := 1; num <= seatsPerRow; num++ {
				seatLabel := fmt.Sprintf("%s%d", row, num)
				_, err := tx.Exec(ctx,
					`INSERT INTO seats (show_id, label)
					 VALUES ($1, $2)
					 ON CONFLICT (show_id, label) DO NOTHING`,
					showID, seatLabel,
				)
				if err != nil {
					return fmt.Errorf("failed inserting seat %s for show %s: %w", seatLabel, showID, err)
				}
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed committing seed transaction: %w", err)
	}

	return nil
}
