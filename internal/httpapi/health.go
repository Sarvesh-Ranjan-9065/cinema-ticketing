package httpapi

import (
	"context"
	"net/http"
)

// HealthEvaluator defines the contract required to assess service health.
type HealthEvaluator interface {
	IsLive() bool
	CheckReady(ctx context.Context) error
}

type healthResponse struct {
	Status string `json:"status"`
}

func handleHealthz(evaluator HealthEvaluator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !evaluator.IsLive() {
			writeError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Service is not live")
			return
		}
		writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
	}
}

func handleReadyz(evaluator HealthEvaluator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := evaluator.CheckReady(r.Context()); err != nil {
			writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
	}
}
