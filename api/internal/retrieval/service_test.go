package retrieval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	qdrantpb "github.com/qdrant/go-client/qdrant"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/embedding"
	"github.com/jpgomesr/NeuralVault/internal/model"
	pgstore "github.com/jpgomesr/NeuralVault/internal/storage/postgres"
	"github.com/jpgomesr/NeuralVault/internal/vectorstorage"
)

const testVectorSize = 8

// fixedEmbedder returns the same vector for every query, regardless of text.
// It satisfies embedding.Embedder without requiring a running Ollama instance.
type fixedEmbedder struct{ vector []float32 }

func (f fixedEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return f.vector, nil
}

func (f fixedEmbedder) EmbedBatch(_ context.Context, chunks []embedding.Chunk) ([]embedding.Embedding, error) {
	out := make([]embedding.Embedding, len(chunks))
	for i, c := range chunks {
		out[i] = embedding.Embedding{ChunkID: c.ID, Vector: f.vector}
	}
	return out, nil
}

// failingEmbedder always returns an error from Embed.
type failingEmbedder struct{}

func (failingEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, errors.New("embed failed")
}
func (failingEmbedder) EmbedBatch(_ context.Context, _ []embedding.Chunk) ([]embedding.Embedding, error) {
	return nil, errors.New("embed batch failed")
}

var (
	sharedPool      *pgxpool.Pool
	sharedVecStore  vectorstorage.Client
	sharedQdrantCfg config.Qdrant
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

	// ── Qdrant ────────────────────────────────────────────────────────────────
	qdrantCtr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "qdrant/qdrant:v1.18.2",
			ExposedPorts: []string{"6333/tcp", "6334/tcp"},
			WaitingFor:   wait.ForHTTP("/healthz").WithPort("6333/tcp"),
		},
		Started: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "start qdrant: %v\n", err)
		return 1
	}
	defer func() { _ = qdrantCtr.Terminate(ctx) }()

	qHost, err := qdrantCtr.Host(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "qdrant host: %v\n", err)
		return 1
	}
	qPort, err := qdrantCtr.MappedPort(ctx, "6334")
	if err != nil {
		fmt.Fprintf(os.Stderr, "qdrant grpc port: %v\n", err)
		return 1
	}

	sharedQdrantCfg = config.Qdrant{
		URL:            qHost,
		GrpcPort:       int(qPort.Num()),
		CollectionName: "retrieval-test",
		VectorSize:     testVectorSize,
	}
	sharedVecStore, err = vectorstorage.NewClient(ctx, &config.Config{Qdrant: sharedQdrantCfg})
	if err != nil {
		fmt.Fprintf(os.Stderr, "create qdrant client: %v\n", err)
		return 1
	}
	defer func() { _ = sharedVecStore.Close() }()

	return m.Run()
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

// insertSource inserts a minimal source row satisfying the chunks FK.
func insertSource(ctx context.Context, t *testing.T, workspaceID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := sharedPool.Exec(ctx, `
		INSERT INTO sources (id, workspace_id, name, type, uri, status)
		VALUES ($1, $2, 'src', 'file', 'uri', 'indexed')`,
		id, workspaceID,
	)
	if err != nil {
		t.Fatalf("insert source: %v", err)
	}
	return id
}

// chunkIndexCounters tracks the next chunk_index to use per source, since
// (source_id, chunk_index) is unique.
var chunkIndexCounters = map[uuid.UUID]int{}

