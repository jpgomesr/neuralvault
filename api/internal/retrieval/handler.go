package retrieval

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jpgomesr/NeuralVault/internal/conversations"
	"github.com/jpgomesr/NeuralVault/internal/logger"
	"github.com/jpgomesr/NeuralVault/internal/model"
	"github.com/jpgomesr/NeuralVault/internal/workspaces"
)

// queryRequest is the JSON body accepted by POST /query.
type queryRequest struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Question    string    `json:"question"`
	TopK        int       `json:"top_k"`
	// ConversationID is optional. When set, the question (and, for
	// /query/stream, the completed answer) are persisted as messages on the
	// conversation. Omitting it keeps the endpoint fully stateless.
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
}

// queryResultItem is a single hydrated chunk in the query response.
type queryResultItem struct {
	ChunkID string  `json:"chunk_id"`
	Content string  `json:"content"`
	Score   float32 `json:"score"`
}

// queryResponse is the JSON body returned by POST /query.
type queryResponse struct {
	Results []queryResultItem `json:"results"`
}

// Handler holds HTTP handler methods for the retrieval domain.
type Handler struct {
	service       Retriever
	members       workspaces.Service
	conversations conversations.Service
}

// NewHandler returns a Handler backed by service. members enforces that the
// caller belongs to the queried workspace; conversationsSvc persists
// messages when a request carries a conversation_id.
func NewHandler(service Retriever, members workspaces.Service, conversationsSvc conversations.Service) *Handler {
	return &Handler{service: service, members: members, conversations: conversationsSvc}
}

// ensureConversationInWorkspace loads conversationID and verifies it belongs
// to workspaceID, so a conversation from one workspace can't be used to
// persist messages while querying another. Writes the HTTP error itself and
// reports whether the caller should continue, mirroring workspaces.EnsureMember.
func (h *Handler) ensureConversationInWorkspace(w http.ResponseWriter, r *http.Request, conversationID, workspaceID uuid.UUID) bool {
	conv, err := h.conversations.GetByID(r.Context(), conversationID)
	if errors.Is(err, conversations.ErrNotFound) {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return false
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "loading conversation failed", "err", err, "conversation_id", conversationID, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "failed to load conversation: "+err.Error(), http.StatusInternalServerError)
		return false
	}
	if conv.WorkspaceID != workspaceID {
		slog.WarnContext(r.Context(), "conversation does not belong to workspace", "conversation_id", conversationID, "workspace_id", workspaceID, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "conversation does not belong to workspace_id", http.StatusBadRequest)
		return false
	}
	return true
}

