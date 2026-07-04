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

	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/llm"
	"github.com/jpgomesr/NeuralVault/internal/llm/ollama"
)

// testCompletionModel is a small instruction-tuned model chosen for fast
// container pulls in CI; independent of the OLLAMA_COMPLETION_MODEL default
// documented in .env.example.
const testCompletionModel = "qwen2.5:0.5b"

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

	exitCode, reader, err := ctr.Exec(ctx, []string{"ollama", "pull", testCompletionModel})
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
			URL:             fmt.Sprintf("http://%s:%s", host, port.Port()),
			CompletionModel: testCompletionModel,
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
			URL:             sharedCfg.Ollama.URL,
			CompletionModel: "llama3",
		},
	}
	_, err := ollama.New(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected model-not-found error, got: %v", err)
	}
}

func TestComplete_RealModel(t *testing.T) {
	client, err := ollama.New(context.Background(), sharedCfg)
	if err != nil {
		t.Fatalf("ollama.New: %v", err)
	}

	resp, err := client.Complete(context.Background(), llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "Reply with the single word: pong"},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if strings.TrimSpace(resp.Content) == "" {
		t.Fatal("expected non-empty content")
	}
	if resp.Usage.TotalTokens == 0 {
		t.Fatal("expected non-zero token usage")
	}
}

func TestStream_RealModel(t *testing.T) {
	client, err := ollama.New(context.Background(), sharedCfg)
	if err != nil {
		t.Fatalf("ollama.New: %v", err)
	}

	chunks, err := client.Stream(context.Background(), llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "Count from one to three."},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var content strings.Builder
	var sawDone bool
	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Error)
		}
		content.WriteString(chunk.Content)
		if chunk.Done {
			sawDone = true
		}
	}

	if !sawDone {
		t.Fatal("expected a final chunk with Done == true")
	}
	if strings.TrimSpace(content.String()) == "" {
		t.Fatal("expected non-empty streamed content")
	}
}
