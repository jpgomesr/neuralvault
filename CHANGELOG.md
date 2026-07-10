# Changelog

All notable changes to NeuralVault will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
NeuralVault uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

---

## [0.1.0] - 2026-07-10

> **Pre-release.** Marks the completion of Phase 1 (core retrieval pipeline) for historical reference. **Not production-ready** — see Known limitations below.

### Added
- Go API foundation (Chi router, health endpoint that probes all infrastructure dependencies, PostgreSQL migrations)
- Chunking engine (markdown section and plain-text splitters), with chunk indexes offset across files to avoid collisions
- Embedding generation via Ollama (`nomic-embed-text`)
- Qdrant vector storage and collection management
- Source ingestion pipeline — upload to MinIO, background indexing, SSE progress stream, guarded against concurrent re-ingestion with stale-vector cleanup and relative-path chunk metadata
- Retrieval engine (`POST /query`) and streaming grounded answers (`POST /query/stream`)
- LLM provider layer with Ollama backend (`Complete` / `Stream`), including usage cache-accounting (ADR-007)
- OIDC authentication (Keycloak dev realm) and native login via ROPC backend proxy, JIT user provisioning, and `RequireUser` session middleware
- Workspace management with membership-enforced tenant isolation across ingest, chunk, and status routes
- Next.js chat UI (Tailwind + shadcn/ui, TanStack Query) — sign-in, workspace switcher, streaming chat, folder upload with live indexing status, indexed-file preview
- CLI (`nv ingest` / `nv query`) as a plain HTTP client of the API
- HTTP server hardened against slow clients, panics, and large uploads

### Known limitations
This release is for historical/development reference only — **do not deploy publicly or use with untrusted content**. Open issues at time of tagging:
- [#144](https://github.com/jpgomesr/neuralvault/issues/144) — stored XSS via source file content served without `nosniff`
- [#63](https://github.com/jpgomesr/neuralvault/issues/63) — server temp-dir path leaked in source metadata
- [#44](https://github.com/jpgomesr/neuralvault/issues/44) — indexing goroutines are unbounded, no concurrency semaphore
- [#49](https://github.com/jpgomesr/neuralvault/issues/49) — ingestion pipeline is not yet parallelized/batched for large sources
- [#67](https://github.com/jpgomesr/neuralvault/issues/67) — internal error details (DB/storage) can leak to API clients

---