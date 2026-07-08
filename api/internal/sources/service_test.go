package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	qdrantpb "github.com/qdrant/go-client/qdrant"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/jpgomesr/NeuralVault/internal/chunking"
	"github.com/jpgomesr/NeuralVault/internal/chunking/markdown"
	"github.com/jpgomesr/NeuralVault/internal/chunking/text"
	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/embedding"
	"github.com/jpgomesr/NeuralVault/internal/model"
	"github.com/jpgomesr/NeuralVault/internal/objectstorage"
	minioclient "github.com/jpgomesr/NeuralVault/internal/objectstorage/minio"
	"github.com/jpgomesr/NeuralVault/internal/sourcereader"
	"github.com/jpgomesr/NeuralVault/internal/storage"
	pgstore "github.com/jpgomesr/NeuralVault/internal/storage/postgres"
	"github.com/jpgomesr/NeuralVault/internal/vectorstorage"
)

// stubEmbedder returns zero-valued vectors of a fixed size.
// It satisfies embedding.Embedder without requiring a running Ollama instance.
type stubEmbedder struct{ dim int }

func (s *stubEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, s.dim), nil
}

func (s *stubEmbedder) EmbedBatch(_ context.Context, chunks []embedding.Chunk) ([]embedding.Embedding, error) {
	out := make([]embedding.Embedding, len(chunks))
	for i, c := range chunks {
		out[i] = embedding.Embedding{ChunkID: c.ID, Vector: make([]float32, s.dim)}
	}
	return out, nil
}

func (s *stubEmbedder) HealthCheck(_ context.Context) error { return nil }

// stubVectorStore discards all writes and returns no-op results.
// It satisfies vectorstorage.Client without requiring a running Qdrant instance.
type stubVectorStore struct{}

func (stubVectorStore) HealthCheck(_ context.Context) (*qdrantpb.HealthCheckReply, error) {
	return &qdrantpb.HealthCheckReply{}, nil
}
func (stubVectorStore) CollectionExists(_ context.Context, _ string) (bool, error) { return true, nil }
func (stubVectorStore) CreateCollection(_ context.Context, _ *qdrantpb.CreateCollection) error {
	return nil
}
func (stubVectorStore) DeleteCollection(_ context.Context, _ string) error { return nil }
func (stubVectorStore) Upsert(_ context.Context, _ *qdrantpb.UpsertPoints) (*qdrantpb.UpdateResult, error) {
	return &qdrantpb.UpdateResult{}, nil
}
func (stubVectorStore) Query(_ context.Context, _ *qdrantpb.QueryPoints) ([]*qdrantpb.ScoredPoint, error) {
	return nil, nil
}
func (stubVectorStore) Delete(_ context.Context, _ *qdrantpb.DeletePoints) (*qdrantpb.UpdateResult, error) {
	return &qdrantpb.UpdateResult{}, nil
}
func (stubVectorStore) Count(_ context.Context, _ *qdrantpb.CountPoints) (uint64, error) {
	return 0, nil
}
func (stubVectorStore) Close() error { return nil }

// failingEmbedder always returns an error from both Embed and EmbedBatch.
type failingEmbedder struct{}

func (failingEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, errors.New("embed failed")
}
func (failingEmbedder) EmbedBatch(_ context.Context, _ []embedding.Chunk) ([]embedding.Embedding, error) {
	return nil, errors.New("embed batch failed")
}
func (failingEmbedder) HealthCheck(_ context.Context) error { return nil }

// errorVectorStore overrides Upsert to return an error; all other methods
// are inherited from stubVectorStore.
type errorVectorStore struct{ stubVectorStore }

func (errorVectorStore) Upsert(_ context.Context, _ *qdrantpb.UpsertPoints) (*qdrantpb.UpdateResult, error) {
	return nil, errors.New("qdrant upsert failed")
}

// deleteErrorVectorStore overrides Delete to return an error; all other methods
// are inherited from stubVectorStore.
type deleteErrorVectorStore struct{ stubVectorStore }

func (deleteErrorVectorStore) Delete(_ context.Context, _ *qdrantpb.DeletePoints) (*qdrantpb.UpdateResult, error) {
	return nil, errors.New("qdrant delete failed")
}

// spyVectorStore records every DeletePoints request it receives so tests can
// assert that re-ingestion drops the previous generation of vectors. All other
// methods are inherited from stubVectorStore. Use as a pointer.
type spyVectorStore struct {
	stubVectorStore
	mu         sync.Mutex
	deleteReqs []*qdrantpb.DeletePoints
}

func (s *spyVectorStore) Delete(_ context.Context, req *qdrantpb.DeletePoints) (*qdrantpb.UpdateResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteReqs = append(s.deleteReqs, req)
	return &qdrantpb.UpdateResult{}, nil
}

