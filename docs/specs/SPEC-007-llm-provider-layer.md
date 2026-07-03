#### SPEC-007: LLM provider layer

##### Status
Draft

##### Problem statement
After retrieval assembles context, NeuralVault must send it to a language model and stream the answer back. The platform is BYOK and multi-model by design — OpenAI, Claude, Gemini, Ollama, Qwen, DeepSeek are all first-class targets — so inference must sit behind one abstraction with no hardcoded provider. Today only the contract exists (`api/internal/llm/llm.go`); no concrete provider is implemented (roadmap Phase 1 "Ollama local model support").

##### Goals
- Implement concrete providers behind the existing `Provider` interface, starting with Ollama (local-first, no API key needed) and following with BYOK cloud providers.
- Stream responses end-to-end: provider stream → `StreamChunk` channel → SSE to the client, reusing the SSE experience established by the sources status endpoint.
- BYOK key handling: provider credentials supplied by the user, stored and scoped per workspace or per user — never shipped in the binary or shared across tenants.

##### Non-goals
- Prompt construction and context budgeting — the context layer's job ([SPEC-008](SPEC-008-context-intelligence.md)).
- Intelligent model routing across providers (roadmap Phase 4) — a future layer on top of this one.
- Tool use / function calling — not in the current contract; would extend `CompletionRequest` when needed.

##### Proposed design
`api/internal/llm/` already defines the shape (mirroring `embedding/` and `vectorstorage/`):

- `Provider` interface: `Complete(ctx, CompletionRequest) (CompletionResponse, error)` and `Stream(ctx, CompletionRequest) (<-chan StreamChunk, error)`; the channel closes after the `Done` chunk or an unrecoverable error, and cancelling `ctx` stops generation. Implementations must be safe for concurrent use.
- Domain types: `Message` with `system|user|assistant` roles, `CompletionRequest` (messages, model, max tokens, temperature), `Usage` token accounting.
- Planned provider subpackages per `CONTRIBUTING.md`: `llm/ollama/`, `llm/openai/`, `llm/claude/`, `llm/gemini/` — each an HTTP/SDK adapter translating the domain types, with a factory (`llm.NewProvider`) mirroring `embedding.NewEmbedder` for selection by config.
- A chat domain (handler/service/routes, likely `api/internal/chat/`) composes retrieval → context → `Provider.Stream` and relays chunks over SSE.

##### Affected components
- `api/internal/llm/` — provider subpackages + factory (interface already present)
- `api/internal/config/` — per-provider config structs (hosts, default models, key references)
- A future chat/completion domain wired in `router/router.go`
- Consumes [SPEC-006](SPEC-006-retrieval-engine.md), [SPEC-008](SPEC-008-context-intelligence.md)

##### Open questions
- Where do BYOK keys live — encrypted in Postgres per workspace, or env-only until multi-user auth ([SPEC-009](SPEC-009-platform-cross-cutting.md)) lands?
- Is provider selection per request, per workspace default, or both?
- How is `Usage` persisted for cost visibility, and does it feed the Phase 2 dashboard?
- Do Qwen/DeepSeek go through OpenAI-compatible endpoints (one generic adapter) or dedicated subpackages?

##### Acceptance criteria
- A completion request against a local Ollama model streams incremental chunks over SSE and terminates cleanly on `Done`, on error, and on client disconnect.
- Adding a second provider touches only `llm/` (new subpackage + factory branch) — no changes in chat or retrieval code.
- No provider credential ever appears in logs or responses.

##### Related (Optional)
- [SPEC-003](SPEC-003-embedding-generation.md) — sibling provider abstraction for embeddings
- [SPEC-006](SPEC-006-retrieval-engine.md), [SPEC-008](SPEC-008-context-intelligence.md)
