# NeuralVault anti-patterns

Convention violations specific to this codebase — not general Go/security bugs (those are `/code-review`'s job). Used by `/review`. Agent-only: not linked from public docs.

## 1. Redeclaring entity shape outside `internal/model/`

Canonical entities (`Chunk`, `Source`, `Workspace`, `User`, `UserIdentity`, `UserWorkspace`) live in `api/internal/model/` with `db:` tags. A domain package must reuse those types, not redeclare a struct with the same fields (e.g. a second `type Source struct { ID uuid.UUID; WorkspaceID uuid.UUID; ... }` inside `internal/sources/`).

**Look for:** a new struct in a domain package whose fields substantially overlap an existing `model.*` type.

**Fix:** reuse `model.X`, or embed it, in the domain-specific request/response type.

## 2. Mixing persistence tags into request/param DTOs

Operation-scoped structs (`ChunkRequest`, `CreateRequest`, `FileUpload`) live in each domain's `service.go` and describe call parameters, not database rows — they must not carry `db:` tags. Conversely, `internal/model/` types must not gain `json:` tags or API-response-shaping fields; if a response needs a different shape than the DB row, that shape is a separate type in the domain package.

**Look for:** `db:` tags outside `internal/model/`, or fields added to a `model.*` struct that only exist for one HTTP response.

## 3. Bypassing the service layer

Handlers call the domain's `Service` interface only. A handler must never import `internal/storage`, `internal/vectorstorage`, or `internal/objectstorage` directly — that logic belongs in `service.go`.

**Look for:** `storage.Pool`, `vectorstorage.Client`, or `objectstorage.Client` referenced from a `handler.go`.

## 4. Breaking the three-file domain layout

Every feature domain under `api/internal/<domain>/` is `handler.go` (HTTP), `service.go` (interface + logic), `routes.go` (Chi subrouter) — see AGENTS.md. Don't add a fourth catch-all file (`utils.go`, `helpers.go`) for logic that belongs in one of the three; don't put route mounting in `handler.go`.

**Look for:** business logic in `handler.go`, HTTP status/response writing in `service.go`, route definitions outside `routes.go`.

## 5. Terminology drift

Use the exact terms in [`glossary.md`](./glossary.md) in code identifiers, comments, commit messages, issues, and PRs. Don't introduce a synonym for a concept that already has a canonical name (e.g. calling a Source a "document" or "connector" in a new PR).

**Look for:** new identifiers, doc strings, or PR text that reintroduce a term listed as "do not use" in the glossary.

## Adding a new anti-pattern

Add an entry here when a review catches the same convention violation twice — one occurrence is a mistake, a second is a pattern worth codifying.
