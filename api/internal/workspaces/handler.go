package workspaces

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jpgomesr/NeuralVault/internal/auth"
	"github.com/jpgomesr/NeuralVault/internal/logger"
	"github.com/jpgomesr/NeuralVault/internal/model"
)

// Handler holds HTTP handler methods for the workspaces domain.
type Handler struct {
	service Service
}

// NewHandler returns a Handler backed by service.
func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

// createRequest is the JSON body accepted by POST /workspaces.
type createRequest struct {
	Name string `json:"name"`
}

// Create handles POST /workspaces. The creator becomes the workspace owner.
// It is mounted behind RequireUser, so the caller is always authenticated.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())

	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.WarnContext(r.Context(), "invalid create workspace request", "err", err, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		slog.WarnContext(r.Context(), "invalid create workspace request", "err", "name is required", "request_id", logger.RequestID(r.Context()))
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	ws, err := h.service.Create(r.Context(), userID, req.Name)
	if err != nil {
		slog.ErrorContext(r.Context(), "create workspace failed", "err", err, "user_id", userID, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "failed to create workspace: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.InfoContext(r.Context(), "workspace created", "workspace_id", ws.ID, "user_id", userID, "request_id", logger.RequestID(r.Context()))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(ws) //nolint:errcheck
}

// List handles GET /workspaces, returning the caller's workspaces.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())

	list, err := h.service.List(r.Context(), userID)
	if err != nil {
		slog.ErrorContext(r.Context(), "list workspaces failed", "err", err, "user_id", userID, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "failed to list workspaces: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []model.Workspace{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list) //nolint:errcheck
}