func (s *spyVectorStore) deletes() []*qdrantpb.DeletePoints {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*qdrantpb.DeletePoints(nil), s.deleteReqs...)
}

// selectiveFailingPool wraps a real storage.Pool and intercepts Exec calls
// whose SQL contains "embedding_model", returning an injected error. All other
// methods delegate to the embedded pool unchanged.
type selectiveFailingPool struct{ storage.Pool }

func (p selectiveFailingPool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "embedding_model") {
		return pgconn.CommandTag{}, fmt.Errorf("injected exec failure for embedding_model")
	}
	return p.Pool.Exec(ctx, sql, args...)
}

// sourceFilesFailingPool fails Exec calls that touch source_files, so tests can
// exercise the metadata-insert failure path. Other statements delegate normally.
type sourceFilesFailingPool struct{ storage.Pool }

func (p sourceFilesFailingPool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "source_files") {
		return pgconn.CommandTag{}, fmt.Errorf("injected source_files failure")
	}
	return p.Pool.Exec(ctx, sql, args...)
}

// sourceFilesQueryFailingPool fails Query calls that touch source_files, so the
// ListFiles query-error path can be exercised.
type sourceFilesQueryFailingPool struct{ storage.Pool }

func (p sourceFilesQueryFailingPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "source_files") {
		return nil, fmt.Errorf("injected source_files query failure")
	}
	return p.Pool.Query(ctx, sql, args...)
}

// errReader always fails on Read, used to exercise copy-error branches.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read boom") }

// fakeStore is an in-memory objectstorage.Client with optional error injection,
// so storage-layer failures can be exercised without a MinIO container.
type fakeStore struct {
	mu              sync.Mutex
	objects         map[string][]byte
	uploadErr       error
	downloadErr     error
	downloadReadErr bool
	listErr         error
}

func newFakeStore() *fakeStore { return &fakeStore{objects: map[string][]byte{}} }

func (f *fakeStore) Upload(_ context.Context, key string, r io.Reader, _ int64) error {
	if f.uploadErr != nil {
		return f.uploadErr
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = b
	return nil
}

func (f *fakeStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	if f.downloadErr != nil {
		return nil, f.downloadErr
	}
	if f.downloadReadErr {
		return io.NopCloser(errReader{}), nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objects[key]
	if !ok {
		return nil, fmt.Errorf("object not found: %s", key)
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (f *fakeStore) ListObjects(_ context.Context, prefix string) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []string
	for k := range f.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (f *fakeStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, key)
	return nil
}

func (f *fakeStore) HealthCheck(_ context.Context) error { return nil }

// buildSvcWithStore builds a SourceService with a custom pool and object store,
// a real chunker on sharedPool, and no-op embedder/vector store.
func buildSvcWithStore(_ context.Context, t *testing.T, pool storage.Pool, store objectstorage.Client) *SourceService {
	t.Helper()
	splitters := map[chunking.ContentType]chunking.Splitter{
		chunking.ContentTypeMarkdown:  markdown.New(),
		chunking.ContentTypePlaintext: text.New(),
	}
	return NewSourceService(
		pool,
		store,
		sourcereader.NewFileReader(),
		chunking.NewChunkService(sharedPool, splitters),
		NewProgressBus(),
		&stubEmbedder{dim: 768},
		stubVectorStore{},
		"test",
		"nomic-embed-text",
	)
}

var (
	sharedPool     *pgxpool.Pool
	sharedMinioCfg *config.Config
)

func TestMain(m *testing.M) {
	os.Exit(runAllTests(m))
}

func runAllTests(m *testing.M) int {
	ctx := context.Background()

	// ── Postgres ──────────────────────────────────────────────────────────────
	pgCtr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:17",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_USER":     "neuralvault",
				"POSTGRES_PASSWORD": "neuralvault",
				"POSTGRES_DB":       "neuralvault",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		},
		Started: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "start postgres: %v\n", err)
		return 1
	}
	defer func() { _ = pgCtr.Terminate(ctx) }()

	pgHost, err := pgCtr.Host(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres host: %v\n", err)
		return 1
	}
	pgPort, err := pgCtr.MappedPort(ctx, "5432")
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres port: %v\n", err)
		return 1
	}

	sharedPool, err = pgstore.NewPool(ctx, config.Config{
		Postgres: config.Postgres{
			Host:     pgHost,
			Port:     int(pgPort.Num()),
			Username: "neuralvault",
			Password: "neuralvault",
			Name:     "neuralvault",
			SSLMode:  "disable",
			MaxConns: 10,
			MinConns: 1,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "create pool: %v\n", err)
		return 1
	}
	defer sharedPool.Close()

	// Run migrations via goose so the sources/chunks tables exist.
	sqlDB := stdlib.OpenDBFromPool(sharedPool)
	defer sqlDB.Close() //nolint:errcheck

	if err := goose.SetDialect("postgres"); err != nil {
		fmt.Fprintf(os.Stderr, "goose dialect: %v\n", err)
		return 1
	}
	wd, _ := os.Getwd()
	migrationsDir := filepath.Join(wd, "../storage/postgres/migrations")
	if err := goose.Up(sqlDB, migrationsDir); err != nil {
		fmt.Fprintf(os.Stderr, "goose up: %v\n", err)
		return 1
	}

	// ── MinIO ─────────────────────────────────────────────────────────────────
	minioCtr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "minio/minio:latest",
			ExposedPorts: []string{"9000/tcp"},
			Env: map[string]string{
				"MINIO_ROOT_USER":     "minioadmin",
				"MINIO_ROOT_PASSWORD": "minioadmin",
			},
			Cmd:        []string{"server", "/data"},
			WaitingFor: wait.ForHTTP("/minio/health/live").WithPort("9000/tcp"),
		},
		Started: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "start minio: %v\n", err)
		return 1
	}
	defer func() { _ = minioCtr.Terminate(ctx) }()

	minioHost, err := minioCtr.Host(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "minio host: %v\n", err)
		return 1
	}
	minioPort, err := minioCtr.MappedPort(ctx, "9000")
	if err != nil {
		fmt.Fprintf(os.Stderr, "minio port: %v\n", err)
		return 1
	}

	sharedMinioCfg = &config.Config{
		MinIO: config.MinIO{
			Endpoint:  fmt.Sprintf("%s:%d", minioHost, minioPort.Num()),
			AccessKey: "minioadmin",
			SecretKey: "minioadmin",
			Bucket:    "neuralvault-svc-test",
			UseSSL:    false,
		},
	}

	return m.Run()
}

