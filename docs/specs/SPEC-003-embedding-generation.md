#### SPEC-003: Embedding generation

##### Status
Implemented

##### Problem statement
Semantic retrieval requires turning chunk text into vectors. NeuralVault is model-agnostic by design (`docs/architecture.md`), so embedding generation must sit behind an abstraction that lets providers be swapped without changing the ingestion pipeline.

##### Goals
- A provider-agnostic `Embedder` interface with batch support to minimise round-trips.
- A local-first default: `nomic-embed-text` via Ollama, self-hosted alongside the stack.
- Record which model produced each chunk's embedding so future model migrations are detectable.

##### Non-goals
- Concrete cloud providers (OpenAI, Gemini) — planned in `CONTRIBUTING.md` as `embedding/openai/` but not implemented.
- Embedding-model configuration per workspace or per user (listed under Pluggable Providers in `docs/architecture.md` as a design intention).
- LLM inference (see [SPEC-007](SPEC-007-llm-provider-layer.md)).

##### Proposed design
`api/internal/embedding/` mirrors the `vectorstorage` package shape:

- `Embedder` interface (`embedding.go`): `Embed(ctx, text) ([]float32, error)` for a single string and `EmbedBatch(ctx, []Chunk) ([]Embedding, error)` preserving input order and linking results via `Embedding.ChunkID`. Implementations must be safe for concurrent use; an empty batch returns an empty slice.
- `embedding/types/` holds the shared `Chunk`/`Embedding` structs to break the import cycle with provider subpackages; `embedding.go` re-exports them so callers import only `embedding`.
- `NewEmbedder(ctx, cfg)` factory returns the configured provider — currently always `ollama.New`. Adding a provider means a new subpackage plus a factory branch, with no caller changes.
- `embedding/ollama/` implements the interface against the Ollama HTTP API using `OLLAMA_`-prefixed config (host, model — default `nomic-embed-text`).

The ingest pipeline ([SPEC-001](SPEC-001-source-ingestion-pipeline.md)) calls `EmbedBatch` per file, validates the result (one non-empty vector per chunk), and stamps `chunks.embedding_model` in Postgres after a successful Qdrant upsert.

##### Affected components
- `api/internal/embedding/` — interface, factory, `types/`, `ollama/`
- `api/internal/config/` — `Ollama` struct (`OLLAMA_` prefix)
- `docker-compose.yml` — `ollama` service; `ollama pull nomic-embed-text` required for local dev

##### Open questions
- How is an embedding-model change handled for existing data — re-embed all chunks (using `chunks.embedding_model` to find stale ones), or maintain one Qdrant collection per model?
- Should the factory select the provider from config (e.g. `EMBEDDING_PROVIDER`) once a second provider exists, and where does vector-dimension validation live then?
- Do query-time embeddings (retrieval) reuse this same interface and instance? (Assumed yes — see [SPEC-006](SPEC-006-retrieval-engine.md).)

##### Acceptance criteria
- `EmbedBatch` returns exactly one embedding per input chunk, in order, each carrying the originating chunk ID.
- Indexed chunks have `embedding_model` set to the configured model name in Postgres.
- Swapping the provider requires touching only the `embedding` package (new subpackage + factory), not the `sources` pipeline.

##### Related (Optional)
- [SPEC-001](SPEC-001-source-ingestion-pipeline.md) — pipeline consumer
- [SPEC-004](SPEC-004-vector-storage.md) — where the vectors land
