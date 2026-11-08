// Package httpapi implements HTTP routing, handlers, middleware, and response formatting.
package httpapi

import (
	"context"
	"crypto/rand"
	"fmt"
)

type contextKey string

const requestIDKey contextKey = "request_id"

// NewRequestID generates an RFC 4122 version 4 UUID using cryptographic randomness.
func NewRequestID() string {
	var b [16]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// WithRequestID returns a new context containing the given request ID.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// GetRequestID extracts the request ID from the context, or returns an empty string if unset.
func GetRequestID(ctx context.Context) string {
	if val, ok := ctx.Value(requestIDKey).(string); ok {
		return val
	}
	return ""
}
