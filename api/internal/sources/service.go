package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jpgomesr/NeuralVault/internal/chunking"
	"github.com/jpgomesr/NeuralVault/internal/model"
	"github.com/jpgomesr/NeuralVault/internal/objectstorage"
	"github.com/jpgomesr/NeuralVault/internal/sourcereader"
	"github.com/jpgomesr/NeuralVault/internal/storage"
)

// FileUpload carries the content of a single uploaded file.
type FileUpload struct {
	Name    string
	Content io.Reader
	Size    int64
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
}

// SourceService is the concrete implementation of Service.
type SourceService struct {
	pool    storage.Pool
	store   objectstorage.Client
	reader  sourcereader.Reader
	chunker *chunking.ChunkService
	bus     *ProgressBus
}

// NewSourceService constructs a SourceService.
func NewSourceService(
	pool storage.Pool,
	store objectstorage.Client,
	reader sourcereader.Reader,
	chunker *chunking.ChunkService,
	bus *ProgressBus,
) *SourceService {
	return &SourceService{
		pool:    pool,
		store:   store,
		reader:  reader,
		chunker: chunker,
		bus:     bus,
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
	for _, f := range files {
		if err := s.storeFile(ctx, tempDir, req.WorkspaceID, sourceID, f); err != nil {
			os.RemoveAll(tempDir) //nolint:errcheck
			return nil, err
		}
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

// indexInBackground runs the read→chunk pipeline for a newly created source.
// It owns tempDir and removes it when done.
// A 10-minute timeout is applied so a stuck pipeline never leaks the goroutine.
func (s *SourceService) indexInBackground(source model.Source, tempDir string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	defer os.RemoveAll(tempDir) //nolint:errcheck

	total, err := s.runPipeline(ctx, source)
	if err != nil {
		_ = s.updateStatus(ctx, source.ID, model.SourceStatusError)
		s.bus.publish(source.ID, ProgressEvent{Type: EventError, Error: err.Error()})
		return
	}

	_ = s.updateStatus(ctx, source.ID, model.SourceStatusIndexed)
	s.bus.publish(source.ID, ProgressEvent{Type: EventDone, Total: total})
}

// reingestInBackground downloads files from object storage and re-runs the pipeline.
// A 10-minute timeout is applied so a stuck pipeline never leaks the goroutine.
func (s *SourceService) reingestInBackground(source model.Source) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if err := s.chunker.DeleteChunks(ctx, source.ID); err != nil {
		_ = s.updateStatus(ctx, source.ID, model.SourceStatusError)
		s.bus.publish(source.ID, ProgressEvent{Type: EventError, Error: err.Error()})
		return
	}

	tempDir, err := os.MkdirTemp("", "neuralvault-*")
	if err != nil {
		_ = s.updateStatus(ctx, source.ID, model.SourceStatusError)
		s.bus.publish(source.ID, ProgressEvent{Type: EventError, Error: err.Error()})
		return
	}
	defer os.RemoveAll(tempDir) //nolint:errcheck

	keys, err := s.store.ListObjects(ctx, source.URI)
	if err != nil {
		_ = s.updateStatus(ctx, source.ID, model.SourceStatusError)
		s.bus.publish(source.ID, ProgressEvent{Type: EventError, Error: err.Error()})
		return
	}

	for _, key := range keys {
		if err := s.downloadToTemp(ctx, key, tempDir); err != nil {
			_ = s.updateStatus(ctx, source.ID, model.SourceStatusError)
			s.bus.publish(source.ID, ProgressEvent{Type: EventError, Error: err.Error()})
			return
		}
	}

	meta, _ := json.Marshal(model.FileSourceMetadata{RootPath: tempDir})
	source.Metadata = json.RawMessage(meta)

	total, err := s.runPipeline(ctx, source)
	if err != nil {
		_ = s.updateStatus(ctx, source.ID, model.SourceStatusError)
		s.bus.publish(source.ID, ProgressEvent{Type: EventError, Error: err.Error()})
		return
	}

	_ = s.updateStatus(ctx, source.ID, model.SourceStatusIndexed)
	s.bus.publish(source.ID, ProgressEvent{Type: EventDone, Total: total})
}

// runPipeline reads source content and persists chunks, publishing an EventIndexing
// event per file. Returns total chunks created.
func (s *SourceService) runPipeline(ctx context.Context, source model.Source) (int, error) {
	requests, err := s.reader.Read(ctx, source)
	if err != nil {
		return 0, fmt.Errorf("reading source: %w", err)
	}

	total := 0
	for _, req := range requests {
		chunks, err := s.chunker.ChunkSource(ctx, req)
		if err != nil {
			return 0, fmt.Errorf("chunking %q: %w", req.FilePath, err)
		}
		s.bus.publish(source.ID, ProgressEvent{
			Type:   EventIndexing,
			File:   filepath.Base(req.FilePath),
			Chunks: len(chunks),
		})
		total += len(chunks)
	}
	return total, nil
}

// storeFile writes an uploaded file to tempDir and uploads it to object storage.
func (s *SourceService) storeFile(ctx context.Context, tempDir string, workspaceID, sourceID uuid.UUID, f FileUpload) error {
	localPath := filepath.Join(tempDir, filepath.Base(f.Name))

	out, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("creating temp file %q: %w", f.Name, err)
	}

	n, err := io.Copy(out, f.Content)
	if cerr := out.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("writing file %q: %w", f.Name, err)
	}

	uploadFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("opening temp file %q: %w", f.Name, err)
	}
	defer uploadFile.Close() //nolint:errcheck

	key := fmt.Sprintf("%s/%s/%s", workspaceID, sourceID, filepath.Base(f.Name))
	if err := s.store.Upload(ctx, key, uploadFile, n); err != nil {
		return fmt.Errorf("uploading %q: %w", f.Name, err)
	}
	return nil
}

// downloadToTemp downloads a single object key into tempDir preserving the filename.
func (s *SourceService) downloadToTemp(ctx context.Context, key, tempDir string) error {
	rc, err := s.store.Download(ctx, key)
	if err != nil {
		return fmt.Errorf("downloading %q: %w", key, err)
	}
	defer rc.Close() //nolint:errcheck

	localPath := filepath.Join(tempDir, filepath.Base(key))
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
