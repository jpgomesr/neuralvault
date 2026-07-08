// Thin client over the Go API's workspace endpoints.

import type { Workspace } from "../types";

export async function listWorkspaces(): Promise<Workspace[]> {
  const res = await fetch("/api/workspaces");
  if (!res.ok) throw new Error(`list workspaces failed: ${res.status}`);
  return res.json();
}

export async function createWorkspace(name: string): Promise<Workspace> {
  const res = await fetch("/api/workspaces", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name }),
  });
  if (!res.ok) throw new Error(`create workspace failed: ${res.status}`);
  return res.json();
}
