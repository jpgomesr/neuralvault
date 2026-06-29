package sources

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/jpgomesr/NeuralVault/internal/chunking"
	"github.com/jpgomesr/NeuralVault/internal/chunking/markdown"
	"github.com/jpgomesr/NeuralVault/internal/chunking/text"
	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/model"
	minioclient "github.com/jpgomesr/NeuralVault/internal/objectstorage/minio"
	"github.com/jpgomesr/NeuralVault/internal/sourcereader"
	pgstore "github.com/jpgomesr/NeuralVault/internal/storage/postgres"
)

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

// newSvc builds a SourceService wired to the shared containers.
func newSvc(ctx context.Context, t *testing.T) *SourceService {
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
	svc := newSvc(ctx, t)
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
}

func TestServiceIngest_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(ctx, t)

	err := svc.Ingest(ctx, uuid.New())
	if err == nil {
		t.Fatal("expected error for non-existent source, got nil")
	}
}
