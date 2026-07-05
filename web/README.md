# NeuralVault Web

The chat frontend: a thin [Next.js](https://nextjs.org/) (App Router, TypeScript)
client. It handles no retrieval, ingestion, or LLM logic — identity, tenant
isolation, and answer generation all live in the Go API.

## Develop

Requires the API and its infrastructure running (see the root `AGENTS.md`):

```bash
docker compose up -d          # incl. keycloak
cd api && go run ./cmd/server # API on :8080
```

Then, in `web/`:

```bash
npm install
npm run dev                   # http://localhost:3000
```

Sign in at `http://localhost:3000` with the bundled Keycloak dev user
(`dev` / `dev`).

## How it talks to the API

`next.config.mjs` rewrites `/api/*` to the Go API (default
`http://localhost:8080`, override with `API_BASE_URL`). Because the browser and
API share an origin, the `nv_session` cookie is first-party and no CORS is
needed. The chat reads `POST /api/query/stream` with a streaming `fetch`; source
indexing status uses an `EventSource` on `GET /api/sources/{id}/status`.

## Scripts

| Script               | Purpose                          |
| -------------------- | -------------------------------- |
| `npm run dev`        | Dev server on :3000              |
| `npm run build`      | Production build                 |
| `npm run lint`       | ESLint (`next lint`)             |
| `npm run type-check` | `tsc --noEmit`                   |

CI (`.github/workflows/ci-web.yml`) runs `lint`, `type-check`, and `build`.
