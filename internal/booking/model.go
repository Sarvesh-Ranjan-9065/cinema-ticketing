// Package booking provides domain models, business logic, and repository operations
// for cinema shows, seats, and safe seat reservations.
package booking

import (
	"time"
)

// ReservationStatus represents the current state of a reservation.
type ReservationStatus string

const (
	StatusHeld      ReservationStatus = "HELD"
	StatusConfirmed ReservationStatus = "CONFIRMED"
	StatusCancelled ReservationStatus = "CANCELLED"
	StatusExpired   ReservationStatus = "EXPIRED"
)

// SeatStatus represents the availability of a seat for a show.
type SeatStatus string

const (
	SeatAvailable SeatStatus = "AVAILABLE"
	SeatHeld      SeatStatus = "HELD"
	SeatConfirmed SeatStatus = "CONFIRMED"
)

// Show represents a scheduled movie screening in a cinema hall.
type Show struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Hall      string    `json:"hall"`
	StartsAt  time.Time `json:"starts_at"`
	CreatedAt time.Time `json:"created_at"`
}

// Seat represents a physical seat in a cinema hall for a given show.
type Seat struct {
	ID        string    `json:"id"`
	ShowID    string    `json:"show_id"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
}

// SeatWithStatus represents a seat along with its real-time reservation availability.
type SeatWithStatus struct {
	ID     string     `json:"id"`
	Label  string     `json:"label"`
	Status SeatStatus `json:"status"`
}

// Reservation represents a single seat booking record and its lifecycle.
type Reservation struct {
	ID            string            `json:"id"`
	ShowID        string            `json:"show_id"`
	SeatID        string            `json:"seat_id"`
	SeatLabel     string            `json:"seat_label"`
	CustomerRef   string            `json:"customer_ref"`
	Status        ReservationStatus `json:"status"`
	HoldExpiresAt time.Time         `json:"hold_expires_at"`
	TicketID      *string           `json:"ticket_id,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	ConfirmedAt   *time.Time        `json:"confirmed_at,omitempty"`
	CancelledAt   *time.Time        `json:"cancelled_at,omitempty"`
}
