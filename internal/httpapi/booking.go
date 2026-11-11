package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"cinema-ticketing/internal/booking"
)

var (
	uuidPattern      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	seatLabelPattern = regexp.MustCompile(`^[A-Za-z][0-9]{1,2}$`)
)

// BookingService specifies the domain capabilities required by HTTP handlers.
type BookingService interface {
	ListShows(ctx context.Context) ([]booking.Show, error)
	GetShow(ctx context.Context, id string) (*booking.Show, error)
	GetSeatMap(ctx context.Context, showID string) ([]booking.SeatWithStatus, error)
	CreateHold(ctx context.Context, showID, seatLabel, customerRef string, ttl time.Duration) (*booking.Reservation, error)
	GetReservation(ctx context.Context, id string) (*booking.Reservation, error)
	ConfirmReservation(ctx context.Context, id string) (*booking.Reservation, error)
	CancelReservation(ctx context.Context, id string) (*booking.Reservation, error)
}

type createHoldRequest struct {
	ShowID      string `json:"show_id"`
	SeatLabel   string `json:"seat_label"`
	CustomerRef string `json:"customer_ref"`
}

func handleListShows(svc BookingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		shows, err := svc.ListShows(r.Context())
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list shows")
			return
		}
		if shows == nil {
			shows = []booking.Show{}
		}
		writeJSON(w, http.StatusOK, shows)
	}
}

func handleGetShow(svc BookingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if !uuidPattern.MatchString(id) {
			writeError(w, r, http.StatusBadRequest, "INVALID_INPUT", "Show identifier must be a valid UUID")
			return
		}

		show, err := svc.GetShow(r.Context(), id)
		if err != nil {
			mapDomainError(w, r, err)
			return
		}

		writeJSON(w, http.StatusOK, show)
	}
}

func handleGetSeatMap(svc BookingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if !uuidPattern.MatchString(id) {
			writeError(w, r, http.StatusBadRequest, "INVALID_INPUT", "Show identifier must be a valid UUID")
			return
		}

		seats, err := svc.GetSeatMap(r.Context(), id)
		if err != nil {
			mapDomainError(w, r, err)
			return
		}
		if seats == nil {
			seats = []booking.SeatWithStatus{}
		}

		writeJSON(w, http.StatusOK, seats)
	}
}

func handleCreateHold(svc BookingService, holdTTL time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isJSONContentType(r.Header.Get("Content-Type")) {
			writeError(w, r, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json")
			return
		}

		// Limit request body to 64KB to prevent memory exhaustion
		r.Body = http.MaxBytesReader(w, r.Body, 64*1024)

		var req createHoldRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeError(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "Request body exceeds maximum allowed size")
				return
			}
			writeError(w, r, http.StatusBadRequest, "INVALID_INPUT", fmt.Sprintf("Malformed JSON body: %v", err))
			return
		}

		req.ShowID = strings.TrimSpace(req.ShowID)
		if !uuidPattern.MatchString(req.ShowID) {
			writeError(w, r, http.StatusBadRequest, "INVALID_INPUT", "show_id must be a valid UUID")
			return
		}

		req.SeatLabel = strings.TrimSpace(req.SeatLabel)
		if !seatLabelPattern.MatchString(req.SeatLabel) {
			writeError(w, r, http.StatusBadRequest, "INVALID_INPUT", "seat_label must follow pattern (e.g. A1, B12)")
			return
		}

		req.CustomerRef = strings.TrimSpace(req.CustomerRef)
		if req.CustomerRef == "" || len(req.CustomerRef) > 100 || hasControlChars(req.CustomerRef) {
			writeError(w, r, http.StatusBadRequest, "INVALID_INPUT", "customer_ref must be 1-100 printable ASCII characters")
			return
		}

		res, err := svc.CreateHold(r.Context(), req.ShowID, req.SeatLabel, req.CustomerRef, holdTTL)
		if err != nil {
			mapDomainError(w, r, err)
			return
		}

		writeJSON(w, http.StatusCreated, res)
	}
}

func handleGetReservation(svc BookingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if !uuidPattern.MatchString(id) {
			writeError(w, r, http.StatusBadRequest, "INVALID_INPUT", "Reservation identifier must be a valid UUID")
			return
		}

		res, err := svc.GetReservation(r.Context(), id)
		if err != nil {
			mapDomainError(w, r, err)
			return
		}

		writeJSON(w, http.StatusOK, res)
	}
}

func handleConfirmReservation(svc BookingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if !uuidPattern.MatchString(id) {
			writeError(w, r, http.StatusBadRequest, "INVALID_INPUT", "Reservation identifier must be a valid UUID")
			return
		}

		res, err := svc.ConfirmReservation(r.Context(), id)
		if err != nil {
			mapDomainError(w, r, err)
			return
		}

		writeJSON(w, http.StatusOK, res)
	}
}

func handleCancelReservation(svc BookingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if !uuidPattern.MatchString(id) {
			writeError(w, r, http.StatusBadRequest, "INVALID_INPUT", "Reservation identifier must be a valid UUID")
			return
		}

		res, err := svc.CancelReservation(r.Context(), id)
		if err != nil {
			mapDomainError(w, r, err)
			return
		}

		writeJSON(w, http.StatusOK, res)
	}
}

func mapDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, booking.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, booking.ErrSeatTaken):
		writeError(w, r, http.StatusConflict, "SEAT_TAKEN", err.Error())
	case errors.Is(err, booking.ErrHoldExpired):
		writeError(w, r, http.StatusConflict, "HOLD_EXPIRED", err.Error())
	case errors.Is(err, booking.ErrInvalidTransition):
		writeError(w, r, http.StatusConflict, "INVALID_STATE_TRANSITION", err.Error())
	case errors.Is(err, booking.ErrShowStarted):
		writeError(w, r, http.StatusConflict, "SHOW_ALREADY_STARTED", err.Error())
	case errors.Is(err, booking.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "INVALID_INPUT", err.Error())
	default:
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "An internal error occurred")
	}
}

func isJSONContentType(header string) bool {
	ct := strings.ToLower(strings.TrimSpace(header))
	return strings.HasPrefix(ct, "application/json")
}

func hasControlChars(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 32 || s[i] == 127 {
			return true
		}
	}
	return false
}
