import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createWorkspace,
  dispatchSSE,
  getMe,
  listSources,
  listWorkspaces,
  logout,
  streamQuery,
  uploadSource,
  watchSourceStatus,
} from "./api";
import type { SourceProgress } from "./types";

/** jsonResponse builds a minimal Response-like object for a mocked fetch. */
function jsonResponse(body: unknown, init?: { ok?: boolean; status?: number }): Response {
  const status = init?.status ?? 200;
  return {
    ok: init?.ok ?? (status >= 200 && status < 300),
    status,
    json: async () => body,
  } as Response;
}

/** streamResponse wraps SSE text chunks in a Response whose body streams them. */
function streamResponse(chunks: string[], init?: { ok?: boolean; status?: number; noBody?: boolean }): Response {
  const encoder = new TextEncoder();
  let i = 0;
  const reader = {
    read: async () => {
      if (i >= chunks.length) return { value: undefined, done: true };
      return { value: encoder.encode(chunks[i++]), done: false };
    },
  };
  return {
    ok: init?.ok ?? true,
    status: init?.status ?? 200,
    body: init?.noBody ? null : { getReader: () => reader },
  } as unknown as Response;
}

const fetchMock = vi.fn();

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  fetchMock.mockReset();
});

describe("getMe", () => {
  it("returns the user on 200", async () => {
    const me = { id: "u1", email: "a@b.com" };
    fetchMock.mockResolvedValueOnce(jsonResponse(me));
    await expect(getMe()).resolves.toEqual(me);
    expect(fetchMock).toHaveBeenCalledWith("/api/auth/me");
  });

  it("returns null when unauthenticated (401)", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 401 }));
    await expect(getMe()).resolves.toBeNull();
  });

  it("throws on a non-401 error status", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 500 }));
    await expect(getMe()).rejects.toThrow("auth/me failed: 500");
  });
});

describe("logout", () => {
  it("POSTs to the logout endpoint", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null));
    await logout();
    expect(fetchMock).toHaveBeenCalledWith("/api/auth/logout", { method: "POST" });
  });
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

describe("listSources", () => {
  it("returns the sources and URL-encodes the workspace id", async () => {
    const sources = [{ ID: "s1", Name: "doc.pdf", Status: "ready" }];
    fetchMock.mockResolvedValueOnce(jsonResponse(sources));
    await expect(listSources("a b/c")).resolves.toEqual(sources);
    expect(fetchMock).toHaveBeenCalledWith("/api/sources?workspace_id=a%20b%2Fc");
  });

  it("coerces a null body to an empty array", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null));
    await expect(listSources("w1")).resolves.toEqual([]);
  });

  it("throws on an error status", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 404 }));
    await expect(listSources("w1")).rejects.toThrow("list sources failed: 404");
  });
});

describe("uploadSource", () => {
  const fileList = (...files: File[]) => files as unknown as FileList;

  it("POSTs a FormData body with the workspace, name and files", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ source: { ID: "s1" }, status_url: "/s" }));
    const files = fileList(new File(["hi"], "a.txt"), new File(["yo"], "b.txt"));

    const result = await uploadSource("w1", "My source", files);

    expect(result).toEqual({ source: { ID: "s1" }, status_url: "/s" });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/sources");
    expect(init.method).toBe("POST");
    const form = init.body as FormData;
    expect(form).toBeInstanceOf(FormData);
    expect(form.get("workspace_id")).toBe("w1");
    expect(form.get("name")).toBe("My source");
    expect(form.getAll("files")).toHaveLength(2);
  });

  it("throws on an error status", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 413 }));
    await expect(uploadSource("w1", "n", fileList())).rejects.toThrow("upload failed: 413");
  });
});

describe("watchSourceStatus", () => {
  class FakeEventSource {
    onmessage: ((e: { data: string }) => void) | null = null;
    onerror: (() => void) | null = null;
    close = vi.fn();
    constructor(public url: string) {}
    emit(ev: SourceProgress) {
      this.onmessage?.({ data: JSON.stringify(ev) });
    }
  }

  /** watch subscribes and returns the FakeEventSource the call constructed. */
  function watch(handlers: Parameters<typeof watchSourceStatus>[1]): FakeEventSource {
    return watchSourceStatus("s1", handlers) as unknown as FakeEventSource;
  }

  beforeEach(() => {
    vi.stubGlobal("EventSource", FakeEventSource);
  });

  it("subscribes to the source status endpoint", () => {
    expect(watch({}).url).toBe("/api/sources/s1/status");
  });

  it("forwards progress events without closing", () => {
    const onProgress = vi.fn();
    const es = watch({ onProgress });
    es.emit({ type: "indexing", chunks: 2, total: 10 });
    expect(onProgress).toHaveBeenCalledWith({ type: "indexing", chunks: 2, total: 10 });
    expect(es.close).not.toHaveBeenCalled();
  });

  it("forwards a done event and closes the stream", () => {
    const onDone = vi.fn();
    const es = watch({ onDone });
    es.emit({ type: "done" });
    expect(onDone).toHaveBeenCalledWith({ type: "done" });
    expect(es.close).toHaveBeenCalledTimes(1);
  });

  it("forwards an error event and closes the stream", () => {
    const onError = vi.fn();
    const es = watch({ onError });
    es.emit({ type: "error", error: "boom" });
    expect(onError).toHaveBeenCalledWith({ type: "error", error: "boom" });
    expect(es.close).toHaveBeenCalledTimes(1);
  });

  it("reports a connection loss via onError and closes", () => {
    const onError = vi.fn();
    const es = watch({ onError });
    es.onerror?.();
    expect(onError).toHaveBeenCalledWith({ type: "error", error: "connection lost" });
    expect(es.close).toHaveBeenCalledTimes(1);
  });
});

