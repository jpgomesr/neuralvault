package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeService returns a fixed Report so the handler's status-code and encoding
// logic can be tested without real dependencies.
type fakeService struct{ report Report }

func (f fakeService) AllHealth(context.Context) Report { return f.report }

func TestGetHealth(t *testing.T) {
	tests := []struct {
		name       string
		report     Report
		wantStatus int
	}{
		{
			name: "all healthy returns 200",
			report: Report{
				Status:   StatusOK,
				Services: map[string]string{"server": StatusOK, "postgres": StatusOK},
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "degraded returns 503",
			report: Report{
				Status:   "degraded",
				Services: map[string]string{"server": StatusOK, "postgres": StatusDown},
			},
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(fakeService{report: tt.report})

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			h.GetHealth(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}

			var got Report
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decoding body: %v", err)
			}
			if got.Status != tt.report.Status {
				t.Errorf("body status = %q, want %q", got.Status, tt.report.Status)
			}
			if len(got.Services) != len(tt.report.Services) {
				t.Errorf("body services = %v, want %v", got.Services, tt.report.Services)
			}
		})
	}
}
