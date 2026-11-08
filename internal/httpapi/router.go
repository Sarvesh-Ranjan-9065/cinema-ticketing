package httpapi

import (
	"log/slog"
	"net/http"
)

// RouterConfig defines the dependencies required to initialize the API router.
type RouterConfig struct {
	// Logger provides structured logging for HTTP requests and panics.
	Logger *slog.Logger

	// Health evaluates liveness and readiness state.
	Health HealthEvaluator
}

// NewRouter wires up the HTTP routes, middleware, and fallback error handling.
func NewRouter(cfg RouterConfig) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", handleHealthz(cfg.Health))
	mux.HandleFunc("GET /readyz", handleReadyz(cfg.Health))

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
