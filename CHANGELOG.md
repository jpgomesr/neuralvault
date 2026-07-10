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
- Go API foundation (Chi router, health endpoint, PostgreSQL migrations)
- Chunking engine (markdown section and plain-text splitters)
- Embedding generation via Ollama (`nomic-embed-text`)
- Qdrant vector storage and collection management
- Source ingestion pipeline — upload to MinIO, background indexing, SSE progress stream
- Retrieval engine (`POST /query`) and streaming grounded answers (`POST /query/stream`)
- LLM provider layer with Ollama backend (`Complete` / `Stream`)
- OIDC authentication (Keycloak dev realm), JIT user provisioning, and `RequireUser` session middleware
- Workspace management with membership-enforced tenant isolation
- Next.js chat UI — OIDC sign-in, workspace switcher, streaming chat, file upload with live indexing status
- CLI (`nv ingest` / `nv query`) as a plain HTTP client of the API
- Native login via ROPC backend proxy, alongside OIDC sign-in
- Tailwind + shadcn/ui component primitives for the web UI
- TanStack Query for server state management in the web UI
- Preview of indexed files with preserved paths and metadata
- Upload a folder (or parent of folders) as one or more sources
- LLM usage cache-accounting fields (ADR-007)

### Changed
- HTTP server hardened against slow clients, panics, and large uploads
- Web UI upgraded to Next.js 16, ESLint 9.39, and TypeScript 6
- Codecov coverage split into `api` and `web` flags with separate badges

### Fixed
- `/health` now probes all infrastructure dependencies and fails if any is down
- Stale vectors are deleted from Qdrant on source re-ingestion
- Chunk index offset across files to avoid a unique-constraint violation
- Chunk metadata stores the source's relative file path instead of a server temp-dir path
- Concurrent re-ingestion of the same source is guarded; indexing left stuck by a crash is reset on startup
- Workspace membership is enforced on ingest, chunk, and status routes

### Known limitations
This release is for historical/development reference only — **do not deploy publicly or use with untrusted content**. Open issues at time of tagging:
- [#144](https://github.com/jpgomesr/neuralvault/issues/144) — stored XSS via source file content served without `nosniff`
- [#63](https://github.com/jpgomesr/neuralvault/issues/63) — server temp-dir path leaked in source metadata
- [#44](https://github.com/jpgomesr/neuralvault/issues/44) — indexing goroutines are unbounded, no concurrency semaphore
- [#49](https://github.com/jpgomesr/neuralvault/issues/49) — ingestion pipeline is not yet parallelized/batched for large sources
- [#67](https://github.com/jpgomesr/neuralvault/issues/67) — internal error details (DB/storage) can leak to API clients

---