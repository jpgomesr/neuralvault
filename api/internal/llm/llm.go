// Package llm defines the provider interface and domain types for LLM inference.
// No business logic should depend on a concrete provider — only this interface.
package llm

import "context"

// Role identifies who authored a message in a conversation.
type Role string

const (
	RoleSystem    Role = "system"    // instructions that frame the model's behaviour
	RoleUser      Role = "user"      // input from the end user
	RoleAssistant Role = "assistant" // previous model turn, used for multi-turn context
)

// Message is a single turn in a conversation.
type Message struct {
	Role    Role
	Content string
}

// CompletionRequest is the input sent to a Provider.
type CompletionRequest struct {
	Messages    []Message
	Model       string
	MaxTokens   int
	Temperature float32
}

// CompletionResponse is the full response returned by a Provider.
type CompletionResponse struct {
	Content string
	Model   string
	Usage   Usage
}

// Usage reports token consumption for a single completion.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// StreamChunk is one incremental piece of a streamed completion.
// Done is true on the final chunk; Content may be empty on that chunk.
// The channel is closed after a final chunk (Done == true)
// or after an unrecoverable streaming error is emitted.
type StreamChunk struct {
    Content string
    Done    bool
    Error   error
}

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
