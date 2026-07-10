---
name: verify
description: This skill should be used before committing a nontrivial NeuralVault change, or when asked "verify this works", "check this end-to-end", "does this pass CI". Mirrors NeuralVault's actual CI gates (ci-api.yml, ci-web.yml, the swagger-drift check, test-coverage-web.yml) and describes how to exercise the affected flow live, not just run tests.
---

# Verifying a NeuralVault change

Two parts. Running only the first without the second is not a complete verification — CI passing doesn't mean the feature works.

## 1. Automated checks (mirror CI exactly)

Run only the side(s) that changed.

**Backend (`api/`)** — mirrors `ci-api.yml`:

```bash
cd api
go test ./... -race
golangci-lint run
go build ./cmd/server
```

If a `swaggo` annotation changed (a handler doc comment), also check for swagger drift — CI fails the build if `api/docs` is stale:

```bash
make swag
git diff --quiet -- api/docs || echo "swagger docs are stale — commit the regenerated api/docs"
```

Per AGENTS.md, only run `make swag` when annotations actually changed, not routinely.

**Frontend (`web/`)** — mirrors `ci-web.yml` + `test-coverage-web.yml`:

```bash
cd web
npm run lint
npm run type-check
npm run test          # vitest run — real, CI-enforced, not listed in AGENTS.md's own command block historically
npm run build
```

## 2. Drive the actual flow

Tests and lint confirm the code compiles and known cases don't regress — they don't confirm the feature works. Start the app (see the `run` skill) and exercise the specific path the change touches:

- **Sources/ingestion change** → upload a file via the UI or `POST /sources`, watch `GET /sources/{id}/status` (SSE) reach `indexed`, confirm chunks via `GET /sources/{id}/chunks`
- **Retrieval/query change** → ask a question via the chat UI or `POST /query` / `POST /query/stream`, confirm the answer is grounded in real chunks with correct source citations
- **Auth/workspace change** → sign out and back in as the seeded `dev` user, confirm the session cookie and workspace membership guard (403 on cross-workspace access) behave as expected
- **Config/provider change** → restart the API and confirm it starts cleanly with the new config; if a provider was swapped, confirm the concrete implementation is wired only in `cmd/server/main.go`

Skip this step only for changes with no runtime surface (docs-only, test-only diffs).
