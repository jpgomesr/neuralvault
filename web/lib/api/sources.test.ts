import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { listSources, uploadSource, watchSourceStatus } from "./sources";
import { jsonResponse } from "./test-helpers";
import type { SourceProgress } from "../types";

const fetchMock = vi.fn();

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  fetchMock.mockReset();
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
