#### ADR-008: Make `storage.Pool` and `vectorstorage.Client` speak local types, and extract an `Indexer` from `SourceService`

##### Status
Proposed

##### Context
Two of the repo's "interface-at-root, provider-in-subpackage" packages define interfaces that are
pass-throughs over their driver rather than abstractions over it, and the source-ingestion service
that consumes them has grown three distinct responsibilities behind a single constructor.

`storage.Pool` (`api/internal/storage/storage.go`) returns raw driver types in four of its six
methods:

```go
type Pool interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
	Ping(ctx context.Context) error
	Close()
}
```

`vectorstorage.Client` (`api/internal/vectorstorage/vectorstorage.go`) takes or returns
`qdrant/go-client` protobuf types in six of its nine methods (`*qdrantpb.CreateCollection`,
`*qdrantpb.UpsertPoints`, `*qdrantpb.QueryPoints`, `*qdrantpb.ScoredPoint`, `*qdrantpb.DeletePoints`,
`*qdrantpb.CountPoints`, `*qdrantpb.UpdateResult`, `*qdrantpb.HealthCheckReply`). A comment above the
interface acknowledges this as an intentional tradeoff: *"method signatures use qdrant protobuf types
since the project uses Qdrant as its vector store. Swap the implementation in NewClient without
changing this interface if the types remain compatible."* [SPEC-004](../specs/SPEC-004-vector-storage.md)
records the same call as a Non-goal ("Hiding Qdrant protobuf types"). The consequence is that
`sources/service.go` and `retrieval/service.go` construct and consume Qdrant protobuf structs
directly in the service layer, and their test fakes are typed in terms of those protobuf types.

Both interfaces diverge from the house convention `CONTRIBUTING.md` documents for `embedding/` and
`llm/` — the root `<domain>.go` file defines "interface + domain types only" and no consumer imports
a concrete backend. Today `storage`'s and `vectorstorage`'s consumers effectively import the backend
by importing its types.

Separately, `SourceService` (`api/internal/sources/service.go`) takes nine constructor dependencies,
including two loose strings (`collectionName`, `embeddingModel`) that are pure pass-through config
(no logic branches on either). Its methods split cleanly into three groups: HTTP-shaped CRUD
(`List`, `GetByID`, `ListFiles`, `OpenFile`, …), pipeline orchestration (`runPipeline`,
`indexInBackground`, `reingestInBackground`, `updateEmbeddingModel`), and Qdrant point construction
(`upsertChunkVectors`, `deleteSourceVectors`, `toEmbeddingChunks`). The vector-upsert responsibility
is the source of both the protobuf coupling and two of the nine constructor arguments.

This ADR records the decision requested in issue #69; the code refactor it describes is deferred to
follow-up issues (see Consequences).

##### Decision
Adopt "domain/local types" (issue #69's Option B) for **both** interfaces, and extract an `Indexer`
component from `SourceService` (issue #69's Option C).

- **`storage.Pool`** will declare locally-owned interfaces (`Rows`, `Row`, `Tx`, and a
  `CommandTag`-shaped result exposing `RowsAffected() int64`) mirroring the minimal method set
  consumers already use, so the exported `Pool` interface no longer names `pgx`/`pgconn` types. This
  is deliberately **not** a row-scanning/mapping layer — callers still invoke `.Scan(...)` on the
  returned rows themselves — so [SPEC-005](../specs/SPEC-005-relational-persistence.md)'s "no ORM /
  repository layer" Non-goal is preserved. The pgx values the postgres implementation already returns
  satisfy these narrower local interfaces structurally, so no wrapper types are expected.

- **`vectorstorage.Client`** will speak package-level domain types (e.g. a `VectorPoint` built on
  `model.Chunk`'s existing "UUID doubles as the Qdrant point ID" convention, plus a scored-result
  type for `Query` to replace `[]*qdrantpb.ScoredPoint`). The `qdrant/` subpackage adapts between
  those domain types and protobuf internally. This **supersedes** SPEC-004's "Hiding Qdrant protobuf
  types" Non-goal and its interface comment.

- **`Indexer`** will be a new component owning the vector-upsert responsibility currently in
  `SourceService`: `runPipeline`, `upsertChunkVectors`, `deleteSourceVectors`, `toEmbeddingChunks`,
  `updateEmbeddingModel`, and the `collectionName`/`embeddingModel` config. `SourceService` keeps
  CRUD and delegates indexing to the `Indexer` dependency, dropping from nine constructor arguments.

##### Consequences

###### Positive
- Both interfaces become genuinely substitutable: `pgx`/`pgconn` and Qdrant protobuf types stop
  leaking into consumers (`auth`, `chunking`, `sources`, `retrieval`, `workspaces`) and their test
  fakes.
- Brings `storage/` and `vectorstorage/` in line with the `embedding/`/`llm/` "interface + domain
  types only" convention documented in `CONTRIBUTING.md`.
- Resolves the SRP problem in `SourceService`: the protobuf coupling and two of its nine constructor
  arguments move out with the extracted `Indexer`, leaving a smaller, CRUD-focused service.

###### Negative
- Upfront cost: new local/domain types plus adapter code in `storage/postgres/` and
  `vectorstorage/qdrant/`, and rewiring five services, `router.NewRouter`, and ~12 tests in
  `sources/service_test.go` that reach into the now-relocated unexported pipeline methods. None of
  this is done in this ADR — it is deferred to follow-up issue(s) referencing ADR-008.
- This abstracts `vectorstorage.Client` **on speculation**: no second Qdrant-compatible backend is
  actually being built. That is the same situation [ADR-007](ADR-007-defer-prompt-caching-boundary.md)
  treated as a reason *not* to add an abstraction ("with zero cloud adapters built … there is no
  real usage pattern to generalize an abstraction from"). This ADR consciously diverges from that
  precedent, betting the convention-alignment and decoupling benefits are worth the cost even before
  a second backend exists; the tension is noted rather than hidden.
- SPEC-004 and SPEC-005 are updated to reflect the new direction, so their current "Implemented"
  status will temporarily lead the code until the refactor lands.

##### Related decision (Optional)
- [ADR-003](ADR-003-core-vector-database-decision.md) — selected Qdrant; this ADR decides how the
  codebase's own interface relates to that SDK.
- [ADR-007](ADR-007-defer-prompt-caching-boundary.md) — contrasting precedent for *not* abstracting
  ahead of a concrete second implementation (see Negative consequences).
- [SPEC-004](../specs/SPEC-004-vector-storage.md) — its "Hiding Qdrant protobuf types" Non-goal is
  superseded by this decision.
- [SPEC-005](../specs/SPEC-005-relational-persistence.md) — its thin-`Pool` Goal is refined to
  local-interface types by this decision.
- Issue #69 — the discussion this ADR resolves.
