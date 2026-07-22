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

	// CacheReadTokens counts prompt tokens served from a provider-side cache
	// hit, typically billed at a reduced rate. Zero for providers with no
	// cache support (e.g. Ollama) or any request without a cache hit.
	CacheReadTokens int

	// CacheCreationTokens counts prompt tokens written to a provider-side
	// cache for reuse by a later request, typically billed at a premium over
	// a normal input token. Zero for providers with no cache support or any
	// request that did not create a cache entry.
	CacheCreationTokens int
}

// ModelInfo identifies one model a provider can serve. Models are listed live
// from each provider rather than hardcoded, since the available IDs change
// frequently and differ per API key.
type ModelInfo struct {
	// ID is the value to send as CompletionRequest.Model.
	ID string `json:"id"`
	// Name is a human-readable label. Providers that expose no display name
	// repeat the ID here.
	Name string `json:"name"`
}

// ModelPurpose is what a listed model would be used for, so a ModelLister can
// filter out models that would fail if picked for that purpose (e.g. a chat
// model selected as an embedder).
//
// Most providers cannot self-report this per model and ignore it, returning
// their full list regardless of purpose.
type ModelPurpose string

const (
	// PurposeAny requests every model the credential can reach, unfiltered.
	// Used when the caller only wants to confirm the credential works, not to
	// populate a picker (see modelconfig.SaveCredential).
	PurposeAny ModelPurpose = ""
	// PurposeCompletion requests models usable for chat completions.
	PurposeCompletion ModelPurpose = "completion"
	// PurposeEmbedding requests models usable for embeddings.
	PurposeEmbedding ModelPurpose = "embedding"
)

// StreamChunk is one incremental piece of a streamed completion.
// Done is true on the final chunk; Content may be empty on that chunk.
// The channel is closed after a final chunk (Done == true)
// or after an unrecoverable streaming error is emitted.
type StreamChunk struct {
	Content string
	Done    bool
	Error   error
}
