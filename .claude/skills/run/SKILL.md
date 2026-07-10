---
name: run
description: This skill should be used when the user asks to "run NeuralVault", "start the app", "start the API", "start the frontend", "launch the stack", "sign in locally", or wants to see a change working end-to-end in the running application. Provides NeuralVault's actual local launch sequence (Docker infra + Go API + Next.js frontend), overriding generic app-launch guidance.
---

# Running NeuralVault locally

NeuralVault runs as Docker Compose infrastructure plus two host processes (the Go API and the Next.js frontend). Start them in this order.

## 1. Infrastructure

Check whether Postgres, Qdrant, Ollama, MinIO, and Keycloak are already up:

```bash
docker compose ps
```

If not running:

```bash
docker compose up -d          # or: make up
```

Keycloak takes a few seconds to import the `neuralvault` dev realm — wait for it to report healthy before signing in later.

## 2. Embedding model

Confirm the embedding model is pulled (one-time, ~270MB):

```bash
ollama pull nomic-embed-text
```

## 3. Database migrations

Migrations are never run automatically by the server — run them explicitly:

```bash
make migrate-up
```

## 4. API

```bash
cd api && go run ./cmd/server        # or: make run
```

Listens on `http://localhost:8080`. Confirm it's healthy:

```bash
curl http://localhost:8080/health
```

## 5. Frontend

```bash
cd web && npm install && npm run dev   # npm install only needed on first run
```

Serves `http://localhost:3000`.

## 6. Sign in

Navigate to `http://localhost:3000` — this redirects to Keycloak OIDC. Use the seeded dev user:

| Username | Password |
| --- | --- |
| `dev` | `dev` |

## Known limitation

The `nv` CLI (`make run-cli`) does not work against a default local setup — it has no OIDC login flow yet ([SPEC-011](../../../docs/specs/SPEC-011-auth-workspaces-tenant-isolation.md)). Drive the app through the API directly (`curl`, an HTTP client) or the frontend instead of the CLI until that's implemented.

## Troubleshooting

- **Ollama unreachable**: `ollama serve`, then retry
- **Login fails**: Keycloak realm import may still be in progress — `docker compose logs keycloak`
- **Migrations fail**: Postgres may not be ready yet — `docker compose up -d postgres`, wait, retry `make migrate-up`
- **Port conflict**: ports in use are `3000` (web), `8080` (api), `5432` (postgres), `6333` (qdrant), `9000`/`9001` (minio), `8081` (keycloak), `11434` (ollama)

Full walkthrough with explanations: [`getting-started.md`](../../../getting-started.md).
