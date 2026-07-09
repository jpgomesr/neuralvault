// Shared fetch/Response mocks used by the lib/api/*.test.ts files.

/** jsonResponse builds a minimal Response-like object for a mocked fetch. */
export function jsonResponse(body: unknown, init?: { ok?: boolean; status?: number }): Response {
  const status = init?.status ?? 200;
  return {
    ok: init?.ok ?? (status >= 200 && status < 300),
    status,
    json: async () => body,
    text: async () =>
      body == null ? "" : typeof body === "string" ? body : JSON.stringify(body),
  } as Response;
}

/** streamResponse wraps SSE text chunks in a Response whose body streams them. */
export function streamResponse(
  chunks: string[],
  init?: { ok?: boolean; status?: number; noBody?: boolean },
): Response {
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
