---
name: auth
description: This skill should be used when modifying api/internal/auth/ (login, session, RequireUser middleware), or when asked "how does login work here", "how are sessions handled", "add a protected route". Documents the three login surfaces and the custom session scheme, since neither is a standard JWT/OIDC-library setup.
---

# Auth: login surfaces and session scheme

Three distinct login surfaces converge on one session-issuing path (`issueSession`):

- `GET /auth/login` + `GET /auth/callback` — the standard OIDC authorization-code redirect flow, protected by a CSRF `nv_oauth_state` cookie set before the redirect and checked on callback
- `POST /auth/token` — Resource Owner Password Credentials grant, used by the frontend's native `dev`/`dev` sign-in form (not a redirect flow)
- Both paths call the same `issueSession` once the identity is established — don't add a fourth login surface without routing it through `issueSession` too, or session issuance logic will fork

`GET /auth/logout` and `GET /me` (behind `RequireUser`) round out the route set.

## Session cookie is not a JWT

The `nv_session` cookie is a custom scheme in `session.go`: `base64url(payload).base64url(hmac)`, signed with an HMAC key from `AUTH_` config — not a JWT, no external claims library. When touching session logic, work with `sessionSigner` in `session.go`, not `handler.go` — the signing/verification logic is deliberately kept out of the HTTP layer.

`RequireUser` (in `middleware.go`) is the gate for protected routes — apply it the same way `/me` does for any new route that needs an authenticated user.
