package retrieval

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jpgomesr/NeuralVault/internal/logger"
	"github.com/jpgomesr/NeuralVault/internal/workspaces"
)

// queryRequest is the JSON body accepted by POST /query.
type queryRequest struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Question    string    `json:"question"`
	TopK        int       `json:"top_k"`
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
	service Retriever
	members workspaces.Service
}

// NewHandler returns a Handler backed by service. members enforces that the
// caller belongs to the queried workspace.
func NewHandler(service Retriever, members workspaces.Service) *Handler {
	return &Handler{service: service, members: members}
}

// Query handles POST /query.
// Embeds the question, runs a workspace-scoped semantic search, and returns
// the top-k matching chunks ordered by descending similarity score.
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

	slog.InfoContext(r.Context(), "query completed",
		"workspace_id", req.WorkspaceID,
		"request_id", logger.RequestID(r.Context()),
		"result_count", len(items),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(queryResponse{Results: items}) //nolint:errcheck
}

// QueryStream handles POST /query/stream as an SSE stream. It emits a single
// "sources" event with the retrieved chunks, then incremental "token" events
// as the LLM generates the answer, and finally a terminal "done" (or "error")
// event. Browsers read this with a streaming fetch (POST carries the session
// cookie); the non-streaming Query above remains for the CLI.
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
	writeSSEEvent(w, "sources", queryResponse{Results: items})
	flusher.Flush()

	for chunk := range stream {
		if chunk.Error != nil {
			slog.ErrorContext(r.Context(), "completion stream error", "err", chunk.Error, "workspace_id", req.WorkspaceID, "request_id", logger.RequestID(r.Context()))
			writeSSEEvent(w, "error", map[string]string{"error": chunk.Error.Error()})
			flusher.Flush()
			return
		}
		if chunk.Content != "" {
			writeSSEEvent(w, "token", map[string]string{"content": chunk.Content})
			flusher.Flush()
		}
		if chunk.Done {
			writeSSEEvent(w, "done", map[string]any{})
			flusher.Flush()
			return
		}
	}
}

// writeSSEEvent writes a named SSE event with a JSON data payload.
func writeSSEEvent(w http.ResponseWriter, event string, data any) {
	payload, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload) //nolint:errcheck
}
