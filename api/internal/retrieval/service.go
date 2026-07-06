// Package retrieval implements the semantic search engine: it embeds a query,
// runs a workspace-scoped search in Qdrant, and hydrates the results with
// chunk content from Postgres.
package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	qdrantpb "github.com/qdrant/go-client/qdrant"

	"github.com/jpgomesr/NeuralVault/internal/embedding"
	"github.com/jpgomesr/NeuralVault/internal/llm"
	"github.com/jpgomesr/NeuralVault/internal/model"
	"github.com/jpgomesr/NeuralVault/internal/storage"
	"github.com/jpgomesr/NeuralVault/internal/vectorstorage"
)

const (
	defaultTopK = 5
	maxTopK     = 50
)

// RetrieveRequest carries the parameters for a semantic search over a workspace's chunks.
type RetrieveRequest struct {
	WorkspaceID uuid.UUID
	Query       string
	// TopK is the number of results to return. Values <= 0 fall back to
	// defaultTopK; values above maxTopK are capped.
	TopK int
}

// RetrievedChunk pairs a chunk with its similarity score from the vector search.
type RetrievedChunk struct {
	Chunk model.Chunk
	Score float32
}

// Retriever embeds a query, searches Qdrant scoped to a workspace, and hydrates
// the results with chunk content from Postgres. Answer additionally streams a
// grounded LLM completion built from the retrieved context.
type Retriever interface {
	Retrieve(ctx context.Context, req RetrieveRequest) ([]RetrievedChunk, error)
	Answer(ctx context.Context, req RetrieveRequest) ([]RetrievedChunk, <-chan llm.StreamChunk, error)
}

// RetrievalService is the concrete implementation of Retriever.
type RetrievalService struct {
	pool            storage.Pool
	embedder        embedding.Embedder
	vectorStore     vectorstorage.Client
	provider        llm.Provider
	collectionName  string
	completionModel string
}

// NewRetrievalService constructs a RetrievalService. provider and
// completionModel back the streaming Answer flow; Retrieve does not use them.
func NewRetrievalService(pool storage.Pool, embedder embedding.Embedder, vectorStore vectorstorage.Client, provider llm.Provider, collectionName, completionModel string) *RetrievalService {
	return &RetrievalService{
		pool:            pool,
		embedder:        embedder,
		vectorStore:     vectorStore,
		provider:        provider,
		collectionName:  collectionName,
		completionModel: completionModel,
	}
}

// Retrieve embeds req.Query, runs a workspace-scoped semantic search in Qdrant,
// and returns the top-k matching chunks ordered by descending similarity score.
func (s *RetrievalService) Retrieve(ctx context.Context, req RetrieveRequest) ([]RetrievedChunk, error) {
	topK := req.TopK
	if topK <= 0 {
		topK = defaultTopK
	}
	if topK > maxTopK {
		topK = maxTopK
	}

	vector, err := s.embedder.Embed(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("embedding query: %w", err)
	}

	start := time.Now()
	scoredPoints, err := s.vectorStore.Query(ctx, &qdrantpb.QueryPoints{
		CollectionName: s.collectionName,
		Query:          qdrantpb.NewQuery(vector...),
		Filter: &qdrantpb.Filter{
			Must: []*qdrantpb.Condition{qdrantpb.NewMatch("workspace_id", req.WorkspaceID.String())},
		},
		Limit:       qdrantpb.PtrOf(uint64(topK)),
		WithPayload: qdrantpb.NewWithPayload(true),
	})
	if err != nil {
		slog.ErrorContext(ctx, "qdrant query failed", "err", err, "workspace_id", req.WorkspaceID)
		return nil, fmt.Errorf("qdrant query: %w", err)
	}
	slog.DebugContext(ctx, "qdrant query completed",
		"workspace_id", req.WorkspaceID,
		"top_k", topK,
		"result_count", len(scoredPoints),
		"duration_ms", time.Since(start).Milliseconds(),
	)
	if len(scoredPoints) == 0 {
		slog.InfoContext(ctx, "no matches found", "workspace_id", req.WorkspaceID)
		return []RetrievedChunk{}, nil
	}

	ids := make([]uuid.UUID, 0, len(scoredPoints))
	scores := make(map[uuid.UUID]float32, len(scoredPoints))
	rank := make(map[uuid.UUID]int, len(scoredPoints))
	for i, p := range scoredPoints {
		rawID := p.GetId().GetUuid()
		id, err := uuid.Parse(rawID)
		if err != nil {
			return nil, fmt.Errorf("parsing point id %q: %w", rawID, err)
		}
		ids = append(ids, id)
		scores[id] = p.GetScore()
		rank[id] = i
	}

	chunks, err := s.loadChunks(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("loading chunks: %w", err)
	}

	// Filtering on WorkspaceID again is defense-in-depth: it should already be
	// guaranteed by the Qdrant filter above, but a chunk row is never returned
	// to a caller outside its workspace even if that filter were ever wrong.
	results := make([]RetrievedChunk, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk.WorkspaceID != req.WorkspaceID {
			continue
		}
		results = append(results, RetrievedChunk{Chunk: chunk, Score: scores[chunk.ID]})
	}
	sort.Slice(results, func(i, j int) bool {
		return rank[results[i].Chunk.ID] < rank[results[j].Chunk.ID]
	})

	return results, nil
}

