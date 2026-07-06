package health

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// StatusOK and StatusDown are the per-service status values reported by /health.
const (
	StatusOK   = "ok"
	StatusDown = "down"
)

// CheckFunc probes a single dependency and returns an error when it is
// unreachable or otherwise unhealthy.
type CheckFunc func(ctx context.Context) error

// Check pairs a dependency name (as it appears in the health report) with the
// function that probes it.
type Check struct {
	Name string
	Fn   CheckFunc
}

// Report is the result of running every health check. Status is "ok" only when
// all services are "ok", otherwise "degraded". Services maps each dependency
// name to StatusOK or StatusDown.
type Report struct {
	Status   string            `json:"status"`
	Services map[string]string `json:"services"`
}

// Healthy reports whether every service is up.
func (r Report) Healthy() bool {
	return r.Status == StatusOK
}

// HealthService runs a fixed set of dependency checks concurrently, each bounded
// by timeout so a single hung dependency cannot stall the whole /health request.
type HealthService struct {
	checks  []Check
	timeout time.Duration
}

// NewHealthService returns a HealthService that runs the given checks, each
// bounded by timeout.
func NewHealthService(timeout time.Duration, checks ...Check) *HealthService {
	return &HealthService{checks: checks, timeout: timeout}
}

// AllHealth runs every check concurrently and returns the aggregated report.
// A check that errors or exceeds the timeout is recorded as StatusDown; the
// process answering at all means the server itself is up.
func (s *HealthService) AllHealth(ctx context.Context) Report {
	services := map[string]string{"server": StatusOK}
	var mu sync.Mutex

	var wg sync.WaitGroup
	for _, c := range s.checks {
		wg.Add(1)
		go func(c Check) {
			defer wg.Done()

			checkCtx, cancel := context.WithTimeout(ctx, s.timeout)
			defer cancel()

			status := StatusOK
			if err := c.Fn(checkCtx); err != nil {
				status = StatusDown
				slog.Warn("health check failed", "service", c.Name, "err", err)
			}

			mu.Lock()
			services[c.Name] = status
			mu.Unlock()
		}(c)
	}
	wg.Wait()

	status := StatusOK
	for _, svcStatus := range services {
		if svcStatus != StatusOK {
			status = "degraded"
			break
		}
	}

	return Report{Status: status, Services: services}
}
