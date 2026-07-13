package workspaces

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jpgomesr/NeuralVault/internal/auth"
	"github.com/jpgomesr/NeuralVault/internal/httperr"
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

// Create godoc
//
// The creator becomes the workspace owner. It is mounted behind RequireUser,
// so the caller is always authenticated.
//
// @Summary Create a workspace
// @Description Creates a workspace from a JSON body {"name": string}; the caller becomes its owner.
// @Tags workspaces
// @Accept json
// @Produce json
// @Success 201
// @Failure 400
// @Failure 401
// @Failure 500
// @Router /workspaces [post]
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
		httperr.Internal(w, r, "create workspace failed", err, "user_id", userID)
		return
	}

	slog.InfoContext(r.Context(), "workspace created", "workspace_id", ws.ID, "user_id", userID, "request_id", logger.RequestID(r.Context()))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(ws) //nolint:errcheck
}

// List godoc
//
// @Summary List workspaces
// @Description Returns the workspaces the authenticated caller belongs to.
// @Tags workspaces
// @Produce json
// @Success 200
// @Failure 401
// @Failure 500
// @Router /workspaces [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())

	list, err := h.service.List(r.Context(), userID)
	if err != nil {
		httperr.Internal(w, r, "list workspaces failed", err, "user_id", userID)
		return
	}
	if list == nil {
		list = []model.Workspace{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list) //nolint:errcheck
}
