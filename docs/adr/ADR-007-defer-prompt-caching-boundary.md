#### ADR-007: Defer prompt-caching modeling at the CompletionRequest boundary

##### Status
Proposed

##### Context
[SPEC-007](../specs/SPEC-007-llm-provider-layer.md) plans four provider subpackages —
`llm/ollama/`, `llm/openai/`, `llm/claude/`, `llm/gemini/` — but only Ollama is
implemented; Claude, OpenAI, and Gemini are "Not started." Prompt caching is
provider-specific: Anthropic requires explicit `cache_control` breakpoints placed on
individual content blocks, OpenAI caches automatically with no client-side signal, and
Gemini uses an explicit, separately-managed context-caching resource. `CompletionRequest`
(`api/internal/llm/types/types.go`) has no caching field today, and `Message.Content` is a
flat `string` with no block/part structure — the shape Anthropic's per-block
`cache_control` would actually need. The live RAG flow (`retrieval.Service.Answer` →
`buildMessages`) is single-shot: a stable system prompt plus per-query retrieved context
that changes every request, with no growing multi-turn history resent — i.e. intrinsically
non-cacheable today. SPEC-007's open question "How is `Usage` persisted for cost
visibility?" also can't be answered accurately once caching-capable providers land, since
cached vs. fresh input tokens are billed differently.

##### Decision
Do **not** add any caching-related field to `CompletionRequest`. Caching intent is left
entirely to each provider adapter to handle internally, using whatever mechanism fits that
provider (e.g. a Claude adapter deciding for itself where to place `cache_control`
breakpoints; a Gemini adapter managing its own cached-content resources), for as long as no
adapter actually needs a signal from the caller. Separately, extend only `llm.Usage` (not
`CompletionRequest`) with `CacheReadTokens` and `CacheCreationTokens` fields now, since
these are provider-reported facts about a completed request, not caller intent, and a
zero-value default is trivially correct for Ollama.

With zero cloud adapters built and the current RAG flow structurally non-cacheable, there
is no real usage pattern to generalize an abstraction from; the three providers' caching
models (annotation vs. automatic vs. resource) are different enough that a generic hint
today would fit at most one of them and would need redesigning once a concrete adapter is
built anyway. The first caching-capable adapter (most likely Claude, given `cache_control`'s
direct mapping to a stable system/context boundary) is expected to also need
`Message.Content` to move from a flat `string` to a block/part-based type to carry per-block
cache annotations — exactly the shape decision this ADR avoids freezing prematurely.

This decision is recorded in SPEC-007 (Proposed design, Open questions) and partially
resolves that spec's "How is `Usage` persisted for cost visibility?" open question (the
field shape is now decided); persistence mechanism and dashboard integration remain open
there.

##### Consequences

###### Positive
- No speculative API surface added to `CompletionRequest`/`Message` before any adapter
  needs it — avoids baking an abstraction that only fits one provider.
- `llm.Usage` gains cache accounting now, unblocking half of SPEC-007's Usage-persistence
  open question without waiting on any cloud adapter.
- Ollama and the current single-shot RAG flow are unaffected; `CacheReadTokens` /
  `CacheCreationTokens` are always zero today, which is trivially correct.

###### Negative
- Building the first caching-capable adapter will almost certainly require a breaking
  change to `Message.Content` (flat `string` → content blocks) to carry `cache_control`
  breakpoints — deferred cost, not avoided cost.
- No forcing function marks when "it becomes relevant"; this stays a judgment call made
  when a caching-capable adapter is actually scoped, not decided by a generic rule now.
- `Usage.CacheReadTokens` / `CacheCreationTokens` are defined but unpopulated and
  unpersisted until an adapter reports them and a persistence layer exists.

##### Related decision (Optional)
- [SPEC-007](../specs/SPEC-007-llm-provider-layer.md) — Proposed design (Usage token
  accounting) and Open questions ("How is Usage persisted for cost visibility") are
  updated to reflect this decision.
