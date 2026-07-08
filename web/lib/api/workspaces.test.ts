import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createWorkspace, listWorkspaces } from "./workspaces";
import { jsonResponse } from "./test-helpers";

const fetchMock = vi.fn();

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  fetchMock.mockReset();
});

describe("listWorkspaces", () => {
  it("returns the workspace list on success", async () => {
    const ws = [{ ID: "w1", Name: "Work", CreatedAt: "", UpdatedAt: "" }];
    fetchMock.mockResolvedValueOnce(jsonResponse(ws));
    await expect(listWorkspaces()).resolves.toEqual(ws);
    expect(fetchMock).toHaveBeenCalledWith("/api/workspaces");
  });

  it("throws on an error status", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 503 }));
    await expect(listWorkspaces()).rejects.toThrow("list workspaces failed: 503");
  });
});

describe("createWorkspace", () => {
  it("POSTs the name as JSON and returns the workspace", async () => {
    const ws = { ID: "w1", Name: "Docs", CreatedAt: "", UpdatedAt: "" };
    fetchMock.mockResolvedValueOnce(jsonResponse(ws));
    await expect(createWorkspace("Docs")).resolves.toEqual(ws);
    expect(fetchMock).toHaveBeenCalledWith("/api/workspaces", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: "Docs" }),
    });
  });

  it("throws on an error status", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 400 }));
    await expect(createWorkspace("x")).rejects.toThrow("create workspace failed: 400");
  });
});