// newSvc builds a SourceService wired to the shared containers with a no-op
// vector store.
func newSvc(ctx context.Context, t *testing.T) *SourceService {
	t.Helper()
	return newSvcVS(ctx, t, stubVectorStore{})
}

// newSvcVS is newSvc with an injectable vector store, so tests can observe
// Qdrant writes (e.g. spyVectorStore).
func newSvcVS(ctx context.Context, t *testing.T, vs vectorstorage.Client) *SourceService {
	t.Helper()
	store, err := minioclient.New(ctx, sharedMinioCfg)
	if err != nil {
		t.Fatalf("minio client: %v", err)
	}
	splitters := map[chunking.ContentType]chunking.Splitter{
		chunking.ContentTypeMarkdown:  markdown.New(),
		chunking.ContentTypePlaintext: text.New(),
	}
	return NewSourceService(
		sharedPool,
		store,
		sourcereader.NewFileReader(),
		chunking.NewChunkService(sharedPool, splitters),
		NewProgressBus(),
		&stubEmbedder{dim: 768},
		vs,
		"test",
		"nomic-embed-text",
	)
}

// insertWS inserts a workspace row and schedules its deletion on test cleanup.
func insertWS(ctx context.Context, t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := sharedPool.Exec(ctx, "INSERT INTO workspace (id, name) VALUES ($1, $2)", id, "test"); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = sharedPool.Exec(context.Background(), "DELETE FROM workspace WHERE id = $1", id)
	})
	return id
}

// awaitIndexed polls GetByID until the source reaches indexed status (30s timeout).
func awaitIndexed(ctx context.Context, t *testing.T, svc *SourceService, id uuid.UUID) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		src, err := svc.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("GetByID while polling: %v", err)
		}
		switch src.Status {
		case model.SourceStatusIndexed:
			return
		case model.SourceStatusError:
			t.Fatalf("source %s entered error status while waiting for indexed", id)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for source %s to be indexed", id)
}

// awaitError polls GetByID until the source reaches error status (30s timeout).
func awaitError(ctx context.Context, t *testing.T, svc *SourceService, id uuid.UUID) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		src, err := svc.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("GetByID while polling: %v", err)
		}
		if src.Status == model.SourceStatusError {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for source %s to reach error status", id)
}

// ── Service tests ─────────────────────────────────────────────────────────────

