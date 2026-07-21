// Package httperr centralizes how handlers report server-side (5xx) failures:
// the real error is always logged via slog with the request ID attached, and
// clients only ever see a generic message plus that same request ID — never
// raw Postgres/MinIO/Qdrant error text or file paths.
package httperr

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jpgomesr/neuralvault/api/internal/logger"
)

const genericMessage = "internal server error"

type body struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id,omitempty"`
}

// Internal logs err server-side (with the request ID and any extra kv
// context) and writes a generic 500 JSON response carrying the request ID,
// so the caller never leaks internal error detail to the client.
func Internal(w http.ResponseWriter, r *http.Request, logMsg string, err error, kv ...any) {
	reqID := logSlog(r, logMsg, err, kv...)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(body{Error: genericMessage, RequestID: reqID})
}

// Message performs the same server-side logging as Internal but returns the
// generic client-safe string instead of writing a response, for call sites
// that can't write a JSON body directly (e.g. an SSE "error" event after
// streaming has already started).
func Message(r *http.Request, logMsg string, err error, kv ...any) string {
	reqID := logSlog(r, logMsg, err, kv...)
	if reqID == "" {
		return genericMessage
	}
	return genericMessage + " (request_id: " + reqID + ")"
}

func logSlog(r *http.Request, logMsg string, err error, kv ...any) string {
	reqID := logger.RequestID(r.Context())
	args := append([]any{"err", err, "request_id", reqID}, kv...)
	slog.ErrorContext(r.Context(), logMsg, args...)
	return reqID
}
