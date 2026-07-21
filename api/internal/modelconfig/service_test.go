package modelconfig

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/jpgomesr/neuralvault/api/internal/catalog"
	"github.com/jpgomesr/neuralvault/api/internal/config"
)

// newValidationOnlyService builds a service backed by nil dependencies. It is
// only safe for tests that exercise a validation error returned before the
// service ever touches the store, an LLM/embedding provider, or Qdrant —
// catalog.Lookup, SupportsCompletions/SupportsEmbeddings, and empty-value
// checks all run first and return before any of those are reached.
func newValidationOnlyService() *ModelConfigService {
	return NewModelConfigService(nil, nil, nil, &config.Config{})
}

func TestSaveCredential_ValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		provider catalog.Provider
		apiKey   string
	}{
		{name: "unknown provider", provider: catalog.Provider("does-not-exist"), apiKey: "sk-test"},
		{name: "provider takes no key", provider: catalog.Ollama, apiKey: "sk-test"},
		{name: "empty api key", provider: catalog.Anthropic, apiKey: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newValidationOnlyService()
			err := s.SaveCredential(context.Background(), uuid.New(), tt.provider, tt.apiKey, "")
			if !errors.Is(err, ErrInvalidProvider) {
				t.Fatalf("err = %v, want ErrInvalidProvider", err)
			}
		})
	}
}

func TestSetLLM_ValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		provider catalog.Provider
		model    string
	}{
		{name: "unknown provider", provider: catalog.Provider("does-not-exist"), model: "x"},
		{name: "empty model", provider: catalog.Anthropic, model: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newValidationOnlyService()
			err := s.SetLLM(context.Background(), uuid.New(), tt.provider, tt.model)
			if !errors.Is(err, ErrInvalidProvider) {
				t.Fatalf("err = %v, want ErrInvalidProvider", err)
			}
		})
	}
}

// TestSetEmbedding_ProviderDoesNotSupportEmbeddings guards the invariant
// resolver/embedderFor relies on: Groq, OpenRouter, and GitHub Models serve
// chat models only, so selecting one as an embedding provider must be
// rejected before ever probing it.
func TestSetEmbedding_ProviderDoesNotSupportEmbeddings(t *testing.T) {
	s := newValidationOnlyService()
	_, err := s.SetEmbedding(context.Background(), uuid.New(), catalog.Anthropic, "some-model")
	if !errors.Is(err, ErrInvalidProvider) {
		t.Fatalf("err = %v, want ErrInvalidProvider", err)
	}
}

func TestSetEmbedding_ValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		provider catalog.Provider
		model    string
	}{
		{name: "unknown provider", provider: catalog.Provider("does-not-exist"), model: "x"},
		{name: "empty model", provider: catalog.OpenAI, model: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newValidationOnlyService()
			_, err := s.SetEmbedding(context.Background(), uuid.New(), tt.provider, tt.model)
			if !errors.Is(err, ErrInvalidProvider) {
				t.Fatalf("err = %v, want ErrInvalidProvider", err)
			}
		})
	}
}

func TestModels_UnknownProvider(t *testing.T) {
	s := newValidationOnlyService()
	_, err := s.Models(context.Background(), uuid.New(), catalog.Provider("does-not-exist"))
	if !errors.Is(err, ErrInvalidProvider) {
		t.Fatalf("err = %v, want ErrInvalidProvider", err)
	}
}
