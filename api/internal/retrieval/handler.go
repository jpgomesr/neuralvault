package retrieval

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jpgomesr/NeuralVault/internal/conversations"
	"github.com/jpgomesr/NeuralVault/internal/httperr"
	"github.com/jpgomesr/NeuralVault/internal/llm"
	"github.com/jpgomesr/NeuralVault/internal/logger"
	"github.com/jpgomesr/NeuralVault/internal/model"
	"github.com/jpgomesr/NeuralVault/internal/workspaces"
)

// sseHeartbeatInterval bounds how long the SSE stream can stay silent while
// waiting on slow work (Answer's retrieval/rerank/LLM-startup, or a gap
// between generated tokens). The frontend's Next.js rewrite proxy
// (web/next.config.mjs) kills a connection after roughly 30s of no bytes
// flowing, even after the response has already started — a comment line at
// half that interval keeps it well clear. SSE comment lines (no "event:"/
// "data:" prefix) are silently ignored by dispatchSSE on the frontend (see
// web/lib/api/query.ts). Var, not const, so tests can shrink it instead of
// waiting out the real interval.
var sseHeartbeatInterval = 15 * time.Second

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
		httperr.Internal(w, r, "loading conversation failed", err, "conversation_id", conversationID)
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
		httperr.Internal(w, r, "query failed", err, "workspace_id", req.WorkspaceID)
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
			httperr.Internal(w, r, "persisting question failed", err, "conversation_id", *req.ConversationID)
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
		httperr.Internal(w, r, "query stream failed", errors.New("response writer does not support flushing"), "workspace_id", req.WorkspaceID)
		return
	}

	// Clear the write deadline so the server's WriteTimeout (120s default)
	// doesn't cut off this stream regardless of how long Answer() or token
	// generation takes — heartbeats alone only defeat a downstream proxy's
	// idle-connection detection, not this server's own fixed per-request
	// deadline. Same pattern as the sources status stream. Ignore ErrNotSupported.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

	// Send SSE headers and flush immediately, before the potentially slow
	// retrieval/rerank/LLM-startup work in Answer(). The frontend's Next.js
	// rewrite proxy (web/next.config.mjs) enforces a hard, non-configurable
	// 30s timeout waiting for the first response byte — without an early
	// flush here, a slow reranker call or a cold-starting LLM can silently
	// exceed that budget and get the connection killed before anything is
	// ever sent. Every failure from this point on is reported as an SSE
	// "error" event instead of an HTTP error status, since the 200 response
	// is already committed.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Answer() (retrieval, hybrid fusion, reranking, and starting the LLM
	// connection) can legitimately take tens of seconds — a cold-loading
	// local model in particular. Run it in a goroutine and send a heartbeat
	// on the SSE stream while waiting, or the proxy in front of this API
	// kills the connection well before Answer() would otherwise return.
	type answerResult struct {
		chunks []RetrievedChunk
		stream <-chan llm.StreamChunk
		err    error
	}
	answerCh := make(chan answerResult, 1)
	go func() {
		chunks, stream, err := h.service.Answer(r.Context(), RetrieveRequest{
			WorkspaceID: req.WorkspaceID,
			Query:       req.Question,
			TopK:        req.TopK,
		})
		answerCh <- answerResult{chunks: chunks, stream: stream, err: err}
	}()

	var ar answerResult
	heartbeat := time.NewTicker(sseHeartbeatInterval)
waitForAnswer:
	for {
		select {
		case ar = <-answerCh:
			break waitForAnswer
		case <-heartbeat.C:
			fmt.Fprint(w, ": keep-alive\n\n") //nolint:errcheck
			flusher.Flush()
		case <-r.Context().Done():
			heartbeat.Stop()
			return
		}
	}
	heartbeat.Stop()

	chunks, stream, err := ar.chunks, ar.stream, ar.err
	if err != nil {
		msg := httperr.Message(r, "answer failed", err, "workspace_id", req.WorkspaceID)
		writeSSEEvent(w, "error", map[string]string{"error": msg})
		flusher.Flush()
		return
	}

	if req.ConversationID != nil {
		if _, err := h.conversations.AppendMessage(r.Context(), *req.ConversationID, model.MessageRoleUser, req.Question, nil); err != nil {
			msg := httperr.Message(r, "persisting question failed", err, "conversation_id", *req.ConversationID)
			writeSSEEvent(w, "error", map[string]string{"error": msg})
			flusher.Flush()
			return
		}
	}

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

	// A select loop rather than a plain "for chunk := range stream": a gap
	// between generated tokens (e.g. a slow model under load) is exactly as
	// capable of tripping the proxy's idle-connection timeout as the wait for
	// Answer() above, so it needs the same heartbeat treatment.
	var answer strings.Builder
	tokenHeartbeat := time.NewTicker(sseHeartbeatInterval)
	defer tokenHeartbeat.Stop()
	for {
		select {
		case chunk, ok := <-stream:
			if !ok {
				return
			}
			tokenHeartbeat.Reset(sseHeartbeatInterval)
			if chunk.Error != nil {
				msg := httperr.Message(r, "completion stream error", chunk.Error, "workspace_id", req.WorkspaceID)
				writeSSEEvent(w, "error", map[string]string{"error": msg})
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
		case <-tokenHeartbeat.C:
			fmt.Fprint(w, ": keep-alive\n\n") //nolint:errcheck
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// writeSSEEvent writes a named SSE event with a JSON data payload.
func writeSSEEvent(w http.ResponseWriter, event string, data any) {
	payload, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload) //nolint:errcheck
}
