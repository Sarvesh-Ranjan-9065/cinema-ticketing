package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockEvaluator struct {
	live  bool
	ready error
}

func (m *mockEvaluator) IsLive() bool {
	return m.live
}

func (m *mockEvaluator) CheckReady(_ context.Context) error {
	return m.ready
}

func setupTestRouter(evaluator HealthEvaluator) http.Handler {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	return NewRouter(RouterConfig{
		Logger: logger,
		Health: evaluator,
	})
}

func TestHealthz_Success(t *testing.T) {
	eval := &mockEvaluator{live: true}
	router := setupTestRouter(eval)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", contentType)
	}

	reqID := rec.Header().Get("X-Request-ID")
	if reqID == "" {
		t.Errorf("expected X-Request-ID header to be populated")
	}

	var body healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", body.Status)
	}
}

func TestReadyz_Success(t *testing.T) {
	eval := &mockEvaluator{live: true, ready: nil}
	router := setupTestRouter(eval)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var body healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", body.Status)
	}
}

func TestReadyz_Failure(t *testing.T) {
	eval := &mockEvaluator{live: true, ready: errors.New("database connection refused")}
	router := setupTestRouter(eval)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d", rec.Code)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if errResp.Code != "DEPENDENCY_UNAVAILABLE" {
		t.Errorf("expected code DEPENDENCY_UNAVAILABLE, got %q", errResp.Code)
	}
	if errResp.Message != "database connection refused" {
		t.Errorf("expected message 'database connection refused', got %q", errResp.Message)
	}
	if errResp.RequestID == "" {
		t.Errorf("expected request_id to be populated in error response")
	}
}

func TestNotFound_Handler(t *testing.T) {
	eval := &mockEvaluator{live: true}
	router := setupTestRouter(eval)

	req := httptest.NewRequest(http.MethodGet, "/unknown-route", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d", rec.Code)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if errResp.Code != "NOT_FOUND" {
		t.Errorf("expected code NOT_FOUND, got %q", errResp.Code)
	}
}
