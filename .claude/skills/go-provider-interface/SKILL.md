---
name: go-provider-interface
description: This skill should be used when adding a new backend to embedding, llm, vectorstorage, objectstorage, or storage (e.g. "add an OpenAI provider", "add a new LLM backend", "swap the vector database"), or when asked how NeuralVault's pluggable-provider pattern works. Documents the interface-package shape, the fail-fast startup convention, and two known exceptions that look like bugs but aren't.
---

# Pluggable provider interface pattern

`embedding`, `llm`, `vectorstorage`, `objectstorage`, and `storage` all follow the same shape:

```
internal/<domain>/
    <domain>.go          # interface + New<Domain> factory — no business logic, no concrete-provider imports
    types/                # shared value types, exists ONLY to break the interface↔provider import cycle
    <provider>/           # one subpackage per concrete backend
        <provider>.go
```

`embedding.go`/`llm.go` re-export their `types/` package's types as aliases so callers never import `types/` directly. This is the same reason `embedding/types` and `llm/types` both exist — without it, the domain package would need to import its own provider subpackage to reference shared types, creating a cycle.

## Current state: one provider each

Every interface has exactly one concrete implementation wired today (Ollama for `embedding`/`llm`, Qdrant, MinIO, Postgres). `NewEmbedder`/`NewProvider` **unconditionally** return the Ollama implementation — there is no provider-selection branch and no stub package for OpenAI/Claude/Gemini/Qwen/DeepSeek, despite AGENTS.md's "Providers" section listing them as the eventual set. Adding a new provider means creating both the subpackage AND the selection branch in the factory from scratch — there's nothing partially built to extend.

## Fail-fast on startup, not first use

Every provider's `New*` constructor validates its backing infrastructure is ready and returns an error immediately if not — it does not defer the check to first use:

- `vectorstorage/qdrant`: `ensureCollection` auto-creates the collection (cosine distance) if missing
- `objectstorage/minio`: `ensureBucket` auto-creates the bucket if `HeadBucket` returns `NotFound`
- `embedding/ollama` and `llm/ollama`: both call `/api/tags` at construction and fail if the configured model isn't pulled

Practically: a missing `ollama pull nomic-embed-text` breaks server *startup*, not the first embedding call. Keep this pattern when adding a new provider — validate at `New*`, not lazily.

## Two known exceptions — don't "fix" them without checking first

- **`chunking` has no `New*` factory.** Unlike the five packages above, its splitter selection (`map[chunking.ContentType]Splitter`) is built inline in `router/router.go`, not behind a factory function. This is a known inconsistency, not an oversight to silently correct mid-task.
- **`sourcereader.NewReader`'s type-dispatch factory is dead code.** It exists and correctly returns an error for unsupported source types (git/web), but `router/router.go` bypasses it and wires a hardcoded `FileReader` directly into `NewSourceService`. If a task involves adding a new source reader, the dispatch logic in `sourcereader.go` needs to actually be wired in `router.go` — check whether that wiring is in scope before assuming `NewReader` is already live.
