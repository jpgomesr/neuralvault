import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createConversation, listConversationMessages, listConversations } from "./conversations";
import { jsonResponse } from "./test-helpers";

const fetchMock = vi.fn();

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  fetchMock.mockReset();
});

describe("listConversations", () => {
  it("returns the conversation list on success", async () => {
    const list = [{ ID: "c1", WorkspaceID: "w1", Title: "Hi", CreatedAt: "", UpdatedAt: "" }];
    fetchMock.mockResolvedValueOnce(jsonResponse(list));
    await expect(listConversations("w1")).resolves.toEqual(list);
    expect(fetchMock).toHaveBeenCalledWith("/api/conversations?workspace_id=w1");
  });

  it("defaults to an empty array when the body is null", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null));
    await expect(listConversations("w1")).resolves.toEqual([]);
  });

  it("throws on an error status", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 500 }));
    await expect(listConversations("w1")).rejects.toThrow("list conversations failed: 500");
  });
});

describe("createConversation", () => {
  it("POSTs the workspace_id as JSON and returns the conversation", async () => {
    const conv = { ID: "c1", WorkspaceID: "w1", Title: "", CreatedAt: "", UpdatedAt: "" };
    fetchMock.mockResolvedValueOnce(jsonResponse(conv));
    await expect(createConversation("w1")).resolves.toEqual(conv);
    expect(fetchMock).toHaveBeenCalledWith("/api/conversations", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ workspace_id: "w1" }),
    });
  });

  it("throws on an error status", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 403 }));
    await expect(createConversation("w1")).rejects.toThrow("create conversation failed: 403");
  });
});

describe("listConversationMessages", () => {
  it("returns the message list on success", async () => {
    const msgs = [{ ID: "m1", ConversationID: "c1", Role: "user", Content: "hi", Sources: null, CreatedAt: "" }];
    fetchMock.mockResolvedValueOnce(jsonResponse(msgs));
    await expect(listConversationMessages("c1")).resolves.toEqual(msgs);
    expect(fetchMock).toHaveBeenCalledWith("/api/conversations/c1/messages");
  });

  it("defaults to an empty array when the body is null", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null));
    await expect(listConversationMessages("c1")).resolves.toEqual([]);
  });

  it("throws on an error status", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 404 }));
    await expect(listConversationMessages("c1")).rejects.toThrow("list messages failed: 404");
  });
});
