package retrieval

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jpgomesr/NeuralVault/internal/logger"
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
}

// NewHandler returns a Handler backed by service.
func NewHandler(service Retriever) *Handler {
	return &Handler{service: service}
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
