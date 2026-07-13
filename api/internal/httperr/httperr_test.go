package httperr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

func withRequestID(r *http.Request, id string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.RequestIDKey, id))
}

func TestInternal(t *testing.T) {
	tests := []struct {
		name          string
		withRequestID bool
		wantRequestID string
	}{
		{name: "with request id", withRequestID: true, wantRequestID: "req-123"},
		{name: "without request id", withRequestID: false, wantRequestID: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/foo", nil)
			if tt.withRequestID {
				req = withRequestID(req, tt.wantRequestID)
			}
			rec := httptest.NewRecorder()

			Internal(rec, req, "operation failed", errors.New("pq: connection refused"))

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}

			var got body
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if got.Error != genericMessage {
				t.Errorf("error = %q, want %q", got.Error, genericMessage)
			}
			if got.RequestID != tt.wantRequestID {
				t.Errorf("request_id = %q, want %q", got.RequestID, tt.wantRequestID)
			}
			if strings.Contains(rec.Body.String(), "connection refused") {
				t.Errorf("response body leaked internal error detail: %s", rec.Body.String())
			}
		})
	}
}

func TestMessage(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	req = withRequestID(req, "req-456")

	got := Message(req, "operation failed", errors.New("s3: access denied"))

	if strings.Contains(got, "access denied") {
		t.Errorf("message leaked internal error detail: %s", got)
	}
	if !strings.Contains(got, "req-456") {
		t.Errorf("message = %q, want it to contain the request id", got)
	}
}

func TestMessage_NoRequestID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)

	got := Message(req, "operation failed", errors.New("boom"))

	if got != genericMessage {
		t.Errorf("message = %q, want %q", got, genericMessage)
	}
}