// Query godoc
//
// Embeds the question, runs a workspace-scoped semantic search, and returns
// the top-k matching chunks ordered by descending similarity score.
//
// @Summary Query a workspace
// @Description Embeds the question from a JSON body {"workspace_id": uuid, "question": string, "top_k": int, "conversation_id": uuid (optional)} and returns the top-k matching chunks ordered by descending similarity score. If conversation_id is set, persists the question as a message on that conversation (no answer is generated here to persist).
// @Tags query
// @Accept json
// @Produce json
// @Success 200
// @Failure 400
// @Failure 401
// @Failure 403
// @Failure 500
// @Router /query [post]
func (h *Handler) Query(w http.ResponseWriter, r *http.Request) {
	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.WarnContext(r.Context(), "invalid query request", "err", err, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.WorkspaceID == uuid.Nil {
		slog.WarnContext(r.Context(), "invalid query request", "err", "workspace_id is required", "request_id", logger.RequestID(r.Context()))
		http.Error(w, "workspace_id is required", http.StatusBadRequest)
		return
	}
	if req.Question == "" {
		slog.WarnContext(r.Context(), "invalid query request", "err", "question is required", "request_id", logger.RequestID(r.Context()))
		http.Error(w, "question is required", http.StatusBadRequest)
		return
	}

	if !workspaces.EnsureMember(w, r, h.members, req.WorkspaceID) {
		return
	}

	if req.ConversationID != nil && !h.ensureConversationInWorkspace(w, r, *req.ConversationID, req.WorkspaceID) {
		return
	}

	results, err := h.service.Retrieve(r.Context(), RetrieveRequest{
		WorkspaceID: req.WorkspaceID,
		Query:       req.Question,
		TopK:        req.TopK,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "query failed", "err", err, "workspace_id", req.WorkspaceID, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "failed to run query: "+err.Error(), http.StatusInternalServerError)
		return
	}

	items := make([]queryResultItem, len(results))
	for i, res := range results {
		items[i] = queryResultItem{
			ChunkID: res.Chunk.ID.String(),
			Content: res.Chunk.Content,
			Score:   res.Score,
		}
	}

	// /query never generates an LLM answer (see Retrieve above), so only the
	// question has a turn to persist here; /query/stream persists the answer too.
	if req.ConversationID != nil {
		if _, err := h.conversations.AppendMessage(r.Context(), *req.ConversationID, model.MessageRoleUser, req.Question, nil); err != nil {
			slog.ErrorContext(r.Context(), "persisting question failed", "err", err, "conversation_id", *req.ConversationID, "request_id", logger.RequestID(r.Context()))
			http.Error(w, "failed to persist message: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	slog.InfoContext(r.Context(), "query completed",
		"workspace_id", req.WorkspaceID,
		"request_id", logger.RequestID(r.Context()),
		"result_count", len(items),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(queryResponse{Results: items}) //nolint:errcheck
}

// QueryStream godoc
//
// It emits a single "sources" event with the retrieved chunks, then incremental
// "token" events as the LLM generates the answer, and finally a terminal "done"
// (or "error") event. Browsers read this with a streaming fetch (POST carries
// the session cookie); the non-streaming Query above remains for the CLI.
//
// @Summary Stream a grounded answer (SSE)
// @Description Runs retrieval for a JSON body {"workspace_id": uuid, "question": string, "top_k": int, "conversation_id": uuid (optional)}, then streams SSE events: one "sources" event with the grounding chunks, incremental "token" events, and a terminal "done" or "error" event. If conversation_id is set, persists the question and the completed answer (with sources) as messages on that conversation.
// @Tags query
// @Accept json
// @Success 200
// @Failure 400
// @Failure 401
// @Failure 403
// @Failure 500
// @Router /query/stream [post]
func (h *Handler) QueryStream(w http.ResponseWriter, r *http.Request) {
	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.WarnContext(r.Context(), "invalid query request", "err", err, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.WorkspaceID == uuid.Nil {
		http.Error(w, "workspace_id is required", http.StatusBadRequest)
		return
	}
	if req.Question == "" {
		http.Error(w, "question is required", http.StatusBadRequest)
		return
	}

	if !workspaces.EnsureMember(w, r, h.members, req.WorkspaceID) {
		return
	}

	if req.ConversationID != nil && !h.ensureConversationInWorkspace(w, r, *req.ConversationID, req.WorkspaceID) {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported by this server", http.StatusInternalServerError)
		return
	}

	// Run retrieval and start the completion stream before writing SSE headers,
	// so a failure here can still return a plain error status.
	chunks, stream, err := h.service.Answer(r.Context(), RetrieveRequest{
		WorkspaceID: req.WorkspaceID,
		Query:       req.Question,
		TopK:        req.TopK,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "answer failed", "err", err, "workspace_id", req.WorkspaceID, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "failed to run query: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Persist the question before writing SSE headers, so a failure here can
	// still return a plain error status instead of corrupting an open stream.
	if req.ConversationID != nil {
		if _, err := h.conversations.AppendMessage(r.Context(), *req.ConversationID, model.MessageRoleUser, req.Question, nil); err != nil {
			slog.ErrorContext(r.Context(), "persisting question failed", "err", err, "conversation_id", *req.ConversationID, "request_id", logger.RequestID(r.Context()))
			http.Error(w, "failed to persist message: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Surface the grounding sources up front so the UI can render them while the
	// answer streams in.
	items := make([]queryResultItem, len(chunks))
	for i, res := range chunks {
		items[i] = queryResultItem{
			ChunkID: res.Chunk.ID.String(),
			Content: res.Chunk.Content,
			Score:   res.Score,
		}
	}
	sourcesPayload, _ := json.Marshal(queryResponse{Results: items})
	writeSSEEvent(w, "sources", queryResponse{Results: items})
	flusher.Flush()

	var answer strings.Builder
	for chunk := range stream {
		if chunk.Error != nil {
			slog.ErrorContext(r.Context(), "completion stream error", "err", chunk.Error, "workspace_id", req.WorkspaceID, "request_id", logger.RequestID(r.Context()))
			writeSSEEvent(w, "error", map[string]string{"error": chunk.Error.Error()})
			flusher.Flush()
			// A partial answer isn't persisted — only completed turns are.
			return
		}
		if chunk.Content != "" {
			answer.WriteString(chunk.Content)
			writeSSEEvent(w, "token", map[string]string{"content": chunk.Content})
			flusher.Flush()
		}
		if chunk.Done {
			writeSSEEvent(w, "done", map[string]any{})
			flusher.Flush()
			// SSE headers are already flushed, so a persistence failure here
			// can only be logged, not turned into an HTTP error response.
			if req.ConversationID != nil {
				if _, err := h.conversations.AppendMessage(r.Context(), *req.ConversationID, model.MessageRoleAssistant, answer.String(), sourcesPayload); err != nil {
					slog.ErrorContext(r.Context(), "persisting answer failed", "err", err, "conversation_id", *req.ConversationID, "request_id", logger.RequestID(r.Context()))
				}
			}
			return
		}
	}
}

// writeSSEEvent writes a named SSE event with a JSON data payload.
func writeSSEEvent(w http.ResponseWriter, event string, data any) {
	payload, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload) //nolint:errcheck
}
