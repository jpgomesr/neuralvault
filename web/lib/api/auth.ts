// Thin client over the Go API's auth endpoints. All calls go through the
// same-origin /api/* proxy (see next.config.mjs), so the session cookie is
// sent automatically.

import type { Me } from "../types";

/** getMe returns the authenticated user, or null when unauthenticated (401). */
export async function getMe(): Promise<Me | null> {
  const res = await fetch("/api/auth/me");
  if (res.status === 401) return null;
  if (!res.ok) throw new Error(`auth/me failed: ${res.status}`);
  return res.json();
}

export async function logout(): Promise<void> {
  await fetch("/api/auth/logout", { method: "POST" });
}

/**
 * login authenticates via the native email/password form, which proxies to
 * the OIDC provider's token endpoint server-side. Throws on invalid
 * credentials or provider failure; on success the nv_session cookie is set.
 */
export async function login(email: string, password: string): Promise<void> {
  const res = await fetch("/api/auth/token", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
  if (res.status === 401) throw new Error("invalid email or password");
  if (!res.ok) throw new Error(`login failed: ${res.status}`);
}
