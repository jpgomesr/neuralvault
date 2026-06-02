package health

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type Service interface {
	AllHealth() (ConnectionHealth, error)
}

type ConnectionHealth struct {
	Server   string `json:"server"`
	Database string `json:"database"`
}

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{
		service: service,
	}
}

func (h *Handler) GetHealth(w http.ResponseWriter, r *http.Request) {
	connection, err := h.service.AllHealth()
	if err != nil {
		slog.Error(
			"error getting all health connection ",
			"err", err,
		)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(connection); err != nil {
		slog.Error(
			"error encoding all health connection ",
			"err", err,
		)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
