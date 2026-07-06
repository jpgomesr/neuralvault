package health

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func ok(context.Context) error   { return nil }
func fail(context.Context) error { return errors.New("boom") }

func TestAllHealth(t *testing.T) {
	tests := []struct {
		name       string
		checks     []Check
		wantStatus string
		wantHealth bool
		want       map[string]string
	}{
		{
			name: "all healthy",
			checks: []Check{
				{Name: "postgres", Fn: ok},
				{Name: "qdrant", Fn: ok},
			},
			wantStatus: StatusOK,
			wantHealth: true,
			want: map[string]string{
				"server":   StatusOK,
				"postgres": StatusOK,
				"qdrant":   StatusOK,
			},
		},
		{
			name: "one down",
			checks: []Check{
				{Name: "postgres", Fn: fail},
				{Name: "qdrant", Fn: ok},
			},
			wantStatus: "degraded",
			wantHealth: false,
			want: map[string]string{
				"server":   StatusOK,
				"postgres": StatusDown,
				"qdrant":   StatusOK,
			},
		},
		{
			name:       "no checks: server still reported",
			checks:     nil,
			wantStatus: StatusOK,
			wantHealth: true,
			want:       map[string]string{"server": StatusOK},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewHealthService(time.Second, tt.checks...)
			report := svc.AllHealth(context.Background())

			if report.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", report.Status, tt.wantStatus)
			}
			if report.Healthy() != tt.wantHealth {
				t.Errorf("Healthy() = %v, want %v", report.Healthy(), tt.wantHealth)
			}
			if len(report.Services) != len(tt.want) {
				t.Fatalf("Services = %v, want %v", report.Services, tt.want)
			}
			for name, want := range tt.want {
				if got := report.Services[name]; got != want {
					t.Errorf("Services[%q] = %q, want %q", name, got, want)
				}
			}
		})
	}
}

// TestAllHealth_TimeoutRecordedAsDown verifies a check that outlives the
// per-check timeout is reported "down" and does not stall the whole request.
func TestAllHealth_TimeoutRecordedAsDown(t *testing.T) {
	blocking := func(ctx context.Context) error {
		<-ctx.Done() // respects the per-check timeout context
		return ctx.Err()
	}

	svc := NewHealthService(50*time.Millisecond,
		Check{Name: "postgres", Fn: ok},
		Check{Name: "slow", Fn: blocking},
	)

	start := time.Now()
	report := svc.AllHealth(context.Background())
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("AllHealth took %v; expected it to return near the 50ms timeout", elapsed)
	}
	if report.Services["slow"] != StatusDown {
		t.Errorf("slow = %q, want %q", report.Services["slow"], StatusDown)
	}
	if report.Services["postgres"] != StatusOK {
		t.Errorf("postgres = %q, want %q", report.Services["postgres"], StatusOK)
	}
	if report.Healthy() {
		t.Error("Healthy() = true, want false when a check timed out")
	}
}

// TestAllHealth_RunsConcurrently verifies checks run in parallel: N checks that
// each block until the timeout complete in ~one timeout, not N of them.
func TestAllHealth_RunsConcurrently(t *testing.T) {
	var running int32
	var maxConcurrent int32

	slow := func(ctx context.Context) error {
		n := atomic.AddInt32(&running, 1)
		for {
			if cur := atomic.LoadInt32(&maxConcurrent); n > cur {
				if atomic.CompareAndSwapInt32(&maxConcurrent, cur, n) {
					break
				}
				continue
			}
			break
		}
		<-ctx.Done()
		atomic.AddInt32(&running, -1)
		return ctx.Err()
	}

	checks := make([]Check, 4)
	for i := range checks {
		checks[i] = Check{Name: string(rune('a' + i)), Fn: slow}
	}

	svc := NewHealthService(50*time.Millisecond, checks...)
	svc.AllHealth(context.Background())

	if maxConcurrent < int32(len(checks)) {
		t.Errorf("max concurrent checks = %d, want %d (checks not running in parallel)", maxConcurrent, len(checks))
	}
}
