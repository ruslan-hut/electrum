package internal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type contextKey string

const requestIDKey contextKey = "requestID"

// GenerateRequestID creates a unique request identifier.
func GenerateRequestID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID if random generation fails
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// WithRequestID adds a request ID to the context.
// If the context already has a request ID, it returns the context unchanged.
func WithRequestID(ctx context.Context) context.Context {
	if _, ok := ctx.Value(requestIDKey).(string); ok {
		// Already has a request ID
		return ctx
	}
	return context.WithValue(ctx, requestIDKey, GenerateRequestID())
}

// GetRequestID retrieves the request ID from the context.
// Returns an empty string if no request ID is present.
func GetRequestID(ctx context.Context) string {
	if reqID, ok := ctx.Value(requestIDKey).(string); ok {
		return reqID
	}
	return ""
}
