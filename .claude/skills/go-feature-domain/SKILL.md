---
name: go-feature-domain
description: This skill should be used when adding or modifying a feature domain under api/internal/ (auth, health, retrieval, sources, workspaces, or a new one), or when asked "how do I add a new endpoint/domain", "where does this logic go", "how are Go tests structured here". Documents the three-file domain layout, its justified exceptions, and the testing conventions.
---

# Go feature domain layout

Every feature domain under `api/internal/<domain>/` is exactly three files, per AGENTS.md: `handler.go` (HTTP layer), `service.go` (interface + business logic), `routes.go` (Chi subrouter). A handler calls its own `Service` interface only — never `internal/storage`, `internal/vectorstorage`, or `internal/objectstorage` directly (anti-pattern #3 in `.claude/anti-patterns.md`).

Mount a new domain in `router/router.go`:

```go
r.Mount("/<path>", domain.Routes(handler))
```

## Justified 4th files

Anti-pattern #4 forbids a catch-all `utils.go`/`helpers.go` for logic that belongs in one of the three files — but a few domains legitimately need a fourth file because the logic doesn't fit the HTTP/service split at all:

- **`auth/session.go`** + **`auth/middleware.go`** — session token signing (`sessionSigner`) and `RequireUser` are cross-cutting concerns other domains import, not this domain's own request/response handling
- **`sources/bus.go`** — `ProgressBus`, an in-memory pub/sub for SSE progress; it's infrastructure for one handler's streaming response, not business logic
- **`workspaces/guard.go`** — `EnsureMember`, a plain function (not middleware — see the `workspaces` skill) that other domains' handlers call directly

When adding a domain, only reach for a 4th file if the new code is similarly cross-cutting or infrastructural — if it's request handling or business logic, it belongs in one of the three standard files.

## Testing conventions

- Table-driven tests with `t.Run(tt.name, ...)` — the standard shape across the codebase (e.g. `api/internal/health/service_test.go`)
- `resetGlobals()` is **not** a project-wide helper — it's scoped to `api/internal/config/config_test.go` only, resetting that package's `sync.Once`-guarded singleton between cases. Don't expect or add an equivalent in other packages unless they have the same singleton-init problem
- No mocks for external services — integration tests hit real Qdrant/Postgres via Docker and are named `*_integration_test.go` (e.g. `ollama_integration_test.go`)

## Middleware order

`router/middleware.go` applies `RequestID` → `requestLogging` → `Recoverer`, in that order, deliberately. `requestLogging`'s "request completed" line must run after `Recoverer` writes the 500 response for a panicking handler — reversing the order would log completion before the panic response is written. Don't reorder this without understanding why it's sequenced this way.
