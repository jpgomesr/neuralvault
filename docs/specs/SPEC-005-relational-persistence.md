#### SPEC-005: Relational persistence and data model

##### Status
Implemented

##### Problem statement
NeuralVault needs durable, transactional storage for everything that is not a vector: users, workspaces, sources, chunk text and metadata, and pipeline state. [ADR-002](../adr/ADR-002-core-database-decision.md) selected PostgreSQL; this spec describes the access layer and the domain model built on it.

##### Goals
- A thin `Pool` interface over `pgxpool` so services depend on an abstraction they can fake in tests. `Pool`'s own signatures declare locally-owned `Rows`/`Row`/`Tx`/`CommandTag` interface types rather than naming `pgx`/`pgconn` directly ([ADR-008](../adr/ADR-008-storage-vectorstorage-interface-abstraction.md)); services still scan rows themselves — this is not an ORM.
- A multi-tenant data model: every source and chunk is scoped to a workspace from day one.
- Schema changes tracked as numbered SQL migrations, applied by a dedicated binary.

##### Non-goals
- An ORM or repository layer — services write SQL directly against `Pool` (see `sources/service.go`, `chunking/service.go`).
- Authentication/authorization logic — the schema anticipates it (`user_identity`), but no auth is enforced yet (see [SPEC-009](SPEC-009-platform-cross-cutting.md)).

##### Proposed design
- `Pool` interface (`api/internal/storage/storage.go`): `Exec`, `Query`, `QueryRow`, `Begin`, `Ping`, `Close`. `NewPool(ctx, cfg)` returns the `pgxpool`-backed implementation in `storage/postgres/` using `POSTGRES_`-prefixed config.
- Migrations live in `storage/postgres/migrations/` as numbered SQL files (`00001_create_user.sql` … `00006_create_chunks.sql`) and are applied via `api/cmd/migrate`.
- Domain structs in `api/internal/model/` map 1:1 to tables: `User`, `Workspace`, `UserWorkspace` (membership join), `UserIdentity` (external identity providers), `Source` (type, URI, status `indexing|indexed|error`, per-type JSON metadata), `Chunk` (content, `chunk_index`, JSON location metadata, `embedding_model`; its UUID doubles as the Qdrant point ID).
- Per-type metadata is `json.RawMessage` with typed structs per source kind (`FileSourceMetadata`, `FileChunkMetadata`, plus `GitChunkMetadata`/`WebChunkMetadata` reserved for future types) — new source types extend metadata without schema changes.

##### Affected components
- `api/internal/storage/` — `Pool` interface + `postgres/` implementation and `migrations/`
- `api/internal/model/` — domain structs
- `api/cmd/migrate/` — migration runner
- `docker-compose.yml` — `postgres` service

##### Open questions
- Where should workspace-scoping be enforced as the API surface grows — per-query `WHERE workspace_id =` (current), middleware, or Postgres RLS? [SPEC-011](SPEC-011-auth-workspaces-tenant-isolation.md) proposes application-layer enforcement.
- Does chunk `content` belong in Postgres long-term, or should very large corpora keep text only in object storage with Postgres holding offsets?
- Cascade semantics: what happens to chunks and Qdrant points when a source or workspace is deleted? (Today only re-ingestion deletes chunks.)

##### Acceptance criteria
- `go run ./cmd/migrate` brings an empty database to the current schema; migrations are strictly ordered and re-runnable per the runner's semantics.
- All source/chunk queries are workspace- or source-scoped — no cross-tenant reads.
- Services depend only on `storage.Pool`, never on `pgxpool` directly.

##### Related (Optional)
- [ADR-002](../adr/ADR-002-core-database-decision.md) — why PostgreSQL
- [ADR-008](../adr/ADR-008-storage-vectorstorage-interface-abstraction.md) — `Pool` declares local `Rows`/`Row`/`Tx`/`CommandTag` types instead of leaking `pgx`/`pgconn`
- [SPEC-001](SPEC-001-source-ingestion-pipeline.md), [SPEC-002](SPEC-002-chunking.md), [SPEC-009](SPEC-009-platform-cross-cutting.md)
