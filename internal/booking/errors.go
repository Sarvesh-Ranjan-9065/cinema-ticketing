package booking

import "errors"

// Domain sentinel errors mapped to specific HTTP status codes by the presentation layer.
var (
	// ErrNotFound indicates the requested show, seat, or reservation does not exist.
	ErrNotFound = errors.New("resource not found")

	// ErrSeatTaken indicates the seat is actively held or confirmed by another reservation.
	ErrSeatTaken = errors.New("seat is already reserved or held")

	// ErrHoldExpired indicates the temporary hold has expired and cannot be confirmed.
	ErrHoldExpired = errors.New("reservation hold has expired")

	// ErrInvalidTransition indicates the requested status transition is not permitted.
	ErrInvalidTransition = errors.New("invalid reservation state transition")

	// ErrShowStarted indicates a reservation cannot be created for a show that has already started.
	ErrShowStarted = errors.New("show has already started")

	// ErrInvalidInput indicates validation failure for input parameters.
	ErrInvalidInput = errors.New("invalid input data")
)
