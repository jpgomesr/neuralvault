package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	qdrantpb "github.com/qdrant/go-client/qdrant"

	"github.com/jpgomesr/NeuralVault/internal/chunking"
	"github.com/jpgomesr/NeuralVault/internal/embedding"
	"github.com/jpgomesr/NeuralVault/internal/model"
	"github.com/jpgomesr/NeuralVault/internal/objectstorage"
	"github.com/jpgomesr/NeuralVault/internal/sourcereader"
	"github.com/jpgomesr/NeuralVault/internal/storage"
	"github.com/jpgomesr/NeuralVault/internal/vectorstorage"
)

// FileUpload carries the content of a single uploaded file. Name is the file's
// path relative to the uploaded root (e.g. "docs/intro.md"); it is sanitized
// before use as an object-storage key or temp-file path.
type FileUpload struct {
	Name        string
	Content     io.Reader
	Size        int64
	ContentType string
}

// CreateRequest holds the non-file parameters for creating a source.
type CreateRequest struct {
	WorkspaceID uuid.UUID
	Name        string
}

// Service manages sources and drives the ingest pipeline.
type Service interface {
	// Create uploads files to object storage, persists the Source record, and
	// starts indexing in the background. Progress is streamed via ProgressBus.
	Create(ctx context.Context, req CreateRequest, files []FileUpload) (*model.Source, error)

	// Ingest re-indexes an existing source from object storage in the background.
	Ingest(ctx context.Context, sourceID uuid.UUID) error

	List(ctx context.Context, workspaceID uuid.UUID) ([]model.Source, error)
	ListChunks(ctx context.Context, sourceID uuid.UUID) ([]model.Chunk, error)
	GetByID(ctx context.Context, sourceID uuid.UUID) (*model.Source, error)

	// ListFiles returns the original files stored for a source.
	ListFiles(ctx context.Context, sourceID uuid.UUID) ([]model.SourceFile, error)

	// OpenFile streams the content of a single file (identified by its relative
	// path) from object storage, returning the reader and its content type. The
	// caller owns closing the reader.
	OpenFile(ctx context.Context, sourceID uuid.UUID, relPath string) (io.ReadCloser, string, error)
}

// SourceService is the concrete implementation of Service.
type SourceService struct {
	pool           storage.Pool
	store          objectstorage.Client
	reader         sourcereader.Reader
	chunker        *chunking.ChunkService
	bus            *ProgressBus
	embedder       embedding.Embedder
	vectorStore    vectorstorage.Client
	collectionName string
	embeddingModel string
}

// NewSourceService constructs a SourceService.
func NewSourceService(
	pool storage.Pool,
	store objectstorage.Client,
	reader sourcereader.Reader,
	chunker *chunking.ChunkService,
	bus *ProgressBus,
	embedder embedding.Embedder,
	vectorStore vectorstorage.Client,
	collectionName string,
	embeddingModel string,
) *SourceService {
	return &SourceService{
		pool:           pool,
		store:          store,
		reader:         reader,
		chunker:        chunker,
		bus:            bus,
		embedder:       embedder,
		vectorStore:    vectorStore,
		collectionName: collectionName,
		embeddingModel: embeddingModel,
	}
}

