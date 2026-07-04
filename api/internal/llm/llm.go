// Package llm defines the provider interface and a factory that returns
// the configured provider implementation.
// No business logic should depend on a concrete provider — only this interface.
package llm

import (
	"context"

	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/llm/ollama"
	"github.com/jpgomesr/NeuralVault/internal/llm/types"
)

// Role, Message, CompletionRequest, CompletionResponse, Usage and StreamChunk
// are re-exported from the shared types package so callers only need to
// import this package.
type (
	Role               = types.Role
	Message            = types.Message
	CompletionRequest  = types.CompletionRequest
	CompletionResponse = types.CompletionResponse
	Usage              = types.Usage
	StreamChunk        = types.StreamChunk
)

const (
	RoleSystem    = types.RoleSystem    // instructions that frame the model's behaviour
	RoleUser      = types.RoleUser      // input from the end user
	RoleAssistant = types.RoleAssistant // previous model turn, used for multi-turn context
)

// Provider is the single abstraction over any LLM backend
// (OpenAI, Claude, Gemini, Ollama, …).
//
// Implementations must be safe for concurrent use.
type Provider interface {
	// Complete sends a blocking request and returns the full response.
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)

	// Stream returns a channel that emits incremental chunks as the model generates them.
	// The channel is closed after the chunk with Done == true is sent.
	// Cancelling ctx stops generation and closes the channel early.
	Stream(ctx context.Context, req CompletionRequest) (<-chan StreamChunk, error)
}

// NewProvider creates and returns a Provider backed by the configured provider.
func NewProvider(ctx context.Context, cfg *config.Config) (Provider, error) {
	return ollama.New(ctx, cfg)
}
