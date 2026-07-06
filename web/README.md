# NeuralVault Web

Thin Next.js (App Router, TypeScript) client for the NeuralVault API: OIDC sign-in gate, workspace switcher, streaming chat, and file upload with live indexing status.

The frontend contains no retrieval, ingestion, or LLM logic — identity, session handling, and tenant isolation are enforced by the API.

## Commands

```bash
npm install        # first run only
npm run dev        # http://localhost:3000
npm run lint       # next lint
npm run type-check # tsc --noEmit
npm run build      # production build
```

## How it talks to the API

`next.config.mjs` rewrites `/api/*` to the API host (`API_BASE_URL`, default `http://localhost:8080`). Browser and API share an origin, so the `nv_session` cookie is first-party and no CORS setup is needed.

## Environment

Nothing is required for local dev. To override `API_BASE_URL` (e.g. pointing at a remote API), copy the template:

```bash
cp .env.example .env.local
```

Browser-exposed variables must be prefixed `NEXT_PUBLIC_` (none are needed today).

## Structure

```
web/
├── app/              # App Router — layout, page, global styles
├── components/       # Chat, Sidebar, SignIn
├── lib/              # api.ts (API client), types.ts
└── next.config.mjs   # /api/* proxy to the Go API
```

## Prerequisites

The API and its infrastructure must be running (including Keycloak for sign-in) — see the repo-root [getting-started.md](../getting-started.md).
