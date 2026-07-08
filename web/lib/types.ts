// Types mirror the Go API responses. Workspace and Source are serialised from
// Go structs without json tags, so their fields are PascalCase.

export interface Me {
  id: string;
  email: string;
}

export interface Workspace {
  ID: string;
  Name: string;
  CreatedAt: string;
  UpdatedAt: string;
}

export interface Source {
  ID: string;
  Name: string;
  Status: string;
}

// SourceFile is one original file stored for a source (GET /sources/{id}/files).
// It has json tags on the Go side, so its fields are snake_case.
export interface SourceFile {
  id: string;
  source_id: string;
  name: string;
  size: number;
  content_type: string;
  created_at: string;
}

// SourceChunk is one grounding chunk returned in the query "sources" event.
export interface SourceChunk {
  chunk_id: string;
  content: string;
  score: number;
}

// SourceProgress is one event from GET /sources/{id}/status.
export interface SourceProgress {
  type: "indexing" | "done" | "error";
  file?: string;
  chunks?: number;
  total?: number;
  error?: string;
}
