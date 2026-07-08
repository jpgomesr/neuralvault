#### ADR-006: Adopt shadcn/ui, and add a native login form via a server-side password grant

##### Status
Accepted

##### Context
The frontend (`web/`) had no component library or CSS framework — a single hand-rolled dark-theme stylesheet (`web/app/globals.css`) and three flat, custom-styled components. This was getting expensive to keep looking polished and consistent as the UI grows, without hand-writing and maintaining more custom CSS.

Separately, the only way to sign in was a full-page redirect to Keycloak's own hosted login screen (`GET /auth/login` → OIDC authorization-code flow → Keycloak's `/auth` UI → `GET /auth/callback`, see [ADR-005](./ADR-005-dev-auth-provider.md)). That hosted screen looks and feels disconnected from the rest of the app.

Both needs are recorded together here because the second depends on the first: the native login form is built from the same shadcn/ui primitives introduced for the rest of the app.

##### Decision
**Adopt shadcn/ui** (Tailwind CSS v4 + Radix UI primitives, installed via the `shadcn` CLI) as the frontend's base component library, as an incremental swap-in: `web/app/globals.css`'s existing CSS custom properties (`--bg`, `--panel`, `--accent`, etc., renamed `--brand`/`--muted-text` where their names collided with shadcn's own semantic tokens) remain the single source of truth for the dark palette, mapped onto shadcn's `--background`/`--primary`/`--muted`/etc. tokens via a Tailwind v4 `@theme inline` block. Only the primitive elements inside existing components were swapped (`Button`, `Input`, `Select`, `Card`, `Label`); structural layout CSS (`.layout`, `.sidebar`, `.chat`, `.messages`, ...) is untouched.

**Add a native email/password login screen**, replacing `SignIn.tsx`'s redirect link with a form built from the new primitives. It authenticates via a **new backend endpoint, `POST /auth/token`**, which performs the OAuth2 **Resource Owner Password Credentials (ROPC) grant** (RFC 6749 §4.3) server-side against the OIDC provider's token endpoint (`golang.org/x/oauth2`'s standard `PasswordCredentialsToken`), reusing the existing ID-token verification, JIT-provisioning, and `nv_session` cookie-issuance logic from `Exchange`. The browser never talks to the identity provider directly and never sees the client secret — this keeps the `neuralvault` Keycloak client confidential, and keeps `api/internal/auth/` targeting the standard OIDC/OAuth2 spec rather than a Keycloak-specific API, so **ADR-005's "provider swappable by config alone, no vendor SDK" decision still holds**. The existing `GET /auth/login` / `GET /auth/callback` authorization-code flow is left in place, unchanged, alongside the new endpoint.

##### Consequences

###### Positive
- The app gets a consistent, maintainable set of UI primitives without a full redesign or new custom CSS
- The login experience matches the rest of the app instead of bouncing through Keycloak's own UI
- The password grant is standard OAuth2, not a Keycloak SDK call, so ADR-005's provider-swappability is preserved; swapping providers later still only requires config changes, as long as the new provider also supports a token-endpoint password grant
- The client secret and the identity provider's token endpoint stay server-side, never exposed to the browser

###### Negative
- The `neuralvault` Keycloak client must keep Direct Access Grants (ROPC) enabled; some providers restrict or deprecate ROPC, so it may not be available if the dev-only Keycloak provider is swapped for another
- Two login code paths (`/login`+`/callback` and `/token`) now exist side by side, mildly increasing the auth domain's surface area
- Tailwind + shadcn's Radix-based primitives are now new frontend dependencies to keep updated

##### Related decision (Optional)
- [ADR-005](./ADR-005-dev-auth-provider.md) — the OIDC/provider-agnostic decision this one extends with a second, still-standard grant type
- [SPEC-011](../specs/SPEC-011-auth-workspaces-tenant-isolation.md) — updated to mention the `/auth/token` endpoint alongside the authorization-code flow
