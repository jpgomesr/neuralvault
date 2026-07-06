package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

type Service interface {
	AllHealth(ctx context.Context) Report
}

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{
		service: service,
	}
}

// GetHealth godoc
//
// @Summary Get all health status
// @Description Probes every infrastructure dependency (Postgres, Qdrant, MinIO, Keycloak, Ollama) and reports each one. Returns 200 when all are healthy and 503 when any is down.
// @Tags health
// @Produce json
// @Success 200 {object} Report "All dependencies healthy"
// @Failure 503 {object} Report "One or more dependencies are down"
// @Router /health [get]
func (h *Handler) GetHealth(w http.ResponseWriter, r *http.Request) {
	report := h.service.AllHealth(r.Context())

	status := http.StatusOK
	if !report.Healthy() {
		status = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(report); err != nil {
		slog.Error(
			"error encoding health report",
			"err", err,
		)
	}
}
