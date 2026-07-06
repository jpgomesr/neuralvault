# Changelog

All notable changes to NeuralVault will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
NeuralVault uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

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

---

<!-- 
When releasing, move items from [Unreleased] into a new version block:

## [0.1.0] - YYYY-MM-DD

### Added
-

### Changed
-

### Fixed
-

### Removed
-
-->