package ollama_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/jpgomesr/neuralvault/api/internal/config"
	"github.com/jpgomesr/neuralvault/api/internal/embedding"
	"github.com/jpgomesr/neuralvault/api/internal/embedding/ollama"
)

const testEmbeddingModel = "nomic-embed-text"

// nomicEmbedTextDimensions is the known vector size produced by nomic-embed-text.
const nomicEmbedTextDimensions = 768

var sharedCfg *config.Config

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	ctx := context.Background()

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "ollama/ollama:latest",
			ExposedPorts: []string{"11434/tcp"},
			WaitingFor:   wait.ForHTTP("/").WithPort("11434/tcp"),
		},
		Started: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "start ollama container: %v\n", err)
		return 1
	}
	defer func() { _ = ctr.Terminate(ctx) }()

	// Pulling the embedding model can take real wall-clock time (network
	// dependent); this mirrors `ollama pull nomic-embed-text` from AGENTS.md.
	exitCode, reader, err := ctr.Exec(ctx, []string{"ollama", "pull", testEmbeddingModel})
	if err != nil {
		fmt.Fprintf(os.Stderr, "exec ollama pull: %v\n", err)
		return 1
	}
	if exitCode != 0 {
		out, _ := io.ReadAll(reader)
		fmt.Fprintf(os.Stderr, "ollama pull failed (exit %d): %s\n", exitCode, out)
		return 1
	}

	host, err := ctr.Host(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get container host: %v\n", err)
		return 1
	}
	port, err := ctr.MappedPort(ctx, "11434")
	if err != nil {
		fmt.Fprintf(os.Stderr, "get mapped port: %v\n", err)
		return 1
	}

	sharedCfg = &config.Config{
		Ollama: config.Ollama{
			URL:            fmt.Sprintf("http://%s:%s", host, port.Port()),
			EmbeddingModel: testEmbeddingModel,
		},
	}

	return m.Run()
}

func TestNew_Success(t *testing.T) {
	client, err := ollama.New(context.Background(), sharedCfg)
	if err != nil {
		t.Fatalf("ollama.New: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNew_ModelNotPulled(t *testing.T) {
	cfg := &config.Config{
		Ollama: config.Ollama{
			URL:            sharedCfg.Ollama.URL,
			EmbeddingModel: "llama3",
		},
	}
	_, err := ollama.New(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected model-not-found error, got: %v", err)
	}
}

func TestEmbed_RealModel(t *testing.T) {
	client, err := ollama.New(context.Background(), sharedCfg)
	if err != nil {
		t.Fatalf("ollama.New: %v", err)
	}

	vector, err := client.Embed(context.Background(), "PostgreSQL is an open-source relational database.")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vector) != nomicEmbedTextDimensions {
		t.Fatalf("expected a %d-dimension vector, got %d", nomicEmbedTextDimensions, len(vector))
	}

	var nonZero bool
	for _, v := range vector {
		if v != 0 {
			nonZero = true
			break
		}
	}
	if !nonZero {
		t.Fatal("expected a non-zero vector")
	}
}

func TestEmbedBatch_RealModel(t *testing.T) {
	client, err := ollama.New(context.Background(), sharedCfg)
	if err != nil {
		t.Fatalf("ollama.New: %v", err)
	}

	chunks := []embedding.Chunk{
		{ID: "a", Text: "PostgreSQL is an open-source relational database."},
		{ID: "b", Text: "The Eiffel Tower is located in Paris, France."},
		{ID: "c", Text: "Go is a statically typed, compiled programming language."},
	}
	got, err := client.EmbedBatch(context.Background(), chunks)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(got) != len(chunks) {
		t.Fatalf("expected %d embeddings, got %d", len(chunks), len(got))
	}

	for i, e := range got {
		if e.ChunkID != chunks[i].ID {
			t.Fatalf("index %d: expected ChunkID %q, got %q", i, chunks[i].ID, e.ChunkID)
		}
		if len(e.Vector) != nomicEmbedTextDimensions {
			t.Fatalf("index %d: expected a %d-dimension vector, got %d", i, nomicEmbedTextDimensions, len(e.Vector))
		}
	}

	if equalVectors(got[0].Vector, got[1].Vector) {
		t.Fatal("expected distinct chunks to produce distinct vectors")
	}
}

func equalVectors(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