// systemPrompt frames the assistant so it answers strictly from the retrieved
// context and admits when the context is insufficient, rather than inventing.
const systemPrompt = "You are NeuralVault's assistant. Answer the user's question using ONLY the provided context. " +
	"If the context does not contain the answer, say you don't know. Be concise and cite nothing outside the context."

// Answer runs retrieval for req, then streams a grounded LLM completion built
// from the retrieved chunks. It returns the chunks (so the caller can surface
// sources immediately) alongside a channel of incremental completion chunks;
// the channel is closed once the model finishes or emits an error.
func (s *RetrievalService) Answer(ctx context.Context, req RetrieveRequest) ([]RetrievedChunk, <-chan llm.StreamChunk, error) {
	chunks, err := s.Retrieve(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("retrieving context: %w", err)
	}

	stream, err := s.provider.Stream(ctx, llm.CompletionRequest{
		Messages: buildMessages(req.Query, chunks),
		Model:    s.completionModel,
	})
	if err != nil {
		return chunks, nil, fmt.Errorf("starting completion stream: %w", err)
	}
	return chunks, stream, nil
}

// buildMessages assembles the RAG prompt: a system instruction plus a user
// message carrying the numbered context chunks and the question.
func buildMessages(question string, chunks []RetrievedChunk) []llm.Message {
	var b strings.Builder
	b.WriteString("Context:\n")
	if len(chunks) == 0 {
		b.WriteString("(no relevant context found)\n")
	}
	for i, c := range chunks {
		fmt.Fprintf(&b, "[%d] %s\n", i+1, c.Chunk.Content)
	}
	fmt.Fprintf(&b, "\nQuestion: %s", question)

	return []llm.Message{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: b.String()},
	}
}

// loadChunks batch-fetches chunks by ID. Row order is not guaranteed to match
// ids; callers must re-sort using their own key (e.g. Qdrant score order).
func (s *RetrievalService) loadChunks(ctx context.Context, ids []uuid.UUID) ([]model.Chunk, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, source_id, workspace_id, content, chunk_index, metadata, embedding_model, created_at
		FROM chunks
		WHERE id = ANY($1)`,
		ids,
	)
	if err != nil {
		slog.ErrorContext(ctx, "loading chunks failed", "err", err)
		return nil, fmt.Errorf("querying chunks: %w", err)
	}
	defer rows.Close()

	var chunks []model.Chunk
	for rows.Next() {
		var ch model.Chunk
		var metaBytes []byte
		if err := rows.Scan(
			&ch.ID, &ch.SourceID, &ch.WorkspaceID,
			&ch.Content, &ch.ChunkIndex,
			&metaBytes, &ch.EmbeddingModel, &ch.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning chunk row: %w", err)
		}
		ch.Metadata = json.RawMessage(metaBytes)
		chunks = append(chunks, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating chunk rows: %w", err)
	}
	return chunks, nil
}
