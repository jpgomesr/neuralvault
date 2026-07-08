import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { dispatchSSE, streamQuery } from "./query";
import { streamResponse } from "./test-helpers";

const fetchMock = vi.fn();

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  fetchMock.mockReset();
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