// insertChunk inserts a chunk row and upserts a matching vector into Qdrant.
func insertChunk(ctx context.Context, t *testing.T, workspaceID, sourceID uuid.UUID, content string, vector []float32) model.Chunk {
	t.Helper()
	idx := chunkIndexCounters[sourceID]
	chunkIndexCounters[sourceID] = idx + 1

	chunk := model.Chunk{
		ID:             uuid.New(),
		SourceID:       sourceID,
		WorkspaceID:    workspaceID,
		Content:        content,
		ChunkIndex:     idx,
		EmbeddingModel: "test-model",
	}
	_, err := sharedPool.Exec(ctx, `
		INSERT INTO chunks (id, source_id, workspace_id, content, chunk_index, embedding_model)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		chunk.ID, chunk.SourceID, chunk.WorkspaceID, chunk.Content, chunk.ChunkIndex, chunk.EmbeddingModel,
	)
	if err != nil {
		t.Fatalf("insert chunk: %v", err)
	}

	_, err = sharedVecStore.Upsert(ctx, &qdrantpb.UpsertPoints{
		CollectionName: sharedQdrantCfg.CollectionName,
		Points: []*qdrantpb.PointStruct{
			{
				Id:      qdrantpb.NewID(chunk.ID.String()),
				Vectors: qdrantpb.NewVectors(vector...),
				Payload: qdrantpb.NewValueMap(map[string]any{
					"chunk_id":     chunk.ID.String(),
					"workspace_id": chunk.WorkspaceID.String(),
					"source_id":    chunk.SourceID.String(),
				}),
			},
		},
	})
	if err != nil {
		t.Fatalf("upsert vector: %v", err)
	}
	return chunk
}

func newSvc(emb embedding.Embedder) *RetrievalService {
	return NewRetrievalService(sharedPool, emb, sharedVecStore, sharedQdrantCfg.CollectionName)
}

// vec returns a fixed one-hot vector, used where only vector identity (not
// direction) matters to the test.
func vec(first float32) []float32 {
	v := make([]float32, testVectorSize)
	v[0] = first
	return v
}

// oneHot returns a vector with val at position dim and zero elsewhere. Cosine
// similarity depends on direction, not magnitude, so tests asserting a score
// ordering must use vectors that differ in direction (not just scale).
func oneHot(dim int, val float32) []float32 {
	v := make([]float32, testVectorSize)
	v[dim] = val
	return v
}

// ── Retrieve integration tests ────────────────────────────────────────────────

func TestRetrieve_ReturnsTopKOrderedByScore(t *testing.T) {
	ctx := context.Background()
	wid := insertWS(ctx, t)
	sid := insertSource(ctx, t, wid)

	// Vectors differ by direction (not just magnitude) so cosine similarity
	// actually distinguishes them: closest is collinear with the query,
	// second is at a 45-degree-ish angle, third is orthogonal.
	insertChunk(ctx, t, wid, sid, "closest match", oneHot(0, 1.0))
	insertChunk(ctx, t, wid, sid, "second match", []float32{0.6, 0.8, 0, 0, 0, 0, 0, 0})
	insertChunk(ctx, t, wid, sid, "third match", oneHot(1, 1.0))

	svc := newSvc(fixedEmbedder{vector: oneHot(0, 1.0)})
	results, err := svc.Retrieve(ctx, RetrieveRequest{WorkspaceID: wid, Query: "anything", TopK: 2})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (topK), got %d", len(results))
	}
	if results[0].Score < results[1].Score {
		t.Errorf("expected results ordered by descending score, got %v then %v", results[0].Score, results[1].Score)
	}
	if results[0].Chunk.Content != "closest match" {
		t.Errorf("expected best match first, got %q", results[0].Chunk.Content)
	}
}

func TestRetrieve_ScopedToWorkspace(t *testing.T) {
	ctx := context.Background()
	widA := insertWS(ctx, t)
	sidA := insertSource(ctx, t, widA)
	insertChunk(ctx, t, widA, sidA, "workspace A content", vec(1.0))

	widB := insertWS(ctx, t)
	sidB := insertSource(ctx, t, widB)
	insertChunk(ctx, t, widB, sidB, "workspace B content", vec(1.0))

	svc := newSvc(fixedEmbedder{vector: vec(1.0)})
	results, err := svc.Retrieve(ctx, RetrieveRequest{WorkspaceID: widA, Query: "anything", TopK: 10})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	for _, r := range results {
		if r.Chunk.WorkspaceID != widA {
			t.Fatalf("leaked chunk from another workspace: %+v", r.Chunk)
		}
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result scoped to workspace A, got %d", len(results))
	}
}

func TestRetrieve_NoMatches(t *testing.T) {
	ctx := context.Background()
	wid := insertWS(ctx, t)

	svc := newSvc(fixedEmbedder{vector: vec(1.0)})
	results, err := svc.Retrieve(ctx, RetrieveRequest{WorkspaceID: wid, Query: "anything"})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty workspace, got %d", len(results))
	}
}

func TestRetrieve_DefaultsTopKWhenUnset(t *testing.T) {
	ctx := context.Background()
	wid := insertWS(ctx, t)
	sid := insertSource(ctx, t, wid)
	for i := 0; i < defaultTopK+2; i++ {
		insertChunk(ctx, t, wid, sid, fmt.Sprintf("chunk %d", i), vec(1.0))
	}

	svc := newSvc(fixedEmbedder{vector: vec(1.0)})
	results, err := svc.Retrieve(ctx, RetrieveRequest{WorkspaceID: wid, Query: "anything"}) // TopK unset
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) != defaultTopK {
		t.Errorf("expected defaultTopK=%d results, got %d", defaultTopK, len(results))
	}
}

func TestRetrieve_CapsTopKAtMax(t *testing.T) {
	ctx := context.Background()
	wid := insertWS(ctx, t)

	svc := newSvc(fixedEmbedder{vector: vec(1.0)})
	_, err := svc.Retrieve(ctx, RetrieveRequest{WorkspaceID: wid, Query: "anything", TopK: maxTopK + 100})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
}

func TestRetrieve_EmbedError(t *testing.T) {
	ctx := context.Background()
	wid := insertWS(ctx, t)

	svc := newSvc(failingEmbedder{})
	_, err := svc.Retrieve(ctx, RetrieveRequest{WorkspaceID: wid, Query: "anything"})
	if err == nil || !strings.Contains(err.Error(), "embedding query") {
		t.Fatalf("expected embedding query error, got: %v", err)
	}
}
