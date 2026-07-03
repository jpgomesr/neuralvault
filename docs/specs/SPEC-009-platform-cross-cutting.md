#### SPEC-009: Platform and cross-cutting concerns

##### Status
Implemented

##### Problem statement
Every feature domain needs the same foundations: configuration, logging, HTTP wiring, health reporting, and API documentation. These cross-cutting concerns must be consistent and centralized so domains stay thin and uniform.

##### Goals
- One config load path with validation, environment-prefixed variables, and env-file layering for local development.
- Structured, machine-parseable logging with a single global initialisation.
- A uniform way to add a feature domain: handler/service/routes wired in one place.
- Liveness/health visibility and always-in-sync API docs.

##### Non-goals
- Authentication and authorization — the data model anticipates it (`user_identity`, workspaces in [SPEC-005](SPEC-005-relational-persistence.md)) but no middleware enforces identity yet; this is an acknowledged gap.
- The frontend (`web/`, Next.js) — planned, not yet in the repository.

##### Proposed design
- **Config** (`api/internal/config/`): loaded once via `sync.Once`, validated with `go-playground/validator`; `kelseyhightower/envconfig` maps `<PREFIX>_<FIELD>` env vars to structs — `SERVER_`, `POSTGRES_`, `QDRANT_`, `OLLAMA_`, `MINIO_`. In non-production, `.env` then `.env.<SERVER_ENV>` are loaded; production uses system env only. The prefix table is mirrored in AGENTS.md and `env.example` (kept in sync by convention).
- **Logging** (`api/internal/logger/`): global `log/slog` JSON logger to stdout, initialised once in `cmd/server/main.go` via `logger.Init(level)`.
- **Router** (`api/internal/router/router.go`): the composition root — instantiates pools/clients/services (Postgres, Qdrant, MinIO, embedder, splitters), then mounts each domain with `r.Mount("/<path>", domain.Routes(handler))`. New domains follow the three-file pattern (`handler.go`, `service.go`, `routes.go`).
- **Health** (`api/internal/health/`): system status endpoint at `/health`.
- **Swagger**: `swaggo/swag` annotations on handlers, served at `/swagger/`; regenerated only via `make swag`; CI fails if `api/docs` drifts from annotations.
- **Entrypoints**: `api/cmd/server` (API on :8080) and `api/cmd/migrate` (migrations). Local infra via `docker compose up -d qdrant postgres ollama minio`.

##### Affected components
- `api/internal/config/`, `api/internal/logger/`, `api/internal/router/`, `api/internal/health/`
- `api/cmd/server/`, `api/cmd/migrate/`
- `docker-compose.yml`, `Makefile`, `.github/workflows/ci-api.yml`, `env.example`

##### Open questions
- Auth design: session vs. token, which identity providers `user_identity` will back, and where workspace membership is enforced (middleware vs. per-service).
- Does `/health` grow dependency checks (Postgres/Qdrant/MinIO/Ollama reachability) for orchestration readiness probes?
- Observability beyond logs — metrics/tracing when the retrieval path needs latency budgets (PostHog is slated for product analytics, not ops).

##### Acceptance criteria
- The API starts with a valid `.env`, fails fast with a clear validation error on missing config, and serves `/health` and `/swagger/`.
- Adding a domain requires only the three files plus one `r.Mount` line in `router.go`.
- CI enforces lint, race-enabled tests, build, and swagger sync on every PR.

##### Related (Optional)
- [ADR-001](../adr/ADR-001-core-language-decision.md) — why Go
- [SPEC-005](SPEC-005-relational-persistence.md) — data-model side of multi-tenancy
