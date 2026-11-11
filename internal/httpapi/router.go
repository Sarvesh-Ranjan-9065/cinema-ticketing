package httpapi

import (
	"log/slog"
	"net/http"
	"time"
)

// RouterConfig defines the dependencies required to initialize the API router.
type RouterConfig struct {
	// Logger provides structured logging for HTTP requests and panics.
	Logger *slog.Logger

	// Health evaluates liveness and readiness state.
	Health HealthEvaluator

	// Booking handles cinema shows, seats, and reservation operations.
	Booking BookingService

	// HoldTTL specifies the lifetime of a seat hold.
	HoldTTL time.Duration
}

// NewRouter wires up the HTTP routes, middleware, and fallback error handling.
func NewRouter(cfg RouterConfig) http.Handler {
	mux := http.NewServeMux()

	// Probes
	mux.HandleFunc("GET /healthz", handleHealthz(cfg.Health))
	mux.HandleFunc("GET /readyz", handleReadyz(cfg.Health))

	// Domain API routes (if booking service provided)
	if cfg.Booking != nil {
		mux.HandleFunc("GET /v1/shows", handleListShows(cfg.Booking))
		mux.HandleFunc("GET /v1/shows/{id}", handleGetShow(cfg.Booking))
		mux.HandleFunc("GET /v1/shows/{id}/seats", handleGetSeatMap(cfg.Booking))
		mux.HandleFunc("POST /v1/reservations", handleCreateHold(cfg.Booking, cfg.HoldTTL))
		mux.HandleFunc("GET /v1/reservations/{id}", handleGetReservation(cfg.Booking))
		mux.HandleFunc("POST /v1/reservations/{id}/confirm", handleConfirmReservation(cfg.Booking))
		mux.HandleFunc("POST /v1/reservations/{id}/cancel", handleCancelReservation(cfg.Booking))
	}

	// Fallback 404 for unmatched routes returning consistent JSON error
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "The requested endpoint does not exist")
	})

	var handler http.Handler = mux
	handler = RecoveryMiddleware(cfg.Logger, handler)
	handler = LoggingMiddleware(cfg.Logger, handler)
	handler = RequestIDMiddleware(handler)

	return handler
}
