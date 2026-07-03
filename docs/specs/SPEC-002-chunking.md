#### SPEC-002: Chunking

##### Status
Implemented

##### Problem statement
Raw source content is too large and unstructured to embed or retrieve as-is. It must be split into ordered, semantically coherent spans that carry enough metadata (file path, heading, line range) to locate each chunk back in its source.

##### Goals
- A single `Splitter` abstraction so new content formats can be added without touching business logic.
- Format-aware splitting: markdown by section structure, plain text as fallback.
- Persist chunks transactionally with per-chunk location metadata for later citation and filtering.

##### Non-goals
- Chunk overlap, token-budgeted sizing, or semantic (embedding-based) chunking — current splitters are structural.
- Choosing the embedding model or storing vectors (see [SPEC-003](SPEC-003-embedding-generation.md) and [SPEC-004](SPEC-004-vector-storage.md)).

##### Proposed design
`api/internal/chunking/` defines the contract and the service:

- `Splitter` interface (`chunking.go`): `Split(ctx, text) ([]Span, error)`. A `Span` is a contiguous, non-overlapping slice of source text with optional `Heading`, `Level` (ATX 1–6), and 1-based `StartLine`/`EndLine`. Implementations must be safe for concurrent use.
- Concrete splitters: `chunking/markdown/` (splits by heading sections, populates heading metadata) and `chunking/text/` (plain text).
- `ChunkService` (`service.go`) holds a `map[ContentType]Splitter` (`markdown`, `plaintext`) injected in `router/router.go`. `ChunkSource(ctx, ChunkRequest)` picks the splitter by content type, builds `model.Chunk` values (fresh UUID, `chunk_index` in order, `FileChunkMetadata` JSON), and inserts them in a single Postgres transaction. `ListChunks` returns a source's chunks ordered by `chunk_index`; `DeleteChunks` removes them (used by re-ingestion).

`model.Chunk.ID` doubles as the Qdrant point ID, establishing the 1:1 row↔vector mapping consumed by [SPEC-001](SPEC-001-source-ingestion-pipeline.md).

##### Affected components
- `api/internal/chunking/` — interface, service, `markdown/` and `text/` splitters
- `api/internal/model/chunk.go` — `Chunk`, `FileChunkMetadata` (plus `GitChunkMetadata`/`WebChunkMetadata` reserved for future source types)
- `api/internal/storage/postgres/migrations/00006_create_chunks.sql`

##### Open questions
- Do markdown sections need a maximum size (token budget) with intra-section splitting for very long sections?
- Should chunk overlap be introduced for retrieval quality, and if so at the splitter or service level?
- Which splitter handles code files when Git sources land — per-language splitters or a generic one keyed by extension?

##### Acceptance criteria
- Chunking a markdown file yields one chunk per section with heading, level, and line-range metadata; plain text files yield ordered plaintext chunks.
- All chunks of a `ChunkSource` call are persisted atomically — a failed insert leaves no partial chunks.
- An unsupported content type returns an error rather than silently skipping the file.

##### Related (Optional)
- [SPEC-001](SPEC-001-source-ingestion-pipeline.md) — consumes `ChunkService` in the ingest pipeline
- [SPEC-005](SPEC-005-relational-persistence.md) — chunks table and metadata columns
