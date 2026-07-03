#### SPEC-008: Context intelligence

##### Status
Draft

##### Problem statement
Retrieved chunks rarely fit an LLM context window as-is, and naive concatenation wastes tokens on redundant or low-value content. Roadmap Phase 3 ("Context Intelligence") plans a layer between retrieval and inference that compresses, prioritizes, and remembers — turning raw retrieval results into an optimized context. This is the least-designed area of the system; this spec frames the problem and boundaries rather than a detailed design.

##### Goals
- Context compression: fit the most relevant retrieved content into a per-model token budget.
- Context prioritization: order and select chunks by value to the query, not just similarity score.
- Conversation memory: carry relevant history across turns; active memory: persist durable facts across sessions.
- Multi-source retrieval shaping: merge and deduplicate context drawn from several sources.

##### Non-goals
- Retrieval itself — candidate chunks arrive from [SPEC-006](SPEC-006-retrieval-engine.md).
- Provider communication — the optimized context is handed to [SPEC-007](SPEC-007-llm-provider-layer.md).
- Knowledge graph and agent memory (roadmap Phase 4) — likely consumers or extensions of this layer, specified separately when they mature.

##### Proposed design
High-level only, to be refined per-feature before implementation:

- A context domain (e.g. `api/internal/contextengine/`) following the repo layout, exposing something like `Build(ctx, BuildRequest) (Context, error)` — input: query, retrieved chunks, conversation history, token budget; output: ordered messages/content ready for `llm.CompletionRequest`.
- Compression strategies range from cheap (truncation, dedup, score-threshold dropping) to model-assisted (summarising chunks via a small local model). Start cheap; keep the strategy pluggable like `Splitter`/`Embedder`.
- Conversation memory implies persisting conversations/turns in Postgres (new tables — extends [SPEC-005](SPEC-005-relational-persistence.md)); active memory implies a write-back path where distilled facts are re-indexed as first-class retrievable content.

##### Affected components
- A new `api/internal/` context domain (name TBD)
- `api/internal/model/` + new migrations — conversation/memory tables
- Consumes [SPEC-006](SPEC-006-retrieval-engine.md); feeds [SPEC-007](SPEC-007-llm-provider-layer.md)

##### Open questions
- Token counting: per-provider tokenizers vs. a single approximation — where does the budget for each model come from?
- Is model-assisted compression worth its latency/cost on the critical path, or an offline enrichment step?
- What is the schema and lifecycle of "active memory" entries — who writes them, how are they invalidated, are they workspace- or user-scoped?
- Does conversation memory live in this layer or in the chat domain that owns conversations?

##### Acceptance criteria
- Given retrieved chunks exceeding the token budget, the layer produces a context within budget that retains the highest-priority content deterministically.
- Multi-turn conversations receive relevant prior-turn context without resending full history every turn.
- The compression strategy is swappable without changing the chat flow.

##### Related (Optional)
- [SPEC-006](SPEC-006-retrieval-engine.md), [SPEC-007](SPEC-007-llm-provider-layer.md), [SPEC-005](SPEC-005-relational-persistence.md)
