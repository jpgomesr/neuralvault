#### ADR-005: Keycloak as the dev/local authentication provider, via standard OIDC

##### Status
Accepted

##### Context
[SPEC-011](../specs/SPEC-011-auth-workspaces-tenant-isolation.md) designs authentication, workspace management, and tenant isolation, but deliberately left "which identity provider launches first" as an open question. The data model authenticates callers through external providers only — `user_identity` links a `users` row to a `(provider, external_id)` pair, and there is no first-party password storage by design.

A concrete provider is needed to build and exercise the login flow locally, but the choice must not leak into the codebase: the maintainer wants to swap the provider later (e.g. Google, GitHub, Auth0, or a managed IdP in production) without rewriting the `auth` domain. Hardcoding any single vendor's SDK or non-standard endpoints would defeat that.

##### Decision
Use **Keycloak as the development/local identity provider**, integrated strictly through the standard **OpenID Connect (OIDC)** authorization-code flow with provider discovery — never through Keycloak-specific admin APIs or vendor extensions.

Because the `api/internal/auth/` domain targets the OIDC spec (discovery document, authorization + token endpoints, ID token claims) rather than Keycloak itself, the provider is swappable by configuration alone: the `AUTH_`-prefixed config (issuer/discovery URL, client ID, client secret, redirect URL) points at a locally-run Keycloak in dev and at any OIDC-compliant provider in other environments. Keycloak runs as a service in `docker-compose.yml` alongside the existing dev dependencies (`qdrant`, `postgres`, `ollama`, `minio`).

This decision is recorded in SPEC-011 (Goals, Proposed design, Affected components) and resolves that spec's "which provider launches first" open question. It does not settle the session mechanism (server-side sessions vs. stateless JWT), which remains an open question there.

##### Consequences

###### Positive
- Unblocks building and testing the full login/JIT-provisioning flow locally without registering an OAuth app with an external vendor
- Keeps the `auth` domain vendor-neutral — swapping the provider is a config change, not a code change, honouring SPEC-011's provider-agnostic intent
- Keycloak can model multiple upstream identity sources (social logins, LDAP) behind one OIDC endpoint, so dev parity with future production setups is high
- Adds only a dev-time `docker compose` service; no new application dependency beyond a standard OIDC client

###### Negative
- Adds a service to the local stack, increasing dev resource footprint and first-run setup (a realm/client must be provisioned — ideally via a checked-in realm import)
- OIDC-only means Keycloak-specific features (fine-grained authorization services, admin APIs) are intentionally off-limits, even where they might be convenient
- A preconfigured Keycloak realm is now the friction point that a dev-mode auth bypass was meant to solve; whether to still offer a bypass for single-user local setups is left open in SPEC-011

##### Related decision (Optional)
- [SPEC-011](../specs/SPEC-011-auth-workspaces-tenant-isolation.md) — the component design this decision configures; session mechanism and dev-mode bypass remain open questions there
