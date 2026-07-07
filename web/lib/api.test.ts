import { describe, expect, it, vi } from "vitest";
import { dispatchSSE } from "./api";

describe("dispatchSSE", () => {
  it("parses a token event and invokes onToken with the content", () => {
    const onToken = vi.fn();
    dispatchSSE('event: token\ndata: {"content":"hello"}', { onToken });
    expect(onToken).toHaveBeenCalledWith("hello");
  });

  it("invokes onDone for a done event", () => {
    const onDone = vi.fn();
    dispatchSSE("event: done\ndata: {}", { onDone });
    expect(onDone).toHaveBeenCalledTimes(1);
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
