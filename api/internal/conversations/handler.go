package conversations

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jpgomesr/neuralvault/api/internal/httperr"
	"github.com/jpgomesr/neuralvault/api/internal/logger"
	"github.com/jpgomesr/neuralvault/api/internal/model"
	"github.com/jpgomesr/neuralvault/api/internal/workspaces"
)

// createRequest is the JSON body accepted by POST /conversations.
type createRequest struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

// Handler holds HTTP handler methods for the conversations domain.
type Handler struct {
	service Service
	members workspaces.Service
}

// NewHandler returns a Handler backed by service. members enforces that the
// caller belongs to the conversation's workspace.
func NewHandler(service Service, members workspaces.Service) *Handler {
	return &Handler{service: service, members: members}
}

// Create godoc
//
// @Summary Create a conversation
// @Description Creates an untitled conversation from a JSON body {"workspace_id": uuid}.
// @Tags conversations
// @Accept json
// @Produce json
// @Success 201
// @Failure 400
// @Failure 401
// @Failure 403
// @Failure 500
// @Router /conversations [post]
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.WarnContext(r.Context(), "invalid create conversation request", "err", err, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.WorkspaceID == uuid.Nil {
		slog.WarnContext(r.Context(), "invalid create conversation request", "err", "workspace_id is required", "request_id", logger.RequestID(r.Context()))
		http.Error(w, "workspace_id is required", http.StatusBadRequest)
		return
	}

	if !workspaces.EnsureMember(w, r, h.members, req.WorkspaceID) {
		return
	}

	conv, err := h.service.Create(r.Context(), req.WorkspaceID)
	if err != nil {
		httperr.Internal(w, r, "create conversation failed", err, "workspace_id", req.WorkspaceID)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(conv) //nolint:errcheck
}

// List godoc
//
// @Summary List a workspace's conversations
// @Description Returns the workspace's conversations, most recently active first.
// @Tags conversations
// @Produce json
// @Param workspace_id query string true "Workspace UUID"
// @Success 200
// @Failure 400
// @Failure 401
// @Failure 403
// @Failure 500
// @Router /conversations [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(r.URL.Query().Get("workspace_id"))
	if err != nil {
		slog.WarnContext(r.Context(), "invalid list conversations request", "err", "invalid workspace_id", "request_id", logger.RequestID(r.Context()))
		http.Error(w, "workspace_id must be a UUID", http.StatusBadRequest)
		return
	}

	if !workspaces.EnsureMember(w, r, h.members, workspaceID) {
		return
	}

	list, err := h.service.List(r.Context(), workspaceID)
	if err != nil {
		httperr.Internal(w, r, "list conversations failed", err, "workspace_id", workspaceID)
		return
	}
	if list == nil {
		list = []model.Conversation{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list) //nolint:errcheck
}

// ListMessages godoc
//
// @Summary List a conversation's messages
// @Description Returns the conversation's messages, oldest first.
// @Tags conversations
// @Produce json
// @Param id path string true "Conversation UUID"
// @Success 200
// @Failure 400
// @Failure 401
// @Failure 403
// @Failure 404
// @Failure 500
// @Router /conversations/{id}/messages [get]
func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		slog.WarnContext(r.Context(), "invalid list messages request", "err", "invalid conversation id", "request_id", logger.RequestID(r.Context()))
		http.Error(w, "invalid conversation id: must be a UUID", http.StatusBadRequest)
		return
	}

	conv, err := h.service.GetByID(r.Context(), conversationID)
	if errors.Is(err, ErrNotFound) {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return
	}
	if err != nil {
		httperr.Internal(w, r, "list messages failed", err, "conversation_id", conversationID)
		return
	}

	if !workspaces.EnsureMember(w, r, h.members, conv.WorkspaceID) {
		return
	}

	msgs, err := h.service.ListMessages(r.Context(), conversationID)
	if err != nil {
		httperr.Internal(w, r, "list messages failed", err, "conversation_id", conversationID)
		return
	}
	if msgs == nil {
		msgs = []model.Message{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msgs) //nolint:errcheck
}
