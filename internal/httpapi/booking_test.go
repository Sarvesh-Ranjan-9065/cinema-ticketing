package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cinema-ticketing/internal/booking"
)

type mockBookingService struct {
	shows         []booking.Show
	seatMap       []booking.SeatWithStatus
	reservation   *booking.Reservation
	err           error
	createHoldErr error
	confirmErr    error
	cancelErr     error
}

func (m *mockBookingService) ListShows(_ context.Context) ([]booking.Show, error) {
	return m.shows, m.err
}

func (m *mockBookingService) GetShow(_ context.Context, _ string) (*booking.Show, error) {
	if m.err != nil {
		return nil, m.err
	}
	if len(m.shows) > 0 {
		return &m.shows[0], nil
	}
	return nil, booking.ErrNotFound
}

func (m *mockBookingService) GetSeatMap(_ context.Context, _ string) ([]booking.SeatWithStatus, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.seatMap, nil
}

func (m *mockBookingService) CreateHold(_ context.Context, showID, seatLabel, customerRef string, _ time.Duration) (*booking.Reservation, error) {
	if m.createHoldErr != nil {
		return nil, m.createHoldErr
	}
	return &booking.Reservation{
		ID:          "11111111-2222-3333-4444-555555555555",
		ShowID:      showID,
		SeatLabel:   seatLabel,
		CustomerRef: customerRef,
		Status:      booking.StatusHeld,
	}, nil
}

func (m *mockBookingService) GetReservation(_ context.Context, _ string) (*booking.Reservation, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.reservation != nil {
		return m.reservation, nil
	}
	return nil, booking.ErrNotFound
}

func (m *mockBookingService) ConfirmReservation(_ context.Context, _ string) (*booking.Reservation, error) {
	if m.confirmErr != nil {
		return nil, m.confirmErr
	}
	tkt := "TKT-12345678"
	return &booking.Reservation{
		ID:       "11111111-2222-3333-4444-555555555555",
		Status:   booking.StatusConfirmed,
		TicketID: &tkt,
	}, nil
}

func (m *mockBookingService) CancelReservation(_ context.Context, _ string) (*booking.Reservation, error) {
	if m.cancelErr != nil {
		return nil, m.cancelErr
	}
	return &booking.Reservation{
		ID:     "11111111-2222-3333-4444-555555555555",
		Status: booking.StatusCancelled,
	}, nil
}

func setupTestBookingRouter(svc BookingService) http.Handler {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	eval := &mockEvaluator{live: true, ready: nil}
	return NewRouter(RouterConfig{
		Logger:  logger,
		Health:  eval,
		Booking: svc,
		HoldTTL: 120 * time.Second,
	})
}

func TestListShows_Success(t *testing.T) {
	svc := &mockBookingService{
		shows: []booking.Show{
			{ID: "00000000-0000-0000-0000-000000000001", Title: "Dune", Hall: "Hall 1"},
		},
	}
	router := setupTestBookingRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/v1/shows", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var shows []booking.Show
	if err := json.Unmarshal(rec.Body.Bytes(), &shows); err != nil {
		t.Fatalf("failed decoding shows: %v", err)
	}

	if len(shows) != 1 || shows[0].Title != "Dune" {
		t.Errorf("unexpected shows response: %+v", shows)
	}
}

func TestGetShow_InvalidUUID(t *testing.T) {
	router := setupTestBookingRouter(&mockBookingService{})

	req := httptest.NewRequest(http.MethodGet, "/v1/shows/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
	}
}

func TestCreateHold_ValidationErrors(t *testing.T) {
	router := setupTestBookingRouter(&mockBookingService{})

	tests := []struct {
		name        string
		contentType string
		body        string
		wantCode    int
		wantErrCode string
	}{
		{
			name:        "missing content type",
			contentType: "text/plain",
			body:        `{}`,
			wantCode:    http.StatusUnsupportedMediaType,
			wantErrCode: "UNSUPPORTED_MEDIA_TYPE",
		},
		{
			name:        "malformed json",
			contentType: "application/json",
			body:        `{invalid json`,
			wantCode:    http.StatusBadRequest,
			wantErrCode: "INVALID_INPUT",
		},
		{
			name:        "non-uuid show id",
			contentType: "application/json",
			body:        `{"show_id":"abc","seat_label":"A1","customer_ref":"cust1"}`,
			wantCode:    http.StatusBadRequest,
			wantErrCode: "INVALID_INPUT",
		},
		{
			name:        "invalid seat label",
			contentType: "application/json",
			body:        `{"show_id":"00000000-0000-0000-0000-000000000001","seat_label":"INVALID","customer_ref":"cust1"}`,
			wantCode:    http.StatusBadRequest,
			wantErrCode: "INVALID_INPUT",
		},
		{
			name:        "empty customer ref",
			contentType: "application/json",
			body:        `{"show_id":"00000000-0000-0000-0000-000000000001","seat_label":"A1","customer_ref":""}`,
			wantCode:    http.StatusBadRequest,
			wantErrCode: "INVALID_INPUT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/reservations", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Fatalf("expected status %d, got %d. Body: %s", tt.wantCode, rec.Code, rec.Body.String())
			}

			var errResp ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
				t.Fatalf("failed decoding error response: %v", err)
			}

			if errResp.Code != tt.wantErrCode {
				t.Errorf("expected error code %s, got %s", tt.wantErrCode, errResp.Code)
			}
		})
	}
}

func TestCreateHold_PayloadTooLarge(t *testing.T) {
	router := setupTestBookingRouter(&mockBookingService{})

	// 70KB oversized payload formatted as valid JSON (limit is 64KB)
	largeBody := `{"show_id":"00000000-0000-0000-0000-000000000001","seat_label":"A1","customer_ref":"` + strings.Repeat("A", 70*1024) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/reservations", strings.NewReader(largeBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 Payload Too Large, got %d", rec.Code)
	}
}

func TestCreateHold_SeatTakenConflict(t *testing.T) {
	svc := &mockBookingService{
		createHoldErr: booking.ErrSeatTaken,
	}
	router := setupTestBookingRouter(svc)

	body := `{"show_id":"00000000-0000-0000-0000-000000000001","seat_label":"A1","customer_ref":"cust-1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/reservations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d", rec.Code)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed decoding error response: %v", err)
	}
	if errResp.Code != "SEAT_TAKEN" {
		t.Errorf("expected code SEAT_TAKEN, got %s", errResp.Code)
	}
}

func TestConfirmReservation_ExpiredHoldConflict(t *testing.T) {
	svc := &mockBookingService{
		confirmErr: booking.ErrHoldExpired,
	}
	router := setupTestBookingRouter(svc)

	req := httptest.NewRequest(http.MethodPost, "/v1/reservations/00000000-0000-0000-0000-000000000001/confirm", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d", rec.Code)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed decoding error: %v", err)
	}
	if errResp.Code != "HOLD_EXPIRED" {
		t.Errorf("expected code HOLD_EXPIRED, got %s", errResp.Code)
	}
}
