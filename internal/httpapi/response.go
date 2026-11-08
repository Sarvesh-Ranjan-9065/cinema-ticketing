package httpapi

import (
	"encoding/json"
	"net/http"
)

// ErrorResponse represents the unified JSON error payload returned by all API endpoints.
type ErrorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// writeJSON writes a JSON payload with appropriate status and Content-Type header.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError writes the standardized error JSON response with the current request ID.
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	reqID := GetRequestID(r.Context())
	writeJSON(w, status, ErrorResponse{
		Code:      code,
		Message:   message,
		RequestID: reqID,
	})
}
