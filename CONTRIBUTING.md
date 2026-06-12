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

This starts Qdrant, PostgreSQL, and Ollama via Docker Compose, without the application itself:

```bash
docker compose up -d qdrant postgres ollama
```

### 4. Set up environment variables

```bash
cp .env.example .env
```

Edit `.env` with your local values. At minimum you need:

```env
DATABASE_URL=postgres://postgres:postgres@localhost:5432/neuralvault
QDRANT_URL=http://localhost:6333
OLLAMA_URL=http://localhost:11434
```

### 5. Run database migrations

```bash
make migrate
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

The UI will be available at `http://localhost:3000`.

---

## Project structure

```
NeuralVault/
├── api/                        # Go backend
│   ├── cmd/server/             # Entry point (main.go)
│   ├── internal/
│   │   ├── config/             # Config loading and validation
│   │   ├── embedding/          # Embedder interface and domain types
│   │   │   ├── embedding.go    # Embedder interface, Chunk, Embedding types
│   │   │   ├── ollama/         # Ollama implementation (planned)
│   │   │   └── openai/         # OpenAI implementation (planned)
│   │   ├── llm/                # LLM provider interface and domain types
│   │   │   ├── llm.go          # Provider interface, Message, CompletionRequest types
│   │   │   ├── openai/         # OpenAI implementation (planned)
│   │   │   ├── claude/         # Claude implementation (planned)
│   │   │   ├── gemini/         # Gemini implementation (planned)
│   │   │   └── ollama/         # Ollama implementation (planned)
│   │   ├── health/             # System health status
│   │   │   ├── handler.go      # HTTP handler
│   │   │   ├── service.go      # Business logic interface and implementation
│   │   │   └── routes.go       # Chi subrouter
│   │   ├── logger/             # Global logger initialisation
│   │   └── router/             # Chi router wiring and top-level route mounting
│   └── migrations/             # PostgreSQL migrations
├── web/                        # Next.js frontend
├── docs/                       # Documentation
│   └── adr/                    # Architecture decision records
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

### Feature domains (`health/`, and future endpoint domains)

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

### Running tests

```bash
# Backend tests
cd api && go test ./...

# Frontend tests
cd web && npm run test
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

## Architecture decisions

If your contribution involves a significant technical choice, consider writing an ADR. The template is at [docs/adr/ADR-XXX-template.md](docs/adr/ADR-XXX-template.md). Open it as part of your PR so the decision is documented alongside the code.