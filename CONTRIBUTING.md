# Contributing to NeuralVault

Thank you for your interest in contributing. This document covers how to set up your environment, the conventions we follow, and how to open a pull request.

---

## Table of contents

- [Code of conduct](#code-of-conduct)
- [Ways to contribute](#ways-to-contribute)
- [Prerequisites](#prerequisites)
- [Local development setup](#local-development-setup)
- [Project structure](#project-structure)
- [Development workflow](#development-workflow)
- [Commit conventions](#commit-conventions)
- [Pull request process](#pull-request-process)
- [Reporting bugs](#reporting-bugs)
- [Requesting features](#requesting-features)
- [Reporting chores and tooling work](#reporting-chores-and-tooling-work)

---

## Code of conduct

Be respectful and constructive. We are here to build something useful together. Harassment, discrimination, or hostile behaviour of any kind will not be tolerated.

---

## Ways to contribute

- Fix a bug — check the [open issues](https://github.com/jpgomesr/neuralvault/issues) labelled `bug`
- Implement a roadmap item — check issues labelled `roadmap`
- Improve documentation — typos, clarity, missing steps
- Write tests — coverage is always welcome
- Open an issue — bug reports and well-described feature requests are contributions too

If you plan to work on something significant, open an issue first so we can discuss the approach before you invest time writing code.

---

## Prerequisites

Make sure you have the following installed before starting:

| Tool | Version | Purpose |
| --- | --- | --- |
| Go | 1.26+ | Backend development |
| Node.js | 20+ | Frontend development |
| Docker + Docker Compose | latest | Running services locally |
| Ollama | latest | Local embeddings and models |
| Make | any | Running project scripts |

---

## Local development setup

### 1. Fork and clone

```bash
git clone https://github.com/jpgomesr/neuralvault.git
cd neuralvault
```

### 2. Pull the embedding model

```bash
ollama pull nomic-embed-text
```

### 3. Start infrastructure services

This starts the backing services via Docker Compose, without the application itself — Postgres, Qdrant, Ollama, MinIO (object storage), and Keycloak (OIDC identity provider):

```bash
docker compose up -d          # or: make up
```

### 4. Set up environment variables

There are three env templates — repo root (Docker Compose), `api/` (Go API), and `web/` (frontend, optional):

```bash
cp .env.example .env                # Docker Compose (ports, service credentials)
cp api/.env.example api/.env        # Go API config
cp web/.env.example web/.env.local  # Frontend (optional — defaults work as-is)
```

The defaults line up with what Docker Compose starts, so for a standard setup **no changes are required**. Config uses `<PREFIX>_<FIELD>` variables (`SERVER_`, `POSTGRES_`, `QDRANT_`, `OLLAMA_`, `MINIO_`, `AUTH_`) — the `AUTH_` group defaults to the bundled Keycloak dev realm. See `api/.env.example` for the full list, including cloud LLM provider keys.

### 5. Run database migrations

```bash
make migrate-up
```

### 6. Start the backend

```bash
cd api
go run ./cmd/server
```

The API will be available at `http://localhost:8080`.

### 7. Start the frontend

```bash
cd web
npm install
npm run dev
```

The UI will be available at `http://localhost:3000`. On first load you're redirected to Keycloak to sign in via OIDC — the dev realm ships a seeded `dev` / `dev` user (see [getting-started](getting-started.md) for the full walkthrough).

---

## Project structure

```
NeuralVault/
├── api/                        # Go backend
│   ├── cmd/
│   │   ├── server/             # API entry point (main.go)
│   │   ├── migrate/            # Migration runner (goose)
│   │   └── cli/                # nv CLI — talks to the API over HTTP
│   ├── internal/
│   │   ├── config/             # Config loading and validation
│   │   ├── auth/               # OIDC login, JIT provisioning, session + RequireUser middleware
│   │   ├── workspaces/         # Workspace management + tenant-isolation guard
│   │   ├── sources/            # Source upload/ingest endpoints + ingestion pipeline
│   │   ├── sourcereader/       # Reads files into chunk requests
│   │   ├── retrieval/          # Query + streaming grounded answers
│   │   ├── embedding/          # Embedder interface and domain types
│   │   │   ├── embedding.go    # Embedder interface
│   │   │   ├── types/          # Shared value types (breaks import cycle)
│   │   │   └── ollama/         # Ollama implementation (nomic-embed-text)
│   │   ├── llm/                # LLM provider interface and domain types
│   │   │   ├── llm.go          # Provider interface
│   │   │   ├── types/          # Shared value types (breaks import cycle)
│   │   │   └── ollama/         # Ollama implementation (OpenAI/Claude/Gemini planned)
│   │   ├── chunking/           # Text splitting
│   │   │   ├── chunking.go     # Splitter interface and Span type
│   │   │   ├── service.go      # ChunkService — ChunkSource, ListChunks, DeleteChunks
│   │   │   ├── markdown/       # Markdown section splitter
│   │   │   └── text/           # Plain-text splitter
│   │   ├── vectorstorage/      # Qdrant client
│   │   ├── objectstorage/      # MinIO (S3-compatible) client
│   │   ├── storage/            # Postgres pool
│   │   │   └── postgres/migrations/  # SQL migrations
│   │   ├── health/             # System health status (handler/service/routes)
│   │   ├── model/              # Shared domain models
│   │   ├── logger/             # Global logger initialisation
│   │   └── router/             # Chi router wiring and top-level route mounting
├── web/                        # Next.js frontend (App Router, TypeScript)
├── docker/keycloak/import/     # Keycloak dev realm (auto-imported)
├── docs/                       # Documentation
│   ├── adr/                    # Architecture decision records
│   └── specs/                  # Technical specs (SPEC-NNN)
└── docker-compose.yml
```

---

## Package conventions

### Interface packages (`embedding/`, `llm/`, and future pluggable domains)

Packages with swappable backends follow a two-level layout:

```
internal/<domain>/
    <domain>.go          # interface + domain types only — no business logic, no imports of concrete packages
    <provider>/          # one sub-package per concrete implementation
        <provider>.go    # implements the interface defined in the parent package
```

The root file (`embedding.go`, `llm.go`) defines only the Go interface and the value types that cross package boundaries. Nothing in the rest of the codebase imports a concrete provider — callers depend solely on the interface. This keeps providers interchangeable and independently testable.

When adding a new provider:

1. Create `internal/<domain>/<provider>/<provider>.go`.
2. Implement every method of the interface defined in the parent package.
3. Wire the concrete type in `cmd/server/main.go` — nowhere else.

### Feature domains (`health/`, `auth/`, `workspaces/`, `sources/`, `retrieval/`, …)

Each feature domain exposes exactly three files:

| File | Responsibility |
| --- | --- |
| `handler.go` | HTTP layer — decode request, call service, encode response |
| `service.go` | Business logic — interface definition and its implementation |
| `routes.go` | Chi subrouter — maps HTTP verbs and paths to handler methods |

Mount the subrouter in `router/router.go`:

```go
r.Mount("/health", health.Routes(handler))
```

---

## Development workflow

### Branching

Branch off `main` using this naming convention:

| Type | Pattern | Example |
| --- | --- | --- |
| Feature | `feat/short-description` | `feat/chunking-engine` |
| Bug fix | `fix/short-description` | `fix/qdrant-timeout` |
| Documentation | `docs/short-description` | `docs/getting-started` |
| Refactor | `refactor/short-description` | `refactor/embedding-interface` |
| Chore / tooling | `chore/short-description` | `chore/swagger-ci-check` |

### Running tests

```bash
# Backend tests
cd api && go test ./...          # or: go test ./... -race (matches CI)

# Frontend type checking and tests
cd web && npm run type-check
cd web && npm run test           # vitest run
cd web && npm run test:coverage  # vitest run --coverage (matches CI's test-coverage-web.yml)
```

### Linting

```bash
# Backend
cd api && golangci-lint run

# Frontend
cd web && npm run lint
```

All lint checks run in CI. Pull requests that fail linting will not be merged.

---

## Commit conventions

We follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <short summary>
```

Common types:

| Type | When to use |
| --- | --- |
| `feat` | A new feature |
| `fix` | A bug fix |
| `docs` | Documentation only |
| `refactor` | Code change with no behaviour change |
| `test` | Adding or updating tests |
| `chore` | Build process, dependencies, tooling |

Examples:

```
feat(chunking): add markdown section splitter
fix(qdrant): handle connection timeout on startup
docs(adr): add ADR-003 vector database decision
```

---

## Pull request process

1. Make sure your branch is up to date with `main` before opening a PR
2. Keep PRs focused — one concern per PR
3. Fill in the PR template completely
4. Link the related issue in the PR description (`Closes #123`)
5. All CI checks must pass before review
6. At least one maintainer approval is required before merging
7. Squash merge is preferred to keep the main branch history clean

---

## Reporting bugs

Open an issue using the **Bug report** template. Include:

- What you expected to happen
- What actually happened
- Steps to reproduce
- Your environment (OS, Go version, Docker version, Ollama version)
- Relevant logs if available

---

## Requesting features

Open an issue using the **Feature request** template. Include:

- The problem you are trying to solve
- Your proposed solution
- Any alternatives you considered

Check the [roadmap](docs/roadmap.md) first — your feature may already be planned.

---

## Reporting chores and tooling work

Open an issue using the **Chore / Tooling** template. Include:

- What is missing, fragile, or manual today
- Your proposed solution

---

## Architecture decisions

If your contribution involves a significant technical choice, consider writing an ADR. The template is at [docs/adr/ADR-XXX-template.md](docs/adr/ADR-XXX-template.md). Open it as part of your PR so the decision is documented alongside the code.