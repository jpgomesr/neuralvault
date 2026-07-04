// Package types defines the shared domain types for LLM completion input and
// output. It has no internal imports so both the llm interface package and
// concrete provider packages can import it without creating an import cycle.
package types

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
