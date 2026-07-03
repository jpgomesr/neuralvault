#### SPEC-006: Retrieval engine

##### Status
Draft

##### Problem statement
Indexed knowledge is useless until it can be queried. NeuralVault needs a retrieval engine that takes a user query, finds the most relevant chunks across the workspace's sources, and hands an ordered, cited set of chunks to the context layer — the core of the product's value proposition. Nothing of this exists in code yet (roadmap Phase 1 "Basic chat interface" through Phase 2 "Retrieval Quality").

##### Goals
- Semantic search: embed the query with the same `Embedder` used at indexing time ([SPEC-003](SPEC-003-embedding-generation.md)) and run a workspace-filtered `Query` against Qdrant ([SPEC-004](SPEC-004-vector-storage.md)).
- Hydrate results: map returned point IDs back to `chunks` rows in Postgres for text and location metadata, preserving score order.
- Evolve toward Phase 2 quality features behind the same interface: hybrid search (semantic + keyword), metadata filtering, and a reranking layer.

##### Non-goals
- Context compression, prioritization, and memory — downstream concerns ([SPEC-008](SPEC-008-context-intelligence.md)).
- LLM inference on the retrieved context ([SPEC-007](SPEC-007-llm-provider-layer.md)).
- Retrieval analytics and dashboard (roadmap Phase 2 observability items) — they consume this engine but are not part of it.

##### Proposed design
A new `api/internal/retrieval/` domain following the established layout (interface at root, `handler.go`/`service.go`/`routes.go`, mounted in `router/router.go`):

- A `Retriever` interface roughly `Retrieve(ctx, RetrieveRequest) ([]RetrievedChunk, error)`, where the request carries workspace ID, query text, limit, and optional source filters, and each result pairs a `model.Chunk` with its similarity score.
- First implementation: query → `Embedder.Embed` → `vectorstorage.Client.Query` with a `workspace_id` payload filter → batch-load chunks by ID from Postgres → return in score order.
- Phase 2 evolutions compose behind the interface: a keyword leg (Postgres full-text search over `chunks.content`) fused with the semantic leg, filter pushdown on chunk metadata, and a pluggable reranker applied to the fused candidate set.

##### Affected components
- `api/internal/retrieval/` (new)
- `api/internal/router/router.go` — wiring
- Consumes [SPEC-003](SPEC-003-embedding-generation.md), [SPEC-004](SPEC-004-vector-storage.md), [SPEC-005](SPEC-005-relational-persistence.md)

##### Open questions
- Is retrieval exposed as its own endpoint (`POST /retrieve`?) for the Phase 5 SDK/MCP surface, or only consumed internally by the chat flow at first?
- Hybrid fusion strategy: reciprocal rank fusion vs. score normalisation — and does Qdrant's built-in hybrid query support cover the keyword leg, or does Postgres FTS own it?
- Reranker choice: local cross-encoder via Ollama vs. provider API — and is it in-process or a separate concern?
- What guards against querying a workspace whose chunks were embedded with a different model than the current one (`chunks.embedding_model` mismatch)?

##### Acceptance criteria
- Given an indexed workspace, a natural-language query returns the top-k chunks with scores, text, and source/location metadata, and never returns chunks from another workspace.
- Query embedding uses the same model as indexing, or the mismatch is surfaced as an error.
- Adding hybrid search or a reranker does not change the `Retriever` interface consumed by the chat flow.

##### Related (Optional)
- [ADR-003](../adr/ADR-003-core-vector-database-decision.md) — Qdrant capabilities this engine builds on
- [SPEC-007](SPEC-007-llm-provider-layer.md), [SPEC-008](SPEC-008-context-intelligence.md)