// Create uploads files to MinIO, creates the Source record, and kicks off
// background indexing. The caller receives the Source immediately (status: indexing).
func (s *SourceService) Create(ctx context.Context, req CreateRequest, files []FileUpload) (*model.Source, error) {
	tempDir, err := os.MkdirTemp("", "neuralvault-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	sourceID := uuid.New()
	sourceFiles := make([]model.SourceFile, 0, len(files))
	for _, f := range files {
		sf, err := s.storeFile(ctx, tempDir, req.WorkspaceID, sourceID, f)
		if err != nil {
			os.RemoveAll(tempDir) //nolint:errcheck
			return nil, err
		}
		sourceFiles = append(sourceFiles, sf)
	}

	minioPrefix := fmt.Sprintf("%s/%s/", req.WorkspaceID, sourceID)
	meta, err := json.Marshal(model.FileSourceMetadata{RootPath: tempDir})
	if err != nil {
		os.RemoveAll(tempDir) //nolint:errcheck
		return nil, fmt.Errorf("marshaling metadata: %w", err)
	}

	source := model.Source{
		ID:          sourceID,
		WorkspaceID: req.WorkspaceID,
		Name:        req.Name,
		Type:        model.SourceTypeFile,
		URI:         minioPrefix,
		Status:      model.SourceStatusIndexing,
		Metadata:    json.RawMessage(meta),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := s.insertSource(ctx, source); err != nil {
		os.RemoveAll(tempDir) //nolint:errcheck
		return nil, fmt.Errorf("inserting source: %w", err)
	}

	if err := s.insertSourceFiles(ctx, req.WorkspaceID, sourceFiles); err != nil {
		os.RemoveAll(tempDir) //nolint:errcheck
		return nil, fmt.Errorf("inserting source files: %w", err)
	}

	slog.InfoContext(ctx, "source created", "source_id", source.ID, "workspace_id", source.WorkspaceID)

	go s.indexInBackground(source, tempDir)

	return &source, nil
}

// Ingest re-indexes an existing source from object storage in the background.
func (s *SourceService) Ingest(ctx context.Context, sourceID uuid.UUID) error {
	source, err := s.getByID(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("loading source: %w", err)
	}

	if err := s.updateStatus(ctx, source.ID, model.SourceStatusIndexing); err != nil {
		return fmt.Errorf("updating source status: %w", err)
	}
	source.Status = model.SourceStatusIndexing

	slog.InfoContext(ctx, "ingest requested", "source_id", source.ID, "workspace_id", source.WorkspaceID)

	go s.reingestInBackground(*source)

	return nil
}

// List returns all sources for a workspace ordered by creation time descending.
func (s *SourceService) List(ctx context.Context, workspaceID uuid.UUID) ([]model.Source, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, name, type, uri, status, metadata, created_at, updated_at
		FROM sources
		WHERE workspace_id = $1
		ORDER BY created_at DESC`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying sources: %w", err)
	}
	defer rows.Close()

	var sources []model.Source
	for rows.Next() {
		var src model.Source
		var metaBytes []byte
		if err := rows.Scan(
			&src.ID, &src.WorkspaceID, &src.Name, &src.Type, &src.URI,
			&src.Status, &metaBytes, &src.CreatedAt, &src.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning source row: %w", err)
		}
		src.Metadata = json.RawMessage(metaBytes)
		sources = append(sources, src)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating source rows: %w", err)
	}
	return sources, nil
}

// ListChunks returns all chunks for a source ordered by chunk_index.
func (s *SourceService) ListChunks(ctx context.Context, sourceID uuid.UUID) ([]model.Chunk, error) {
	return s.chunker.ListChunks(ctx, sourceID)
}

// GetByID returns a single source by ID.
func (s *SourceService) GetByID(ctx context.Context, sourceID uuid.UUID) (*model.Source, error) {
	return s.getByID(ctx, sourceID)
}

// ListFiles returns the original files stored for a source, ordered by path.
func (s *SourceService) ListFiles(ctx context.Context, sourceID uuid.UUID) ([]model.SourceFile, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, source_id, name, size, content_type, created_at
		FROM source_files
		WHERE source_id = $1
		ORDER BY name`,
		sourceID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying source files: %w", err)
	}
	defer rows.Close()

	var files []model.SourceFile
	for rows.Next() {
		var f model.SourceFile
		if err := rows.Scan(&f.ID, &f.SourceID, &f.Name, &f.Size, &f.ContentType, &f.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning source file row: %w", err)
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating source file rows: %w", err)
	}
	return files, nil
}

// OpenFile streams a single file's content from object storage. relPath is the
// file's path relative to the source root; it is sanitized before use.
func (s *SourceService) OpenFile(ctx context.Context, sourceID uuid.UUID, relPath string) (io.ReadCloser, string, error) {
	rel, err := cleanRelPath(relPath)
	if err != nil {
		return nil, "", err
	}

	source, err := s.getByID(ctx, sourceID)
	if err != nil {
		return nil, "", fmt.Errorf("loading source: %w", err)
	}

	var contentType string
	err = s.pool.QueryRow(ctx,
		`SELECT content_type FROM source_files WHERE source_id = $1 AND name = $2`,
		sourceID, rel,
	).Scan(&contentType)
	if err != nil {
		return nil, "", fmt.Errorf("file %q not found for source %s: %w", rel, sourceID, err)
	}

	rc, err := s.store.Download(ctx, source.URI+rel)
	if err != nil {
		return nil, "", fmt.Errorf("downloading %q: %w", rel, err)
	}
	return rc, contentType, nil
}

// indexInBackground runs the read→chunk pipeline for a newly created source.
// It owns tempDir and removes it when done.
// A 10-minute timeout is applied so a stuck pipeline never leaks the goroutine.
func (s *SourceService) indexInBackground(source model.Source, tempDir string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	defer os.RemoveAll(tempDir) //nolint:errcheck

	start := time.Now()
	total, err := s.runPipeline(ctx, source)
	if err != nil {
		slog.ErrorContext(ctx, "indexing failed", "err", err, "source_id", source.ID, "workspace_id", source.WorkspaceID)
		if err := s.updateStatus(ctx, source.ID, model.SourceStatusError); err != nil {
			slog.ErrorContext(ctx, "failed to update source status", "err", err, "source_id", source.ID)
		}
		s.bus.publish(source.ID, ProgressEvent{Type: EventError, Error: err.Error()})
		return
	}

	if err := s.updateStatus(ctx, source.ID, model.SourceStatusIndexed); err != nil {
		slog.ErrorContext(ctx, "failed to update source status", "err", err, "source_id", source.ID)
	}
	slog.InfoContext(ctx, "indexing completed",
		"source_id", source.ID,
		"workspace_id", source.WorkspaceID,
		"chunks_total", total,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	s.bus.publish(source.ID, ProgressEvent{Type: EventDone, Total: total})
}

// reingestInBackground downloads files from object storage and re-runs the pipeline.
// A 10-minute timeout is applied so a stuck pipeline never leaks the goroutine.
func (s *SourceService) reingestInBackground(source model.Source) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	start := time.Now()

	if err := s.chunker.DeleteChunks(ctx, source.ID); err != nil {
		s.failReingest(ctx, source, err)
		return
	}

	if err := s.deleteSourceVectors(ctx, source.ID); err != nil {
		s.failReingest(ctx, source, err)
		return
	}

	tempDir, err := os.MkdirTemp("", "neuralvault-*")
	if err != nil {
		s.failReingest(ctx, source, err)
		return
	}
	defer os.RemoveAll(tempDir) //nolint:errcheck

	keys, err := s.store.ListObjects(ctx, source.URI)
	if err != nil {
		s.failReingest(ctx, source, err)
		return
	}

	for _, key := range keys {
		if err := s.downloadToTemp(ctx, key, source.URI, tempDir); err != nil {
			s.failReingest(ctx, source, err)
			return
		}
	}

	meta, _ := json.Marshal(model.FileSourceMetadata{RootPath: tempDir})
	source.Metadata = json.RawMessage(meta)

	total, err := s.runPipeline(ctx, source)
	if err != nil {
		s.failReingest(ctx, source, err)
		return
	}

	if err := s.updateStatus(ctx, source.ID, model.SourceStatusIndexed); err != nil {
		slog.ErrorContext(ctx, "failed to update source status", "err", err, "source_id", source.ID)
	}
	slog.InfoContext(ctx, "reingest completed",
		"source_id", source.ID,
		"workspace_id", source.WorkspaceID,
		"chunks_total", total,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	s.bus.publish(source.ID, ProgressEvent{Type: EventDone, Total: total})
}

// failReingest logs a reingest failure, marks the source as errored, and
// publishes the terminal error event to any subscribed status stream.
func (s *SourceService) failReingest(ctx context.Context, source model.Source, cause error) {
	slog.ErrorContext(ctx, "reingest failed", "err", cause, "source_id", source.ID, "workspace_id", source.WorkspaceID)
	if err := s.updateStatus(ctx, source.ID, model.SourceStatusError); err != nil {
		slog.ErrorContext(ctx, "failed to update source status", "err", err, "source_id", source.ID)
	}
	s.bus.publish(source.ID, ProgressEvent{Type: EventError, Error: cause.Error()})
}

// runPipeline reads source content, chunks it, generates embeddings, and upserts
// vectors into Qdrant. Publishes an EventIndexing event per file processed.
// Returns total chunks created.
func (s *SourceService) runPipeline(ctx context.Context, source model.Source) (int, error) {
	requests, err := s.reader.Read(ctx, source)
	if err != nil {
		return 0, fmt.Errorf("reading source: %w", err)
	}

	total := 0
	for _, req := range requests {
		// Offset each file's chunk indexes by the running total so chunk_index
		// stays unique within the source (constraint chunks_source_id_chunk_index_key).
		req.BaseIndex = total
		chunks, err := s.chunker.ChunkSource(ctx, req)
		if err != nil {
			return 0, fmt.Errorf("chunking %q: %w", req.FilePath, err)
		}

		if len(chunks) > 0 {
			embChunks := toEmbeddingChunks(chunks)
			embeddings, err := s.embedder.EmbedBatch(ctx, embChunks)
			if err != nil {
				return 0, fmt.Errorf("embedding chunks from %q: %w", req.FilePath, err)
			}
			if err := s.upsertChunkVectors(ctx, chunks, embeddings); err != nil {
				return 0, fmt.Errorf("upserting vectors for %q: %w", req.FilePath, err)
			}
			if err := s.updateEmbeddingModel(ctx, chunks); err != nil {
				return 0, fmt.Errorf("updating embedding model for %q: %w", req.FilePath, err)
			}
		}

		slog.DebugContext(ctx, "file processed", "source_id", source.ID, "file", req.FilePath, "chunks", len(chunks))
		s.bus.publish(source.ID, ProgressEvent{
			Type:   EventIndexing,
			File:   filepath.Base(req.FilePath),
			Chunks: len(chunks),
		})
		total += len(chunks)
	}
	return total, nil
}

// toEmbeddingChunks converts model.Chunk slice to embedding.Chunk slice.
func toEmbeddingChunks(chunks []model.Chunk) []embedding.Chunk {
	out := make([]embedding.Chunk, len(chunks))
	for i, c := range chunks {
		out[i] = embedding.Chunk{
			ID:     c.ID.String(),
			Text:   c.Content,
			Source: c.SourceID.String(),
		}
	}
	return out
}

// upsertChunkVectors validates embeddings and upserts them into Qdrant with a
// minimal payload containing only the IDs needed for workspace-scoped filtering.
func (s *SourceService) upsertChunkVectors(ctx context.Context, chunks []model.Chunk, embeddings []embedding.Embedding) error {
	if len(embeddings) != len(chunks) {
		return fmt.Errorf("embedding count mismatch: got %d embeddings for %d chunks", len(embeddings), len(chunks))
	}

	points := make([]*qdrantpb.PointStruct, len(chunks))
	for i, chunk := range chunks {
		if len(embeddings[i].Vector) == 0 {
			return fmt.Errorf("empty vector for chunk %s", chunk.ID)
		}
		if chunk.ID == uuid.Nil || chunk.WorkspaceID == uuid.Nil || chunk.SourceID == uuid.Nil {
			return fmt.Errorf("chunk %s has zero-value UUID field", chunk.ID)
		}
		points[i] = &qdrantpb.PointStruct{
			Id:      qdrantpb.NewID(chunk.ID.String()),
			Vectors: qdrantpb.NewVectors(embeddings[i].Vector...),
			Payload: qdrantpb.NewValueMap(map[string]any{
				"chunk_id":     chunk.ID.String(),
				"workspace_id": chunk.WorkspaceID.String(),
				"source_id":    chunk.SourceID.String(),
			}),
		}
	}

	if _, err := s.vectorStore.Upsert(ctx, &qdrantpb.UpsertPoints{
		CollectionName: s.collectionName,
		Points:         points,
	}); err != nil {
		return fmt.Errorf("qdrant upsert: %w", err)
	}
	return nil
}

// deleteSourceVectors removes every Qdrant point belonging to a source, matched
// by the source_id payload written in upsertChunkVectors. Used during re-ingestion
// to drop the previous generation of vectors before new chunks are upserted;
// without it, re-ingested chunks get fresh UUIDs, never collide with the old point
// IDs, and the previous generation leaks in Qdrant forever.
func (s *SourceService) deleteSourceVectors(ctx context.Context, sourceID uuid.UUID) error {
	_, err := s.vectorStore.Delete(ctx, &qdrantpb.DeletePoints{
		CollectionName: s.collectionName,
		Points: qdrantpb.NewPointsSelectorFilter(&qdrantpb.Filter{
			Must: []*qdrantpb.Condition{qdrantpb.NewMatch("source_id", sourceID.String())},
		}),
	})
	if err != nil {
		return fmt.Errorf("qdrant delete for source %s: %w", sourceID, err)
	}
	return nil
}

// updateEmbeddingModel records the model name used to generate embeddings on each chunk row.
func (s *SourceService) updateEmbeddingModel(ctx context.Context, chunks []model.Chunk) error {
	ids := make([]uuid.UUID, len(chunks))
	for i, c := range chunks {
		ids[i] = c.ID
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE chunks SET embedding_model = $1 WHERE id = ANY($2)`,
		s.embeddingModel, ids,
	)
	if err != nil {
		return fmt.Errorf("update embedding_model: %w", err)
	}
	return nil
}

// cleanRelPath normalizes an uploaded file's relative path for safe use as an
// object-storage key and temp-file path. It normalizes to forward slashes and
// rejects absolute paths and any parent-directory traversal, so a malicious
// filename can never escape the source's prefix or the temp directory.
func cleanRelPath(name string) (string, error) {
	rel := strings.TrimPrefix(filepath.ToSlash(name), "/")
	if rel == "" {
		return "", fmt.Errorf("empty file path")
	}
	cleaned := path.Clean(rel)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) {
		return "", fmt.Errorf("invalid file path %q", name)
	}
	return cleaned, nil
}

// resolveContentType prefers the client-provided content type and falls back to
// the file extension, defaulting to a generic binary type.
func resolveContentType(clientType, relPath string) string {
	if clientType != "" {
		return clientType
	}
	if byExt := mime.TypeByExtension(filepath.Ext(relPath)); byExt != "" {
		return byExt
	}
	return "application/octet-stream"
}

// storeFile writes an uploaded file to tempDir (preserving its relative path)
// and uploads it to object storage. It returns the file's metadata for
// persistence.
func (s *SourceService) storeFile(ctx context.Context, tempDir string, workspaceID, sourceID uuid.UUID, f FileUpload) (model.SourceFile, error) {
	rel, err := cleanRelPath(f.Name)
	if err != nil {
		return model.SourceFile{}, err
	}

	localPath := filepath.Join(tempDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return model.SourceFile{}, fmt.Errorf("creating dir for %q: %w", rel, err)
	}

	out, err := os.Create(localPath)
	if err != nil {
		return model.SourceFile{}, fmt.Errorf("creating temp file %q: %w", rel, err)
	}

	n, err := io.Copy(out, f.Content)
	if cerr := out.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		return model.SourceFile{}, fmt.Errorf("writing file %q: %w", rel, err)
	}

	uploadFile, err := os.Open(localPath)
	if err != nil {
		return model.SourceFile{}, fmt.Errorf("opening temp file %q: %w", rel, err)
	}
	defer uploadFile.Close() //nolint:errcheck

	key := fmt.Sprintf("%s/%s/%s", workspaceID, sourceID, rel)
	if err := s.store.Upload(ctx, key, uploadFile, n); err != nil {
		return model.SourceFile{}, fmt.Errorf("uploading %q: %w", rel, err)
	}

	return model.SourceFile{
		ID:          uuid.New(),
		SourceID:    sourceID,
		Name:        rel,
		Size:        n,
		ContentType: resolveContentType(f.ContentType, rel),
		CreatedAt:   time.Now(),
	}, nil
}

// downloadToTemp downloads a single object key into tempDir, preserving its
// path relative to prefix so nested directory structure is reconstructed.
func (s *SourceService) downloadToTemp(ctx context.Context, key, prefix, tempDir string) error {
	rc, err := s.store.Download(ctx, key)
	if err != nil {
		return fmt.Errorf("downloading %q: %w", key, err)
	}
	defer rc.Close() //nolint:errcheck

	rel := strings.TrimPrefix(key, prefix)
	localPath := filepath.Join(tempDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return fmt.Errorf("creating dir for %q: %w", key, err)
	}

	out, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("creating temp file for %q: %w", key, err)
	}
	defer out.Close() //nolint:errcheck

	if _, err := io.Copy(out, rc); err != nil {
		return fmt.Errorf("writing %q to temp: %w", key, err)
	}
	return nil
}

func (s *SourceService) insertSource(ctx context.Context, src model.Source) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sources (id, workspace_id, name, type, uri, status, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		src.ID, src.WorkspaceID, src.Name, src.Type, src.URI,
		src.Status, []byte(src.Metadata), src.CreatedAt, src.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert sources: %w", err)
	}
	return nil
}

func (s *SourceService) insertSourceFiles(ctx context.Context, workspaceID uuid.UUID, files []model.SourceFile) error {
	for _, f := range files {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO source_files (id, source_id, workspace_id, name, size, content_type, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			f.ID, f.SourceID, workspaceID, f.Name, f.Size, f.ContentType, f.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert source_files: %w", err)
		}
	}
	return nil
}

func (s *SourceService) updateStatus(ctx context.Context, id uuid.UUID, status model.SourceStatus) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sources SET status = $1, updated_at = now() WHERE id = $2`,
		status, id,
	)
	if err != nil {
		return fmt.Errorf("update source status: %w", err)
	}
	return nil
}

func (s *SourceService) getByID(ctx context.Context, id uuid.UUID) (*model.Source, error) {
	var src model.Source
	var metaBytes []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, workspace_id, name, type, uri, status, metadata, created_at, updated_at
		FROM sources WHERE id = $1`, id,
	).Scan(
		&src.ID, &src.WorkspaceID, &src.Name, &src.Type, &src.URI,
		&src.Status, &metaBytes, &src.CreatedAt, &src.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get source by id: %w", err)
	}
	src.Metadata = json.RawMessage(metaBytes)
	return &src, nil
}
