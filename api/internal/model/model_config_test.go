package model

import (
	"testing"

	"github.com/jpgomesr/neuralvault/api/internal/catalog"
)

func TestWorkspaceModelSettings_HasLLM(t *testing.T) {
	tests := []struct {
		name     string
		settings WorkspaceModelSettings
		want     bool
	}{
		{name: "unset", settings: WorkspaceModelSettings{}, want: false},
		{name: "provider only", settings: WorkspaceModelSettings{LLMProvider: catalog.Anthropic}, want: false},
		{name: "model only", settings: WorkspaceModelSettings{LLMModel: "claude-sonnet-5"}, want: false},
		{name: "both set", settings: WorkspaceModelSettings{LLMProvider: catalog.Anthropic, LLMModel: "claude-sonnet-5"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.settings.HasLLM(); got != tt.want {
				t.Errorf("HasLLM() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestWorkspaceModelSettings_HasEmbedding covers the doc comment's invariant:
// a half-configured embedding setup (missing the collection or dimensions
// resolver.ResolveEmbedder needs to search the right Qdrant collection) must
// never be considered valid.
func TestWorkspaceModelSettings_HasEmbedding(t *testing.T) {
	full := WorkspaceModelSettings{
		EmbeddingProvider:   catalog.OpenAI,
		EmbeddingModel:      "text-embedding-3-small",
		EmbeddingCollection: "nv_openai_text_embedding_3_small_1536",
		EmbeddingDimensions: 1536,
	}

	tests := []struct {
		name     string
		settings WorkspaceModelSettings
		want     bool
	}{
		{name: "unset", settings: WorkspaceModelSettings{}, want: false},
		{name: "fully configured", settings: full, want: true},
		{name: "missing provider", settings: func() WorkspaceModelSettings { s := full; s.EmbeddingProvider = ""; return s }(), want: false},
		{name: "missing model", settings: func() WorkspaceModelSettings { s := full; s.EmbeddingModel = ""; return s }(), want: false},
		{name: "missing collection", settings: func() WorkspaceModelSettings { s := full; s.EmbeddingCollection = ""; return s }(), want: false},
		{name: "zero dimensions", settings: func() WorkspaceModelSettings { s := full; s.EmbeddingDimensions = 0; return s }(), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.settings.HasEmbedding(); got != tt.want {
				t.Errorf("HasEmbedding() = %v, want %v", got, tt.want)
			}
		})
	}
}
