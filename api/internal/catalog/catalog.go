// Package catalog is the single source of truth for which model providers
// NeuralVault can talk to, how to reach them, and what they can do.
//
// It deliberately does NOT list models. Model IDs change constantly, so they
// are fetched live from each provider (see the ModelLister interface in the
// llm package) rather than hardcoded here where they would rot.
//
// Most entries share one implementation: Gemini, OpenRouter, Groq, GitHub
// Models and OpenAI all expose an OpenAI-compatible /chat/completions API, so
// only the base URL differs between them.
package catalog

// Provider identifies a backend. These values are persisted in
// provider_credential.provider and workspace_model_settings.*_provider, and
// are used on the wire by the frontend — renaming one is a breaking change.
type Provider string

const (
	Ollama     Provider = "ollama"
	Anthropic  Provider = "anthropic"
	Gemini     Provider = "gemini"
	OpenRouter Provider = "openrouter"
	Groq       Provider = "groq"
	GitHub     Provider = "github"
	OpenAI     Provider = "openai"
)

// Kind is the client implementation backing a Provider.
type Kind string

const (
	// KindOllama is the local Ollama server (native /api/chat, /api/embed).
	KindOllama Kind = "ollama"
	// KindAnthropic is Anthropic's native Messages API. It is not folded into
	// KindOpenAICompat because only the native API reports prompt-cache token
	// counts, which llm/types.Usage already models.
	KindAnthropic Kind = "anthropic"
	// KindOpenAICompat is any OpenAI-compatible /chat/completions endpoint.
	KindOpenAICompat Kind = "openai_compat"
)

// Entry describes one provider.
type Entry struct {
	Provider Provider `json:"provider"`
	// Name is the human-readable label shown in the UI.
	Name string `json:"name"`
	// Kind selects the client implementation; not exposed to the frontend.
	Kind Kind `json:"-"`
	// BaseURL is the default endpoint. Empty for Ollama, whose URL comes from
	// the server's own configuration rather than the catalog.
	BaseURL string `json:"base_url"`
	// RequiresAPIKey is false only for Ollama, which is unauthenticated and
	// server-local.
	RequiresAPIKey bool `json:"requires_api_key"`
	// SupportsCompletions and SupportsEmbeddings gate which dropdowns a
	// provider may appear in. Anthropic has no embeddings API at all, and
	// Groq/OpenRouter/GitHub Models serve chat models only — offering them as
	// an embedding backend would produce a runtime failure at index time.
	SupportsCompletions bool `json:"supports_completions"`
	SupportsEmbeddings  bool `json:"supports_embeddings"`
	// FreeTier flags providers with a usable no-cost tier, which is what makes
	// them viable for local development without running Ollama.
	FreeTier bool `json:"free_tier"`
}

// entries is ordered as the UI should present it: the local default first,
// then the providers with a free tier, then the paid ones.
var entries = []Entry{
	{
		Provider:            Ollama,
		Name:                "Ollama (local)",
		Kind:                KindOllama,
		RequiresAPIKey:      false,
		SupportsCompletions: true,
		SupportsEmbeddings:  true,
		FreeTier:            true,
	},
	{
		Provider:            Gemini,
		Name:                "Google AI Studio (Gemini)",
		Kind:                KindOpenAICompat,
		BaseURL:             "https://generativelanguage.googleapis.com/v1beta/openai",
		RequiresAPIKey:      true,
		SupportsCompletions: true,
		SupportsEmbeddings:  true,
		FreeTier:            true,
	},
	{
		Provider:            Groq,
		Name:                "Groq",
		Kind:                KindOpenAICompat,
		BaseURL:             "https://api.groq.com/openai/v1",
		RequiresAPIKey:      true,
		SupportsCompletions: true,
		SupportsEmbeddings:  false,
		FreeTier:            true,
	},
	{
		Provider:            OpenRouter,
		Name:                "OpenRouter",
		Kind:                KindOpenAICompat,
		BaseURL:             "https://openrouter.ai/api/v1",
		RequiresAPIKey:      true,
		SupportsCompletions: true,
		SupportsEmbeddings:  false,
		FreeTier:            true,
	},
	{
		Provider:            GitHub,
		Name:                "GitHub Models",
		Kind:                KindOpenAICompat,
		BaseURL:             "https://models.github.ai/inference",
		RequiresAPIKey:      true,
		SupportsCompletions: true,
		SupportsEmbeddings:  false,
		FreeTier:            true,
	},
	{
		Provider:            Anthropic,
		Name:                "Anthropic",
		Kind:                KindAnthropic,
		BaseURL:             "https://api.anthropic.com",
		RequiresAPIKey:      true,
		SupportsCompletions: true,
		SupportsEmbeddings:  false,
		FreeTier:            false,
	},
	{
		Provider:            OpenAI,
		Name:                "OpenAI",
		Kind:                KindOpenAICompat,
		BaseURL:             "https://api.openai.com/v1",
		RequiresAPIKey:      true,
		SupportsCompletions: true,
		SupportsEmbeddings:  true,
		FreeTier:            false,
	},
}

// All returns every known provider, in display order.
func All() []Entry {
	out := make([]Entry, len(entries))
	copy(out, entries)
	return out
}

// Lookup returns the entry for p, reporting whether it is a known provider.
func Lookup(p Provider) (Entry, bool) {
	for _, e := range entries {
		if e.Provider == p {
			return e, true
		}
	}
	return Entry{}, false
}
