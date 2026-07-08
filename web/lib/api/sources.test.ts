import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  fetchSourceFileText,
  filesToUploadFiles,
  groupFilesByFolder,
  listSourceFiles,
  listSources,
  sourceFileContentUrl,
  totalBytes,
  uploadSource,
  watchSourceStatus,
} from "./sources";
import { jsonResponse } from "./test-helpers";
import type { SourceProgress } from "../types";

/** fileWithPath builds a File carrying a webkitRelativePath (which the ctor can't set). */
function fileWithPath(relativePath: string): File {
  const name = relativePath.split("/").pop() ?? relativePath;
  const file = new File(["x"], name);
  Object.defineProperty(file, "webkitRelativePath", { value: relativePath });
  return file;
}

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
  it("POSTs a FormData body with the workspace, name and files using relative paths", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ source: { ID: "s1" }, status_url: "/s" }));
    const files = [
      { file: new File(["hi"], "a.txt"), path: "docs/a.txt" },
      { file: new File(["yo"], "b.txt"), path: "b.txt" },
    ];

    const result = await uploadSource("w1", "My source", files);

    expect(result).toEqual({ source: { ID: "s1" }, status_url: "/s" });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/sources");
    expect(init.method).toBe("POST");
    const form = init.body as FormData;
    expect(form).toBeInstanceOf(FormData);
    expect(form.get("workspace_id")).toBe("w1");
    expect(form.get("name")).toBe("My source");
    const parts = form.getAll("files") as File[];
    expect(parts).toHaveLength(2);
    // The relative path is used as each part's filename.
    expect(parts[0].name).toBe("docs/a.txt");
  });

  it("surfaces the server error body on a failed upload", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse("name is required", { ok: false, status: 400 }));
    await expect(uploadSource("w1", "n", [])).rejects.toThrow("name is required");
  });

  it("falls back to the status code when there is no error body", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 413 }));
    await expect(uploadSource("w1", "n", [])).rejects.toThrow("upload failed: 413");
  });
});

describe("filesToUploadFiles", () => {
  const asFileList = (...files: File[]) => files as unknown as FileList;

  it("maps each file to its own name with no directory structure", () => {
    const result = filesToUploadFiles(asFileList(new File(["a"], "a.txt"), new File(["b"], "b.md")));
    expect(result.map((f) => f.path)).toEqual(["a.txt", "b.md"]);
    expect(result[0].file.name).toBe("a.txt");
  });
});

describe("totalBytes", () => {
  it("sums the sizes of the underlying files", () => {
    const files = [
      { file: new File(["abc"], "a"), path: "a" },
      { file: new File(["de"], "b"), path: "b" },
    ];
    expect(totalBytes(files)).toBe(5);
  });
});

describe("groupFilesByFolder", () => {
  const asFileList = (...files: File[]) => files as unknown as FileList;

  it("falls back to the file name when there is no relative path", () => {
    // A loose file (no webkitRelativePath) becomes a single-file group.
    const groups = groupFilesByFolder(asFileList(new File(["x"], "loose.md")));
    expect(groups).toEqual([{ name: "loose.md", files: [expect.objectContaining({ path: "loose.md" })] }]);
  });

  it("makes one source named after the folder for files directly inside it", () => {
    const groups = groupFilesByFolder(
      asFileList(fileWithPath("dataset/a.md"), fileWithPath("dataset/b.md")),
    );
    expect(groups).toHaveLength(1);
    expect(groups[0].name).toBe("dataset");
    expect(groups[0].files.map((f) => f.path)).toEqual(["a.md", "b.md"]);
  });

  it("makes one source per top-level subfolder, keeping nested paths", () => {
    const groups = groupFilesByFolder(
      asFileList(
        fileWithPath("parent/subA/x.md"),
        fileWithPath("parent/subA/deep/y.md"),
        fileWithPath("parent/subB/z.md"),
      ),
    );
    const byName = Object.fromEntries(groups.map((g) => [g.name, g]));
    expect(Object.keys(byName).sort()).toEqual(["subA", "subB"]);
    expect(byName.subA.files.map((f) => f.path).sort()).toEqual(["deep/y.md", "x.md"]);
    expect(byName.subB.files.map((f) => f.path)).toEqual(["z.md"]);
  });
});

describe("listSourceFiles", () => {
  it("returns the files and URL-encodes the source id", async () => {
    const files = [{ id: "f1", source_id: "s1", name: "a.md", size: 3, content_type: "text/markdown", created_at: "" }];
    fetchMock.mockResolvedValueOnce(jsonResponse(files));
    await expect(listSourceFiles("s 1")).resolves.toEqual(files);
    expect(fetchMock).toHaveBeenCalledWith("/api/sources/s%201/files");
  });

  it("coerces a null body to an empty array", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null));
    await expect(listSourceFiles("s1")).resolves.toEqual([]);
  });

  it("throws on an error status", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 403 }));
    await expect(listSourceFiles("s1")).rejects.toThrow("list source files failed: 403");
  });
});

describe("sourceFileContentUrl", () => {
  it("builds a content URL with the id and path encoded", () => {
    expect(sourceFileContentUrl("s 1", "docs/a b.md")).toBe(
      "/api/sources/s%201/files/content?path=docs%2Fa%20b.md",
    );
  });
});

describe("fetchSourceFileText", () => {
  it("returns the file body text", async () => {
    fetchMock.mockResolvedValueOnce({ ok: true, text: async () => "# Hi" } as Response);
    await expect(fetchSourceFileText("s1", "a.md")).resolves.toBe("# Hi");
    expect(fetchMock).toHaveBeenCalledWith("/api/sources/s1/files/content?path=a.md");
  });

  it("throws on an error status", async () => {
    fetchMock.mockResolvedValueOnce({ ok: false, status: 404 } as Response);
    await expect(fetchSourceFileText("s1", "a.md")).rejects.toThrow("fetch file failed: 404");
  });
});

describe("listSourceFiles", () => {
  it("returns the files and URL-encodes the source id", async () => {
    const files = [{ id: "f1", source_id: "s1", name: "a.md", size: 3, content_type: "text/markdown", created_at: "" }];
    fetchMock.mockResolvedValueOnce(jsonResponse(files));
    await expect(listSourceFiles("s 1")).resolves.toEqual(files);
    expect(fetchMock).toHaveBeenCalledWith("/api/sources/s%201/files");
  });

  it("coerces a null body to an empty array", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null));
    await expect(listSourceFiles("s1")).resolves.toEqual([]);
  });

  it("throws on an error status", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 403 }));
    await expect(listSourceFiles("s1")).rejects.toThrow("list source files failed: 403");
  });
});

describe("sourceFileContentUrl", () => {
  it("builds a content URL with the id and path encoded", () => {
    expect(sourceFileContentUrl("s 1", "docs/a b.md")).toBe(
      "/api/sources/s%201/files/content?path=docs%2Fa%20b.md",
    );
  });
});

describe("fetchSourceFileText", () => {
  it("returns the file body text", async () => {
    fetchMock.mockResolvedValueOnce({ ok: true, text: async () => "# Hi" } as Response);
    await expect(fetchSourceFileText("s1", "a.md")).resolves.toBe("# Hi");
    expect(fetchMock).toHaveBeenCalledWith("/api/sources/s1/files/content?path=a.md");
  });

  it("throws on an error status", async () => {
    fetchMock.mockResolvedValueOnce({ ok: false, status: 404 } as Response);
    await expect(fetchSourceFileText("s1", "a.md")).rejects.toThrow("fetch file failed: 404");
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
