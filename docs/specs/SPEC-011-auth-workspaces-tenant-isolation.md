#### SPEC-011: Authentication, workspace management, and tenant isolation

##### Status
Implemented

##### Problem statement
The data model is multi-tenant from day one (`users`, `workspace`, `user_workspace`, `user_identity` — [SPEC-005](SPEC-005-relational-persistence.md)), and every ingestion and retrieval operation is workspace-scoped. But nothing enforces any of it: `workspace_id` is client-supplied on every request (`POST /query` body, `POST /sources` form field), no endpoint creates workspaces or users, and no middleware establishes who the caller is. Any caller can read or write any workspace. [SPEC-009](SPEC-009-platform-cross-cutting.md) records this as an acknowledged gap; roadmap Phase 1 now lists Auth, Workspace management, and Workspace membership middleware. This spec designs how identity is established, how workspaces are created and joined, and where tenant isolation is enforced.

##### Goals
- Establish caller identity on every request via an auth middleware, backed by external OpenID Connect (OIDC) identity providers through `user_identity` (no first-party password storage — the schema has none by design).
- A `workspaces` domain: create a workspace (creator becomes `owner` in `user_workspace`) and list the authenticated user's workspaces — giving the frontend a real way to obtain a `workspace_id`.
- Enforce workspace membership on every `workspace_id`-scoped route (`/sources`, `/query`, and future chat), so a valid identity cannot touch a workspace it doesn't belong to.
- Just-in-time user provisioning: first login through a provider creates the `users` row and its `user_identity` link atomically.

##### Non-goals
- First-party credentials (password hashing, reset flows) — `user_identity` models external providers only.
- Role-based permission granularity beyond membership — `owner`/`admin`/`member` exist in the schema, but per-role authorization rules (who can delete sources, invite members) are deferred until the operations that need them exist.
- API tokens for non-browser clients (CLI, SDKs, MCP — [SPEC-010](SPEC-010-ecosystem-dx.md)) — designed later on top of the same middleware. Consequence: the CLI cannot authenticate until that lands ([#104](https://github.com/jpgomesr/neuralvault/issues/104)).
- Postgres row-level security — isolation is enforced in the application layer for now (see Open questions in [SPEC-005](SPEC-005-relational-persistence.md)).

##### Proposed design
Two new domains following the repo's handler/service/routes layout, plus middleware in `router/router.go`:

- **`api/internal/auth/`** — the login flow, built on the standard OIDC authorization-code flow with provider discovery, so no provider-specific code lives in the domain. **Keycloak is the development identity provider** (run locally via `docker compose`); because the integration targets the OIDC spec rather than Keycloak's own APIs, the provider is swappable (Google, GitHub, Auth0, …) by configuration alone, with no code changes. On callback, the service looks up `user_identity` by `(provider, external_id)`; if absent, it creates `users` + `user_identity` in one transaction (JIT provisioning). It then issues the session credential and exposes middleware `auth.RequireUser` that resolves the credential to a `user_id` and stores it in the request context (pattern mirrors `logger.RequestID`).
- **`api/internal/workspaces/`** — `POST /workspaces` (creates `workspace` + `user_workspace` row with role `owner` transactionally) and `GET /workspaces` (lists workspaces joined through `user_workspace` for the context's `user_id`, using the existing `idx_user_workspace_workspace_id` access paths). Mounted in `router.go` like every other domain.
- **Membership enforcement** — `auth.RequireWorkspace` middleware (or a shared service check) runs after `RequireUser` on workspace-scoped routes: it reads the request's `workspace_id` and verifies a `user_workspace` row exists for `(user_id, workspace_id)`, rejecting with `403` otherwise. Handlers keep receiving `workspace_id` explicitly as today; the middleware only vouches that the pair is legitimate.
- **Route wiring** — `/health` and `/swagger` stay public; `/sources`, `/query`, `/workspaces` are wrapped by `RequireUser`, and the first two additionally by the membership check.

##### Affected components
- `api/internal/auth/` (new), `api/internal/workspaces/` (new)
- `api/internal/router/router.go` — middleware ordering and route wiring
- `api/internal/config/` — OIDC provider config (`AUTH_`-prefixed, following the existing prefix convention): issuer/discovery URL, client ID, client secret, redirect URL — pointing at Keycloak in dev, at any OIDC-compliant provider in other environments
- `docker-compose.yml` — a Keycloak service for local development, alongside the existing `qdrant`/`postgres`/`ollama`/`minio` services
- `api/internal/model/` — `User`, `Workspace`, `UserWorkspace`, `UserIdentity` already exist; no schema change expected
- `docs/roadmap.md` Phase 1 items: Auth, Workspace management, Workspace membership middleware

##### Open questions
All but the last were settled by the implementation (#94–#98):

- **Session mechanism — settled: stateless HMAC-signed token.** The `nv_session` HttpOnly cookie carries `base64url(claimsJSON).base64url(HMAC-SHA256)` (`internal/auth/session.go`), 24h TTL, keyed by `AUTH_SESSION_SECRET`. Self-contained and tamper-evident without a session store or a JWT dependency; revocation is expiry-only for now.
- **Dev-mode auth bypass — settled: not needed.** The Compose Keycloak service auto-imports the `neuralvault` realm with a seeded `dev`/`dev` user (`docker/keycloak/import/`), which removes the local-setup friction that motivated a bypass.
- **Enforcement point — settled: per-handler guard, not middleware.** `workspaces.EnsureMember` (`internal/workspaces/guard.go`) is called by each workspace-scoped handler (`sources`, `retrieval`) after `RequireUser`; `workspace_id` keeps living in body/form/query, so no breaking path reshape was needed.
- **SSE authentication — settled: same cookie.** `GET /sources/{id}/status` sits inside the `RequireUser` group; `EventSource` cannot set headers, but it sends first-party cookies, and the frontend reaches the API through a same-origin `/api/*` proxy (`web/next.config.mjs`), so the `nv_session` cookie flows on SSE requests too.
- **CLI authentication — still open**, now tracked in [#104](https://github.com/jpgomesr/neuralvault/issues/104): `api/cmd/cli` sends no credential, so `nv ingest`/`nv query` receive `401` against the protected routes until an `nv login` flow (device grant, loopback code flow, or API tokens per [SPEC-010](SPEC-010-ecosystem-dx.md)) exists.

##### Acceptance criteria
- An unauthenticated request to `/sources`, `/query`, or `/workspaces` receives `401`; `/health` and `/swagger` remain public.
- First login through a provider creates exactly one `users` row and one `user_identity` row; subsequent logins reuse them.
- `POST /workspaces` returns a workspace whose creator holds an `owner` row in `user_workspace`.
- An authenticated user querying a workspace they don't belong to receives `403`, and no cross-workspace data is returned.

##### Related (Optional)
- [SPEC-005](SPEC-005-relational-persistence.md) — the schema this spec activates
- [SPEC-009](SPEC-009-platform-cross-cutting.md) — where the gap is recorded
- [SPEC-010](SPEC-010-ecosystem-dx.md) — non-browser client auth builds on this
- [SPEC-007](SPEC-007-llm-provider-layer.md) — BYOK key scoping depends on identity