describe("streamQuery", () => {
  it("calls onError when the response is not ok", async () => {
    fetchMock.mockResolvedValueOnce(streamResponse([], { ok: false, status: 500 }));
    const onError = vi.fn();
    await streamQuery("w1", "q", { onError });
    expect(onError).toHaveBeenCalledWith("request failed: 500");
  });

  it("calls onError when the response has no body", async () => {
    fetchMock.mockResolvedValueOnce(streamResponse([], { ok: true, status: 200, noBody: true }));
    const onError = vi.fn();
    await streamQuery("w1", "q", { onError });
    expect(onError).toHaveBeenCalledWith("request failed: 200");
  });

  it("POSTs the workspace and question as JSON", async () => {
    fetchMock.mockResolvedValueOnce(streamResponse([]));
    await streamQuery("w1", "hello?", {});
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/query/stream");
    expect(init.method).toBe("POST");
    expect(init.body).toBe(JSON.stringify({ workspace_id: "w1", question: "hello?" }));
  });

  it("dispatches SSE events split across chunk boundaries", async () => {
    fetchMock.mockResolvedValueOnce(
      streamResponse([
        'event: sources\ndata: {"results":[{"chunk_id":"c1"}]}\n\nevent: to',
        'ken\ndata: {"content":"hi"}\n\nevent: done\ndata: {}\n\n',
      ]),
    );
    const onSources = vi.fn();
    const onToken = vi.fn();
    const onDone = vi.fn();
    await streamQuery("w1", "q", { onSources, onToken, onDone });
    expect(onSources).toHaveBeenCalledWith([{ chunk_id: "c1" }]);
    expect(onToken).toHaveBeenCalledWith("hi");
    expect(onDone).toHaveBeenCalledTimes(1);
  });

  it("passes an abort signal through to fetch", async () => {
    fetchMock.mockResolvedValueOnce(streamResponse([]));
    const signal = new AbortController().signal;
    await streamQuery("w1", "q", {}, signal);
    expect(fetchMock.mock.calls[0][1].signal).toBe(signal);
  });
});

describe("dispatchSSE", () => {
  it("parses a token event and invokes onToken with the content", () => {
    const onToken = vi.fn();
    dispatchSSE('event: token\ndata: {"content":"hello"}', { onToken });
    expect(onToken).toHaveBeenCalledWith("hello");
  });

  it("parses a sources event and invokes onSources with the results", () => {
    const onSources = vi.fn();
    dispatchSSE('event: sources\ndata: {"results":[{"chunk_id":"c1"}]}', { onSources });
    expect(onSources).toHaveBeenCalledWith([{ chunk_id: "c1" }]);
  });

  it("defaults sources to an empty array when results is missing", () => {
    const onSources = vi.fn();
    dispatchSSE("event: sources\ndata: {}", { onSources });
    expect(onSources).toHaveBeenCalledWith([]);
  });

  it("invokes onDone for a done event", () => {
    const onDone = vi.fn();
    dispatchSSE("event: done\ndata: {}", { onDone });
    expect(onDone).toHaveBeenCalledTimes(1);
  });

  it("invokes onError with the payload error for an error event", () => {
    const onError = vi.fn();
    dispatchSSE('event: error\ndata: {"error":"nope"}', { onError });
    expect(onError).toHaveBeenCalledWith("nope");
  });

  it("falls back to a default message for an error event without an error field", () => {
    const onError = vi.fn();
    dispatchSSE("event: error\ndata: {}", { onError });
    expect(onError).toHaveBeenCalledWith("stream error");
  });

  it("joins multi-line data payloads before parsing", () => {
    const onToken = vi.fn();
    dispatchSSE('event: token\ndata: {"content":\ndata: "multi"}', { onToken });
    expect(onToken).toHaveBeenCalledWith("multi");
  });

  it("invokes onError when the payload is malformed JSON", () => {
    const onToken = vi.fn();
    const onError = vi.fn();
    dispatchSSE("event: token\ndata: not-json", { onToken, onError });
    expect(onError).toHaveBeenCalledWith("malformed stream event");
  });

  it("ignores a block with no event line", () => {
    const onToken = vi.fn();
    dispatchSSE('data: {"content":"hello"}', { onToken });
    expect(onToken).not.toHaveBeenCalled();
  });
});
