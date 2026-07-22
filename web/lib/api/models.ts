// Thin client over the Go API's model-configuration endpoints (BYOK).

/** Provider is a model backend the API can talk to. Matches internal/catalog. */
export type Provider = "ollama" | "anthropic" | "gemini" | "openrouter" | "groq" | "github" | "openai";

/**
 * ProviderStatus is a catalog entry plus this workspace's credential state.
 * The API key is never returned — only api_key_hint, its last 4 characters.
 */
export interface ProviderStatus {
  provider: Provider;
  name: string;
  base_url: string;
  requires_api_key: boolean;
  supports_completions: boolean;
  supports_embeddings: boolean;
  free_tier: boolean;
  configured: boolean;
  api_key_hint?: string;
}

/** ModelInfo is one model a provider can serve, listed live from the provider. */
export interface ModelInfo {
  id: string;
  name: string;
}

/**
 * ModelPurpose filters a model list to what the caller intends to use it for.
 * Most providers cannot self-report this per model and ignore it; Gemini is
 * the current exception (see internal/llm/openaicompat on the API side).
 */
export type ModelPurpose = "completion" | "embedding";

/** ModelSettings is a workspace's chosen models. Empty fields mean the server default. */
export interface ModelSettings {
  llm_provider?: Provider;
  llm_model?: string;
  embedding_provider?: Provider;
  embedding_model?: string;
  embedding_dimensions?: number;
}

/** EmbeddingChange reports the consequences of switching embedding models. */
export interface EmbeddingChange {
  collection: string;
  dimensions: number;
  reindex_required: boolean;
  stale_sources: number;
}

/**
 * apiError surfaces the API's own message rather than a bare status code.
 * Provider failures (a rejected API key, an unknown model) come back as plain
 * text and are exactly what the user needs to fix their configuration.
 */
async function apiError(res: Response, action: string): Promise<Error> {
  const detail = (await res.text()).trim();
  return new Error(detail || `${action} failed: ${res.status}`);
}

export async function listProviders(workspaceId: string): Promise<ProviderStatus[]> {
  const res = await fetch(`/api/workspaces/${workspaceId}/providers`);
  if (!res.ok) throw await apiError(res, "list providers");
  return res.json();
}

export async function listModels(
  workspaceId: string,
  provider: Provider,
  purpose: ModelPurpose,
): Promise<ModelInfo[]> {
  const res = await fetch(
    `/api/workspaces/${workspaceId}/providers/${provider}/models?purpose=${purpose}`,
  );
  if (!res.ok) throw await apiError(res, "list models");
  return res.json();
}

export async function saveCredential(
  workspaceId: string,
  provider: Provider,
  apiKey: string,
  baseUrl?: string,
): Promise<void> {
  const res = await fetch(`/api/workspaces/${workspaceId}/providers/${provider}/credential`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ api_key: apiKey, base_url: baseUrl }),
  });
  if (!res.ok) throw await apiError(res, "save credential");
}

export async function deleteCredential(workspaceId: string, provider: Provider): Promise<void> {
  const res = await fetch(`/api/workspaces/${workspaceId}/providers/${provider}/credential`, {
    method: "DELETE",
  });
  if (!res.ok) throw await apiError(res, "delete credential");
}

export async function getModelSettings(workspaceId: string): Promise<ModelSettings> {
  const res = await fetch(`/api/workspaces/${workspaceId}/model-settings`);
  if (!res.ok) throw await apiError(res, "get model settings");
  return res.json();
}

export async function setLLMSettings(
  workspaceId: string,
  provider: Provider,
  model: string,
): Promise<void> {
  const res = await fetch(`/api/workspaces/${workspaceId}/model-settings/llm`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ provider, model }),
  });
  if (!res.ok) throw await apiError(res, "set completion model");
}

/**
 * setEmbeddingSettings changes the embedding model. The response reports how
 * many sources still hold vectors from the previous model: until they are
 * re-indexed, retrieval finds nothing.
 */
export async function setEmbeddingSettings(
  workspaceId: string,
  provider: Provider,
  model: string,
): Promise<EmbeddingChange> {
  const res = await fetch(`/api/workspaces/${workspaceId}/model-settings/embedding`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ provider, model }),
  });
  if (!res.ok) throw await apiError(res, "set embedding model");
  return res.json();
}

export async function reindexWorkspace(workspaceId: string): Promise<{ queued: number }> {
  const res = await fetch(`/api/workspaces/${workspaceId}/reindex`, { method: "POST" });
  if (!res.ok) throw await apiError(res, "reindex workspace");
  return res.json();
}
