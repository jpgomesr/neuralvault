package retrieval

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jpgomesr/NeuralVault/internal/llm"
	"github.com/jpgomesr/NeuralVault/internal/model"
)

func TestBuildMessages_IncludesContextAndQuestion(t *testing.T) {
	chunks := []RetrievedChunk{
		{Chunk: model.Chunk{ID: uuid.New(), Content: "the sky is blue"}, Score: 0.9},
		{Chunk: model.Chunk{ID: uuid.New(), Content: "grass is green"}, Score: 0.8},
	}

	msgs := buildMessages("what colour is the sky?", chunks)

	if len(msgs) != 2 {
		t.Fatalf("expected system + user message, got %d", len(msgs))
	}
	if msgs[0].Role != llm.RoleSystem {
		t.Errorf("first message should be system, got %q", msgs[0].Role)
	}
	if msgs[1].Role != llm.RoleUser {
		t.Errorf("second message should be user, got %q", msgs[1].Role)
	}
	user := msgs[1].Content
	for _, want := range []string{"the sky is blue", "grass is green", "what colour is the sky?"} {
		if !strings.Contains(user, want) {
			t.Errorf("user message missing %q; got:\n%s", want, user)
		}
	}
}

func TestBuildMessages_NoContext(t *testing.T) {
	msgs := buildMessages("anything?", nil)
	if !strings.Contains(msgs[1].Content, "no relevant context") {
		t.Errorf("expected a no-context note; got:\n%s", msgs[1].Content)
	}
}
