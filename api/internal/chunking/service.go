package chunking

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jpgomesr/NeuralVault/internal/model"
	"github.com/jpgomesr/NeuralVault/internal/storage"
)

// ContentType identifies the format of content to be split.
type ContentType string

const (
	ContentTypeMarkdown  ContentType = "markdown"
	ContentTypePlaintext ContentType = "plaintext"
)

// ChunkRequest carries the parameters for a single ChunkSource call.
type ChunkRequest struct {
	SourceID    uuid.UUID
	WorkspaceID uuid.UUID
	Content     string
	ContentType ContentType
	FilePath    string // used to populate FileChunkMetadata
	PageNumber  int    // used to populate FileChunkMetadata (PDF pages)
	BaseIndex   int    // offset added to per-request chunk indexes so chunk_index stays unique across a multi-file source
}

// ChunkService is the concrete implementation of Service.
type ChunkService struct {
	pool      storage.Pool
	splitters map[ContentType]Splitter
}

// NewChunkService constructs a ChunkService.
func NewChunkService(pool storage.Pool, splitters map[ContentType]Splitter) *ChunkService {
	return &ChunkService{pool: pool, splitters: splitters}
}

// ChunkSource splits the request's content and persists the resulting chunks in
// a single database transaction. Returns the persisted chunks on success.
func (s *ChunkService) ChunkSource(ctx context.Context, req ChunkRequest) ([]model.Chunk, error) {
	sp, ok := s.splitters[req.ContentType]
	if !ok {
		return nil, fmt.Errorf("unsupported content type: %q", req.ContentType)
	}

	spans, err := sp.Split(ctx, req.Content)
	if err != nil {
		return nil, fmt.Errorf("splitting content: %w", err)
	}

	chunks := make([]model.Chunk, 0, len(spans))
	for i, span := range spans {
		meta, err := buildMetadata(req, span)
		if err != nil {
			return nil, fmt.Errorf("building metadata for chunk %d: %w", i, err)
		}
		chunks = append(chunks, model.Chunk{
			ID:             uuid.New(),
			SourceID:       req.SourceID,
			WorkspaceID:    req.WorkspaceID,
			Content:        span.Content,
			ChunkIndex:     req.BaseIndex + i,
			Metadata:       meta,
			EmbeddingModel: "",
		})
	}

	start := time.Now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "chunk persist failed", "err", err, "source_id", req.SourceID)
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	const q = `
		INSERT INTO chunks (id, source_id, workspace_id, content, chunk_index, metadata, embedding_model)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`

	for _, ch := range chunks {
		if _, err := tx.Exec(ctx, q,
			ch.ID, ch.SourceID, ch.WorkspaceID,
			ch.Content, ch.ChunkIndex, []byte(ch.Metadata), ch.EmbeddingModel,
		); err != nil {
			slog.ErrorContext(ctx, "chunk persist failed", "err", err, "source_id", req.SourceID)
			return nil, fmt.Errorf("inserting chunk %d: %w", ch.ChunkIndex, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		slog.ErrorContext(ctx, "chunk persist failed", "err", err, "source_id", req.SourceID)
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	slog.DebugContext(ctx, "chunks persisted",
		"source_id", req.SourceID,
		"chunk_count", len(chunks),
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return chunks, nil
}

// ListChunks returns all chunks for a source ordered by chunk_index.
func (s *ChunkService) ListChunks(ctx context.Context, sourceID uuid.UUID) ([]model.Chunk, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, source_id, workspace_id, content, chunk_index, metadata, embedding_model, created_at
		FROM chunks
		WHERE source_id = $1
		ORDER BY chunk_index`,
		sourceID,
	)
	if err != nil {
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
			&metaBytes,
			&ch.EmbeddingModel, &ch.CreatedAt,
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

// DeleteChunks removes all chunks belonging to the given source.
func (s *ChunkService) DeleteChunks(ctx context.Context, sourceID uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM chunks WHERE source_id = $1`, sourceID); err != nil {
		slog.ErrorContext(ctx, "delete chunks failed", "err", err, "source_id", sourceID)
		return fmt.Errorf("deleting chunks for source %s: %w", sourceID, err)
	}
	return nil
}

func buildMetadata(req ChunkRequest, span Span) (json.RawMessage, error) {
	meta := model.FileChunkMetadata{
		FilePath:  req.FilePath,
		Page:      req.PageNumber,
		Heading:   span.Heading,
		Level:     span.Level,
		StartLine: span.StartLine,
		EndLine:   span.EndLine,
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}