func TestServiceCreate(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(ctx, t)
	wid := insertWS(ctx, t)

	const content = "# Hello\nContent here."
	src, err := svc.Create(ctx, CreateRequest{WorkspaceID: wid, Name: "vault"}, []FileUpload{
		{Name: "notes.md", Content: bytes.NewBufferString(content), Size: int64(len(content))},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if src.Status != model.SourceStatusIndexing {
		t.Errorf("expected status=indexing immediately after Create, got %s", src.Status)
	}
	if src.WorkspaceID != wid {
		t.Errorf("workspace mismatch: want %s, got %s", wid, src.WorkspaceID)
	}

	awaitIndexed(ctx, t, svc, src.ID)
}

func TestServiceFiles_RoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(ctx, t)
	wid := insertWS(ctx, t)

	const rootContent = "# Root doc"
	const nestedContent = "nested plain text"
	src, err := svc.Create(ctx, CreateRequest{WorkspaceID: wid, Name: "vault"}, []FileUpload{
		{Name: "readme.md", Content: bytes.NewBufferString(rootContent), Size: int64(len(rootContent)), ContentType: "text/markdown"},
		{Name: "docs/guide/intro.txt", Content: bytes.NewBufferString(nestedContent), Size: int64(len(nestedContent))},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	awaitIndexed(ctx, t, svc, src.ID)

	files, err := svc.ListFiles(ctx, src.ID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %+v", len(files), files)
	}
	// Ordered by name: "docs/guide/intro.txt" before "readme.md".
	if files[0].Name != "docs/guide/intro.txt" {
		t.Errorf("expected nested path preserved, got %q", files[0].Name)
	}
	if files[0].Size != int64(len(nestedContent)) {
		t.Errorf("nested file size mismatch: want %d, got %d", len(nestedContent), files[0].Size)
	}
	if files[1].Name != "readme.md" || files[1].ContentType != "text/markdown" {
		t.Errorf("unexpected root file metadata: %+v", files[1])
	}

	// OpenFile streams the nested file's original bytes.
	rc, ct, err := svc.OpenFile(ctx, src.ID, "docs/guide/intro.txt")
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer rc.Close() //nolint:errcheck
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read content: %v", err)
	}
	if string(got) != nestedContent {
		t.Errorf("content mismatch: want %q, got %q", nestedContent, string(got))
	}
	if ct == "" {
		t.Error("expected a content type from OpenFile")
	}

	// Path traversal is rejected.
	if _, _, err := svc.OpenFile(ctx, src.ID, "../escape"); err == nil {
		t.Error("expected OpenFile to reject traversal path")
	}
}

func TestServiceStoreFile_UploadError(t *testing.T) {
	ctx := context.Background()
	wid := insertWS(ctx, t)
	store := newFakeStore()
	store.uploadErr = errors.New("upload boom")
	svc := buildSvcWithStore(ctx, t, sharedPool, store)

	_, err := svc.Create(ctx, CreateRequest{WorkspaceID: wid, Name: "x"}, []FileUpload{
		{Name: "a.md", Content: bytes.NewBufferString("hi"), Size: 2},
	})
	if err == nil {
		t.Fatal("expected an upload error from Create")
	}
}

func TestServiceStoreFile_WriteError(t *testing.T) {
	ctx := context.Background()
	wid := insertWS(ctx, t)
	svc := buildSvcWithStore(ctx, t, sharedPool, newFakeStore())

	_, err := svc.Create(ctx, CreateRequest{WorkspaceID: wid, Name: "x"}, []FileUpload{
		{Name: "a.md", Content: errReader{}, Size: 2},
	})
	if err == nil {
		t.Fatal("expected a write error from Create")
	}
}

func TestServiceStoreFile_MkdirError(t *testing.T) {
	ctx := context.Background()
	wid := insertWS(ctx, t)
	svc := buildSvcWithStore(ctx, t, sharedPool, newFakeStore())

	// "a" is written as a file, then "a/b.md" needs "a" to be a directory —
	// MkdirAll fails because a file already occupies that path.
	_, err := svc.Create(ctx, CreateRequest{WorkspaceID: wid, Name: "x"}, []FileUpload{
		{Name: "a", Content: bytes.NewBufferString("file"), Size: 4},
		{Name: "a/b.md", Content: bytes.NewBufferString("nested"), Size: 6},
	})
	if err == nil {
		t.Fatal("expected a mkdir error from conflicting file/dir paths")
	}
}

func TestServiceListFiles_QueryError(t *testing.T) {
	ctx := context.Background()
	svc := buildSvcWithStore(ctx, t, sourceFilesQueryFailingPool{Pool: sharedPool}, newFakeStore())

	if _, err := svc.ListFiles(ctx, uuid.New()); err == nil {
		t.Fatal("expected a query error from ListFiles")
	}
}

func TestServiceReingest_DownloadReadError(t *testing.T) {
	ctx := context.Background()
	wid := insertWS(ctx, t)
	store := newFakeStore()
	svc := buildSvcWithStore(ctx, t, sharedPool, store)

	src, err := svc.Create(ctx, CreateRequest{WorkspaceID: wid, Name: "x"}, []FileUpload{
		{Name: "a.md", Content: bytes.NewBufferString("hi"), Size: 2},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	awaitIndexed(ctx, t, svc, src.ID)

	// The object streams but fails mid-read, so copying it to temp errors.
	store.downloadReadErr = true
	if err := svc.Ingest(ctx, src.ID); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	awaitError(ctx, t, svc, src.ID)
}

func TestServiceCreate_InsertFilesFailure(t *testing.T) {
	ctx := context.Background()
	wid := insertWS(ctx, t)
	svc := buildSvcWithStore(ctx, t, sourceFilesFailingPool{Pool: sharedPool}, newFakeStore())

	_, err := svc.Create(ctx, CreateRequest{WorkspaceID: wid, Name: "x"}, []FileUpload{
		{Name: "a.md", Content: bytes.NewBufferString("hi"), Size: 2},
	})
	if err == nil {
		t.Fatal("expected a source_files insert failure from Create")
	}
}

func TestServiceOpenFile_Errors(t *testing.T) {
	ctx := context.Background()
	wid := insertWS(ctx, t)
	svc := buildSvcWithStore(ctx, t, sharedPool, newFakeStore())

	src, err := svc.Create(ctx, CreateRequest{WorkspaceID: wid, Name: "x"}, []FileUpload{
		{Name: "a.md", Content: bytes.NewBufferString("hi"), Size: 2, ContentType: "text/markdown"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	awaitIndexed(ctx, t, svc, src.ID)

	// A file name that isn't recorded for the source has no metadata row.
	if _, _, err := svc.OpenFile(ctx, src.ID, "missing.md"); err == nil {
		t.Error("expected error opening an unknown file")
	}
	// An unknown source fails at GetByID.
	if _, _, err := svc.OpenFile(ctx, uuid.New(), "a.md"); err == nil {
		t.Error("expected error opening a file for an unknown source")
	}
}

func TestServiceReingest_DownloadError(t *testing.T) {
	ctx := context.Background()
	wid := insertWS(ctx, t)
	store := newFakeStore()
	svc := buildSvcWithStore(ctx, t, sharedPool, store)

	src, err := svc.Create(ctx, CreateRequest{WorkspaceID: wid, Name: "x"}, []FileUpload{
		{Name: "a.md", Content: bytes.NewBufferString("hi"), Size: 2},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	awaitIndexed(ctx, t, svc, src.ID)

	// Re-ingest now fails while downloading objects back from storage.
	store.downloadErr = errors.New("download boom")
	if err := svc.Ingest(ctx, src.ID); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	awaitError(ctx, t, svc, src.ID)
}

func TestServiceCreate_BadWorkspace(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(ctx, t)

	_, err := svc.Create(ctx, CreateRequest{WorkspaceID: uuid.New(), Name: "bad"}, []FileUpload{
		{Name: "f.md", Content: bytes.NewBufferString("# Hi"), Size: 4},
	})
	if err == nil {
		t.Fatal("expected FK error for non-existent workspace, got nil")
	}
}

func TestServiceList(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(ctx, t)
	wid := insertWS(ctx, t)

	const content = "# Doc\nBody text."
	src, err := svc.Create(ctx, CreateRequest{WorkspaceID: wid, Name: "src1"}, []FileUpload{
		{Name: "doc.md", Content: bytes.NewBufferString(content), Size: int64(len(content))},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	awaitIndexed(ctx, t, svc, src.ID)

	got, err := svc.List(ctx, wid)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 source, got %d", len(got))
	}
	if got[0].ID != src.ID {
		t.Errorf("source ID mismatch: want %s, got %s", src.ID, got[0].ID)
	}
}

func TestServiceList_Empty(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(ctx, t)
	wid := insertWS(ctx, t)

	got, err := svc.List(ctx, wid)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 sources, got %d", len(got))
	}
}

func TestServiceGetByID(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(ctx, t)
	wid := insertWS(ctx, t)

	const content = "plain text content"
	src, err := svc.Create(ctx, CreateRequest{WorkspaceID: wid, Name: "fetchable"}, []FileUpload{
		{Name: "doc.txt", Content: bytes.NewBufferString(content), Size: int64(len(content))},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	awaitIndexed(ctx, t, svc, src.ID)

	got, err := svc.GetByID(ctx, src.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID != src.ID {
		t.Errorf("ID mismatch: want %s, got %s", src.ID, got.ID)
	}
	if got.Name != "fetchable" {
		t.Errorf("name mismatch: want fetchable, got %s", got.Name)
	}
}

func TestServiceGetByID_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(ctx, t)

	_, err := svc.GetByID(ctx, uuid.New())
	if err == nil {
		t.Fatal("expected error for non-existent ID, got nil")
	}
}

func TestServiceListChunks(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(ctx, t)
	wid := insertWS(ctx, t)

	const content = "# Section A\nContent A.\n\n# Section B\nContent B."
	src, err := svc.Create(ctx, CreateRequest{WorkspaceID: wid, Name: "chunky"}, []FileUpload{
		{Name: "multi.md", Content: bytes.NewBufferString(content), Size: int64(len(content))},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	awaitIndexed(ctx, t, svc, src.ID)

	chunks, err := svc.ListChunks(ctx, src.ID)
	if err != nil {
		t.Fatalf("ListChunks: %v", err)
	}
	if len(chunks) == 0 {
		t.Error("expected at least one chunk after indexing a two-section markdown file")
	}
}

func TestServiceIngest(t *testing.T) {
	ctx := context.Background()
	spy := &spyVectorStore{}
	svc := newSvcVS(ctx, t, spy)
	wid := insertWS(ctx, t)

	const content = "# Hello\nOriginal content."
	src, err := svc.Create(ctx, CreateRequest{WorkspaceID: wid, Name: "re-ingest"}, []FileUpload{
		{Name: "file.md", Content: bytes.NewBufferString(content), Size: int64(len(content))},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	awaitIndexed(ctx, t, svc, src.ID)

	if err := svc.Ingest(ctx, src.ID); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	awaitIndexed(ctx, t, svc, src.ID)

	chunks, err := svc.ListChunks(ctx, src.ID)
	if err != nil {
		t.Fatalf("ListChunks after re-ingest: %v", err)
	}
	if len(chunks) == 0 {
		t.Error("expected chunks after re-ingest")
	}

	// Regression guard for #61: re-ingestion must delete the previous generation
	// of vectors from Qdrant (initial ingest does not delete). Exactly one Delete
	// should have been issued, scoped to this source's source_id.
	deletes := spy.deletes()
	if len(deletes) != 1 {
		t.Fatalf("expected exactly one Qdrant delete during re-ingest, got %d", len(deletes))
	}
	if got := deleteFilterSourceID(t, deletes[0]); got != src.ID.String() {
		t.Errorf("delete filter source_id = %q, want %q", got, src.ID.String())
	}
}

// deleteFilterSourceID extracts the source_id keyword from a DeletePoints request
// built by deleteSourceVectors, failing the test if the shape is unexpected.
func deleteFilterSourceID(t *testing.T, req *qdrantpb.DeletePoints) string {
	t.Helper()
	must := req.GetPoints().GetFilter().GetMust()
	if len(must) != 1 {
		t.Fatalf("expected exactly one filter condition, got %d", len(must))
	}
	field := must[0].GetField()
	if field.GetKey() != "source_id" {
		t.Fatalf("delete filter key = %q, want source_id", field.GetKey())
	}
	return field.GetMatch().GetKeyword()
}

func TestServiceIngest_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(ctx, t)

	err := svc.Ingest(ctx, uuid.New())
	if err == nil {
		t.Fatal("expected error for non-existent source, got nil")
	}
}

// TestServiceIngest_DeleteVectorsError verifies that a failure deleting the old
// Qdrant vectors during re-ingestion aborts the pipeline and marks the source
// errored. Initial indexing still succeeds because deleteErrorVectorStore only
// fails Delete, not Upsert.
func TestServiceIngest_DeleteVectorsError(t *testing.T) {
	ctx := context.Background()
	svc := newSvcVS(ctx, t, deleteErrorVectorStore{})
	wid := insertWS(ctx, t)

	const content = "# Hello\nOriginal content."
	src, err := svc.Create(ctx, CreateRequest{WorkspaceID: wid, Name: "re-ingest-delete-fail"}, []FileUpload{
		{Name: "file.md", Content: bytes.NewBufferString(content), Size: int64(len(content))},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	awaitIndexed(ctx, t, svc, src.ID)

	if err := svc.Ingest(ctx, src.ID); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	awaitError(ctx, t, svc, src.ID)
}

// ── Helpers for pipeline-level tests ─────────────────────────────────────────

// buildCustomSvc builds a SourceService like newSvc but accepts custom pool,
// embedder, and vector store. The object-storage field is nil because the
// tests below call runPipeline directly, which never touches object storage.
// The chunking service always uses sharedPool so FK constraints are satisfied.
func buildCustomSvc(_ context.Context, t *testing.T, pool storage.Pool, emb embedding.Embedder, vs vectorstorage.Client) *SourceService {
	t.Helper()
	splitters := map[chunking.ContentType]chunking.Splitter{
		chunking.ContentTypeMarkdown:  markdown.New(),
		chunking.ContentTypePlaintext: text.New(),
	}
	return NewSourceService(
		pool,
		nil, // objectstorage.Client — not used by runPipeline
		sourcereader.NewFileReader(),
		chunking.NewChunkService(sharedPool, splitters),
		NewProgressBus(),
		emb,
		vs,
		"test",
		"nomic-embed-text",
	)
}

// insertSrcRow inserts a source row directly (bypassing the service) so that
// chunk FK constraints are satisfied when calling runPipeline. A cleanup
// deletes chunks and source on test exit.
func insertSrcRow(ctx context.Context, t *testing.T, wid uuid.UUID, tempDir string) model.Source {
	t.Helper()
	meta, _ := json.Marshal(model.FileSourceMetadata{RootPath: tempDir})
	src := model.Source{
		ID:          uuid.New(),
		WorkspaceID: wid,
		Name:        "pipeline-test",
		Type:        model.SourceTypeFile,
		URI:         fmt.Sprintf("%s/%s/", wid, uuid.New()),
		Status:      model.SourceStatusIndexing,
		Metadata:    json.RawMessage(meta),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	_, err := sharedPool.Exec(ctx, `
		INSERT INTO sources (id, workspace_id, name, type, uri, status, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		src.ID, src.WorkspaceID, src.Name, src.Type, src.URI,
		src.Status, []byte(src.Metadata), src.CreatedAt, src.UpdatedAt,
	)
	if err != nil {
		t.Fatalf("insertSrcRow: %v", err)
	}
	t.Cleanup(func() {
		_, _ = sharedPool.Exec(context.Background(), "DELETE FROM chunks WHERE source_id = $1", src.ID)
		_, _ = sharedPool.Exec(context.Background(), "DELETE FROM sources WHERE id = $1", src.ID)
	})
	return src
}

// makeTempDirWithFile creates a t.TempDir containing one file with the given
// name and content string.
func makeTempDirWithFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer f.Close() //nolint:errcheck
	if _, err := fmt.Fprint(f, content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return dir
}

// ── upsertChunkVectors direct tests ──────────────────────────────────────────

func TestUpsertChunkVectors_CountMismatch(t *testing.T) {
	ctx := context.Background()
	svc := &SourceService{vectorStore: stubVectorStore{}, collectionName: "test"}

	chunks := []model.Chunk{
		{ID: uuid.New(), WorkspaceID: uuid.New(), SourceID: uuid.New()},
		{ID: uuid.New(), WorkspaceID: uuid.New(), SourceID: uuid.New()},
	}
	embeddings := []embedding.Embedding{
		{ChunkID: chunks[0].ID.String(), Vector: []float32{0.1}},
	}

	err := svc.upsertChunkVectors(ctx, chunks, embeddings)
	if err == nil || !strings.Contains(err.Error(), "embedding count mismatch") {
		t.Fatalf("expected embedding count mismatch error, got: %v", err)
	}
}

func TestUpsertChunkVectors_EmptyVector(t *testing.T) {
	ctx := context.Background()
	svc := &SourceService{vectorStore: stubVectorStore{}, collectionName: "test"}

	id := uuid.New()
	chunks := []model.Chunk{
		{ID: id, WorkspaceID: uuid.New(), SourceID: uuid.New()},
	}
	embeddings := []embedding.Embedding{
		{ChunkID: id.String(), Vector: nil},
	}

	err := svc.upsertChunkVectors(ctx, chunks, embeddings)
	if err == nil || !strings.Contains(err.Error(), "empty vector") {
		t.Fatalf("expected empty vector error, got: %v", err)
	}
}

func TestUpsertChunkVectors_ZeroUUID(t *testing.T) {
	ctx := context.Background()
	svc := &SourceService{vectorStore: stubVectorStore{}, collectionName: "test"}

	chunks := []model.Chunk{
		{ID: uuid.Nil, WorkspaceID: uuid.New(), SourceID: uuid.New()},
	}
	embeddings := []embedding.Embedding{
		{ChunkID: uuid.Nil.String(), Vector: []float32{0.1, 0.2}},
	}

	err := svc.upsertChunkVectors(ctx, chunks, embeddings)
	if err == nil || !strings.Contains(err.Error(), "zero-value UUID") {
		t.Fatalf("expected zero-value UUID error, got: %v", err)
	}
}

func TestUpsertChunkVectors_UpsertError(t *testing.T) {
	ctx := context.Background()
	svc := &SourceService{vectorStore: errorVectorStore{}, collectionName: "test"}

	id := uuid.New()
	chunks := []model.Chunk{
		{ID: id, WorkspaceID: uuid.New(), SourceID: uuid.New()},
	}
	embeddings := []embedding.Embedding{
		{ChunkID: id.String(), Vector: []float32{0.1, 0.2}},
	}

	err := svc.upsertChunkVectors(ctx, chunks, embeddings)
	if err == nil || !strings.Contains(err.Error(), "qdrant upsert") {
		t.Fatalf("expected qdrant upsert error, got: %v", err)
	}
}

// ── deleteSourceVectors direct tests ─────────────────────────────────────────

func TestDeleteSourceVectors(t *testing.T) {
	ctx := context.Background()
	spy := &spyVectorStore{}
	svc := &SourceService{vectorStore: spy, collectionName: "test"}

	sourceID := uuid.New()
	if err := svc.deleteSourceVectors(ctx, sourceID); err != nil {
		t.Fatalf("deleteSourceVectors: %v", err)
	}

	deletes := spy.deletes()
	if len(deletes) != 1 {
		t.Fatalf("expected one delete request, got %d", len(deletes))
	}
	if got := deletes[0].GetCollectionName(); got != "test" {
		t.Errorf("collection name = %q, want test", got)
	}
	if got := deleteFilterSourceID(t, deletes[0]); got != sourceID.String() {
		t.Errorf("delete filter source_id = %q, want %q", got, sourceID.String())
	}
}

func TestDeleteSourceVectors_DeleteError(t *testing.T) {
	ctx := context.Background()
	svc := &SourceService{vectorStore: deleteErrorVectorStore{}, collectionName: "test"}

	err := svc.deleteSourceVectors(ctx, uuid.New())
	if err == nil || !strings.Contains(err.Error(), "qdrant delete") {
		t.Fatalf("expected qdrant delete error, got: %v", err)
	}
}

// ── updateEmbeddingModel direct test ─────────────────────────────────────────

func TestUpdateEmbeddingModel_ExecError(t *testing.T) {
	ctx := context.Background()
	svc := &SourceService{
		pool:           selectiveFailingPool{Pool: sharedPool},
		embeddingModel: "nomic-embed-text",
	}

	chunks := []model.Chunk{{ID: uuid.New()}}
	err := svc.updateEmbeddingModel(ctx, chunks)
	if err == nil || !strings.Contains(err.Error(), "update embedding_model") {
		t.Fatalf("expected update embedding_model error, got: %v", err)
	}
}

// ── runPipeline integration tests ─────────────────────────────────────────────

func TestRunPipeline_EmptyFile(t *testing.T) {
	ctx := context.Background()
	dir := makeTempDirWithFile(t, "empty.txt", "")

	meta, _ := json.Marshal(model.FileSourceMetadata{RootPath: dir})
	src := model.Source{
		ID:          uuid.New(),
		WorkspaceID: uuid.New(), // no DB record needed — 0 chunks means no FK check
		Type:        model.SourceTypeFile,
		Metadata:    json.RawMessage(meta),
	}

	svc := buildCustomSvc(ctx, t, sharedPool, &stubEmbedder{dim: 768}, stubVectorStore{})
	total, err := svc.runPipeline(ctx, src)
	if err != nil {
		t.Fatalf("runPipeline with empty file: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0 chunks for empty file, got %d", total)
	}
}

func TestRunPipeline_EmbedBatchError(t *testing.T) {
	ctx := context.Background()
	wid := insertWS(ctx, t)
	dir := makeTempDirWithFile(t, "doc.md", "# Section\nSome content here.")
	src := insertSrcRow(ctx, t, wid, dir)

	svc := buildCustomSvc(ctx, t, sharedPool, failingEmbedder{}, stubVectorStore{})
	_, err := svc.runPipeline(ctx, src)
	if err == nil || !strings.Contains(err.Error(), "embedding chunks from") {
		t.Fatalf("expected embed batch error, got: %v", err)
	}
}

func TestRunPipeline_UpsertError(t *testing.T) {
	ctx := context.Background()
	wid := insertWS(ctx, t)
	dir := makeTempDirWithFile(t, "doc.md", "# Section\nSome content here.")
	src := insertSrcRow(ctx, t, wid, dir)

	svc := buildCustomSvc(ctx, t, sharedPool, &stubEmbedder{dim: 768}, errorVectorStore{})
	_, err := svc.runPipeline(ctx, src)
	if err == nil || !strings.Contains(err.Error(), "upserting vectors for") {
		t.Fatalf("expected upsert error, got: %v", err)
	}
}

func TestRunPipeline_UpdateEmbeddingModelError(t *testing.T) {
	ctx := context.Background()
	wid := insertWS(ctx, t)
	dir := makeTempDirWithFile(t, "doc.md", "# Section\nSome content here.")
	src := insertSrcRow(ctx, t, wid, dir)

	// selectiveFailingPool intercepts only "UPDATE chunks SET embedding_model".
	// The chunker uses sharedPool directly (captured at construction time)
	// so chunk INSERTs continue to succeed.
	svc := buildCustomSvc(ctx, t, selectiveFailingPool{Pool: sharedPool}, &stubEmbedder{dim: 768}, stubVectorStore{})
	_, err := svc.runPipeline(ctx, src)
	if err == nil || !strings.Contains(err.Error(), "updating embedding model for") {
		t.Fatalf("expected updateEmbeddingModel error, got: %v", err)
	}
}
