#### SPEC-001: Source ingestion pipeline

##### Status
Implemented

##### Problem statement
Users need to bring their own knowledge (local files today; Obsidian vaults, Git repositories, and PDFs later) into NeuralVault so it can be retrieved as context for LLM queries. Ingestion must accept uploads, keep the original files durably, and index them asynchronously without blocking the HTTP request.

##### Goals
- Accept multipart file uploads scoped to a workspace and persist the originals in S3-compatible object storage (MinIO).
- Index sources in the background (chunk → embed → store vectors) and expose progress to clients in real time.
- Support re-indexing an existing source from the stored originals without re-uploading.

##### Non-goals
- Retrieval/search over indexed content (see [SPEC-006](SPEC-006-retrieval-engine.md)).
- Non-file source types (Git, web) — the `model.Source` type field and per-type metadata structs anticipate them, but only `file` sources are implemented.
- Durable job queue semantics — indexing runs in an in-process goroutine; a queue (RabbitMQ) is a future infrastructure item in `docs/architecture.md`.

##### Proposed design
The `sources` domain (`api/internal/sources/`) follows the repo's handler/service/routes layout and drives the whole pipeline:

- `POST /sources` — `Service.Create` writes each upload to a temp dir, uploads it to MinIO under the key `<workspace_id>/<source_id>/<filename>`, inserts a `sources` row with status `indexing`, returns `202 Accepted`, and spawns `indexInBackground`.
- `POST /sources/{id}/ingest` — `Service.Ingest` deletes the source's existing chunks, re-downloads the originals from MinIO, and re-runs the pipeline in the background.
- `GET /sources/{id}/status` — SSE stream fed by `ProgressBus` (`bus.go`): one `indexing` event per file processed, then `done` with the total chunk count, or `error`. Heartbeat every 30s, stream timeout 15min.
- `GET /sources?workspace_id=` and `GET /sources/{id}/chunks` — listing endpoints.

`runPipeline` (in `service.go`) is the core sequence: `sourcereader.Reader.Read` maps files to `chunking.ChunkRequest` values → `ChunkService.ChunkSource` persists chunks → `Embedder.EmbedBatch` generates vectors → validation (count match, non-empty vectors, non-nil UUIDs) → `vectorstorage.Client.Upsert` into Qdrant with a minimal payload (`chunk_id`, `workspace_id`, `source_id`) → `chunks.embedding_model` batch-updated in Postgres. Background goroutines carry a 10-minute timeout so a stuck pipeline never leaks; on failure the source status becomes `error`.

##### Affected components
- `api/internal/sources/` — handler, service, routes, progress bus
- `api/internal/objectstorage/` — MinIO-backed `Client` (`Upload`, `Download`, `ListObjects`); `ensureBucket` on startup
- `api/internal/sourcereader/` — `Reader` interface + `FileReader` (walks a directory, infers content type)
- Depends on [SPEC-002](SPEC-002-chunking.md), [SPEC-003](SPEC-003-embedding-generation.md), [SPEC-004](SPEC-004-vector-storage.md), [SPEC-005](SPEC-005-relational-persistence.md)

##### Open questions
- When does ingestion move from in-process goroutines to a durable queue, and what delivery guarantees do we need for large sources?
- How will non-file source types (Git, Obsidian, web) plug into `sourcereader` — one `Reader` per type selected by `model.SourceType`?
- Should partially indexed sources be resumable instead of restarted from scratch on `ingest`?

##### Acceptance criteria
- Uploading files via `POST /sources` returns `202` with the source in `indexing` status, and the originals appear in MinIO under the workspace/source prefix.
- `GET /sources/{id}/status` streams per-file progress and terminates with `done` (status `indexed`) or `error`.
- `POST /sources/{id}/ingest` replaces the source's chunks and vectors with freshly indexed ones.

##### Related (Optional)
- [ADR-003](../adr/ADR-003-core-vector-database-decision.md) — Qdrant as the vector database
- [SPEC-002](SPEC-002-chunking.md), [SPEC-003](SPEC-003-embedding-generation.md), [SPEC-004](SPEC-004-vector-storage.md), [SPEC-005](SPEC-005-relational-persistence.md)
