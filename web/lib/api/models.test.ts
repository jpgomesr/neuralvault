import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  deleteCredential,
  getModelSettings,
  listModels,
  listProviders,
  reindexWorkspace,
  saveCredential,
  setEmbeddingSettings,
  setLLMSettings,
} from "./models";
import { jsonResponse } from "./test-helpers";

const fetchMock = vi.fn();

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  fetchMock.mockReset();
});

describe("listProviders", () => {
  it("returns the provider catalog annotated with credential state", async () => {
    const providers = [
      { provider: "anthropic", name: "Anthropic", configured: true, api_key_hint: "abcd" },
    ];
    fetchMock.mockResolvedValueOnce(jsonResponse(providers));
    await expect(listProviders("w1")).resolves.toEqual(providers);
    expect(fetchMock).toHaveBeenCalledWith("/api/workspaces/w1/providers");
  });

  it("surfaces the server error body", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse("forbidden: not a member of this workspace", { ok: false, status: 403 }));
    await expect(listProviders("w1")).rejects.toThrow("forbidden: not a member of this workspace");
  });

  it("falls back to the status code when there is no error body", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 500 }));
    await expect(listProviders("w1")).rejects.toThrow("list providers failed: 500");
  });
});

describe("listModels", () => {
  it("returns the live model list for a provider, filtered by purpose", async () => {
    const models = [{ id: "claude-sonnet-5", name: "Claude Sonnet 5" }];
    fetchMock.mockResolvedValueOnce(jsonResponse(models));
    await expect(listModels("w1", "anthropic", "completion")).resolves.toEqual(models);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/workspaces/w1/providers/anthropic/models?purpose=completion",
    );
  });

  it("surfaces a missing-credential error so the picker can explain why", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse('provider credential not found: no api key saved for provider "anthropic"', { ok: false, status: 400 }));
    await expect(listModels("w1", "anthropic", "embedding")).rejects.toThrow("no api key saved for provider");
  });
});

describe("saveCredential", () => {
  it("PUTs the api key and base url as JSON", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { status: 204 }));
    await expect(saveCredential("w1", "anthropic", "sk-test", "https://example.com")).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith("/api/workspaces/w1/providers/anthropic/credential", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ api_key: "sk-test", base_url: "https://example.com" }),
    });
  });

  it("surfaces the provider's own rejection message on an invalid key", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse("provider unavailable: anthropic request failed (status 401): invalid x-api-key", { ok: false, status: 502 }));
    await expect(saveCredential("w1", "anthropic", "sk-bad")).rejects.toThrow("invalid x-api-key");
  });
});

describe("deleteCredential", () => {
  it("DELETEs the stored credential", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { status: 204 }));
    await expect(deleteCredential("w1", "anthropic")).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith("/api/workspaces/w1/providers/anthropic/credential", {
      method: "DELETE",
    });
  });

  it("falls back to the status code when there is no error body", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 500 }));
    await expect(deleteCredential("w1", "anthropic")).rejects.toThrow("delete credential failed: 500");
  });
});

describe("getModelSettings", () => {
  it("returns the workspace's chosen models", async () => {
    const settings = { llm_provider: "anthropic", llm_model: "claude-sonnet-5" };
    fetchMock.mockResolvedValueOnce(jsonResponse(settings));
    await expect(getModelSettings("w1")).resolves.toEqual(settings);
    expect(fetchMock).toHaveBeenCalledWith("/api/workspaces/w1/model-settings");
  });

  it("falls back to the status code when there is no error body", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 500 }));
    await expect(getModelSettings("w1")).rejects.toThrow("get model settings failed: 500");
  });
});

describe("setLLMSettings", () => {
  it("PUTs the provider and model as JSON", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { status: 204 }));
    await expect(setLLMSettings("w1", "anthropic", "claude-sonnet-5")).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith("/api/workspaces/w1/model-settings/llm", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ provider: "anthropic", model: "claude-sonnet-5" }),
    });
  });

  it("surfaces the server error body", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse("invalid provider: unknown provider \"bogus\"", { ok: false, status: 400 }));
    await expect(setLLMSettings("w1", "anthropic", "x")).rejects.toThrow("unknown provider");
  });
});

describe("setEmbeddingSettings", () => {
  it("PUTs the provider and model, returning the resulting embedding change", async () => {
    const change = { collection: "nv_openai_text_embedding_3_small_1536", dimensions: 1536, reindex_required: true, stale_sources: 2 };
    fetchMock.mockResolvedValueOnce(jsonResponse(change));
    await expect(setEmbeddingSettings("w1", "openai", "text-embedding-3-small")).resolves.toEqual(change);
    expect(fetchMock).toHaveBeenCalledWith("/api/workspaces/w1/model-settings/embedding", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ provider: "openai", model: "text-embedding-3-small" }),
    });
  });

  it("falls back to the status code when there is no error body", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 500 }));
    await expect(setEmbeddingSettings("w1", "openai", "x")).rejects.toThrow("set embedding model failed: 500");
  });
});

describe("reindexWorkspace", () => {
  it("POSTs and returns how many sources were queued", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ queued: 3 }));
    await expect(reindexWorkspace("w1")).resolves.toEqual({ queued: 3 });
    expect(fetchMock).toHaveBeenCalledWith("/api/workspaces/w1/reindex", { method: "POST" });
  });

  it("surfaces the server error body", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse("forbidden: not a member of this workspace", { ok: false, status: 403 }));
    await expect(reindexWorkspace("w1")).rejects.toThrow("forbidden: not a member of this workspace");
  });
});
