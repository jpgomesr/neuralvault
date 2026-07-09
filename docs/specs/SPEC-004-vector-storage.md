#### SPEC-004: Vector storage

##### Status
Implemented

##### Problem statement
Chunk embeddings need a store that supports fast approximate nearest-neighbour search with metadata filtering, self-hosted alongside the rest of the stack. [ADR-003](../adr/ADR-003-core-vector-database-decision.md) selected Qdrant; this spec describes how the codebase talks to it.

##### Goals
- A single `Client` interface wrapping all vector-database operations, so business logic never imports the Qdrant SDK's connection handling directly.
- Collection lifecycle managed at startup — the API never assumes the collection exists.
- Every stored point carries the IDs needed for workspace-scoped filtering.
- `Client` method signatures speak package-level domain types rather than `qdrant/go-client` protobuf types ([ADR-008](../adr/ADR-008-storage-vectorstorage-interface-abstraction.md)), so consumers never import the Qdrant SDK.

##### Non-goals
- Query-side retrieval logic — ranking, filters, hybrid search live in the retrieval engine ([SPEC-006](SPEC-006-retrieval-engine.md)); this layer only exposes the raw `Query` primitive.

##### Proposed design
`api/internal/vectorstorage/` follows the interface-at-root, provider-in-subpackage pattern:

- `Client` interface (`vectorstorage.go`): `HealthCheck`, `CollectionExists`, `CreateCollection`, `DeleteCollection`, `Upsert`, `Query`, `Delete`, `Count`, `Close`.
- `NewClient(ctx, cfg)` returns the Qdrant-backed implementation (`vectorstorage/qdrant/`, connection pool over gRPC, `QDRANT_`-prefixed config).
- `ensureCollection` runs on startup: creates the configured collection if missing, dimensioned for the configured embedding model.
- Point schema (written by the ingest pipeline, [SPEC-001](SPEC-001-source-ingestion-pipeline.md)): point ID = chunk UUID (1:1 with the `chunks` row); payload holds only `chunk_id`, `workspace_id`, `source_id`. Chunk text and rich metadata stay in Postgres — Qdrant stores vectors and filter keys, nothing more.

##### Affected components
- `api/internal/vectorstorage/` — interface + `qdrant/` implementation
- `api/internal/config/` — `Qdrant` struct (`QDRANT_` prefix)
- `docker-compose.yml` — `qdrant` service

##### Open questions
- Are payload indexes on `workspace_id`/`source_id` needed once collections grow, and who creates them (`ensureCollection`?)?
- One shared collection with payload filtering vs. per-workspace collections as tenant isolation hardens?
- How are vector-dimension changes (new embedding model) migrated — new collection + re-embed, or versioned collection names?

##### Acceptance criteria
- On startup the configured collection exists (created if missing) and `HealthCheck` succeeds.
- After ingestion, `Count` for the collection matches the number of persisted chunks, and each point's payload contains exactly `chunk_id`, `workspace_id`, `source_id`.
- Deleting a source's points via `Delete` leaves other workspaces' points untouched.

##### Related (Optional)
- [ADR-003](../adr/ADR-003-core-vector-database-decision.md) — why Qdrant
- [ADR-008](../adr/ADR-008-storage-vectorstorage-interface-abstraction.md) — `Client` speaks domain types instead of Qdrant protobuf (supersedes the former "Hiding Qdrant protobuf types" Non-goal)
- [SPEC-001](SPEC-001-source-ingestion-pipeline.md), [SPEC-003](SPEC-003-embedding-generation.md), [SPEC-006](SPEC-006-retrieval-engine.md)
