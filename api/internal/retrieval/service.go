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
	"time"

	"github.com/google/uuid"
	qdrantpb "github.com/qdrant/go-client/qdrant"

	"github.com/jpgomesr/NeuralVault/internal/embedding"
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
// the results with chunk content from Postgres.
type Retriever interface {
	Retrieve(ctx context.Context, req RetrieveRequest) ([]RetrievedChunk, error)
}

// RetrievalService is the concrete implementation of Retriever.
type RetrievalService struct {
	pool           storage.Pool
	embedder       embedding.Embedder
	vectorStore    vectorstorage.Client
	collectionName string
}

// NewRetrievalService constructs a RetrievalService.
func NewRetrievalService(pool storage.Pool, embedder embedding.Embedder, vectorStore vectorstorage.Client, collectionName string) *RetrievalService {
	return &RetrievalService{
		pool:           pool,
		embedder:       embedder,
		vectorStore:    vectorStore,
		collectionName: collectionName,
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
