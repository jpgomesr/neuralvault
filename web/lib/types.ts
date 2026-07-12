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
// source_id/file_path aren't sent by the API yet (see issue tracking that) —
// until then a citation can't be resolved back to a viewable file.
export interface SourceChunk {
  chunk_id: string;
  content: string;
  score: number;
  source_id?: string;
  file_path?: string;
}

// SourceProgress is one event from GET /sources/{id}/status.
export interface SourceProgress {
  type: "indexing" | "done" | "error";
  file?: string;
  chunks?: number;
  total?: number;
  error?: string;
}

// Conversation is a persisted chat thread (GET/POST /conversations). Like
// Workspace and Source, it's serialised from a Go struct without json tags,
// so its fields are PascalCase.
export interface Conversation {
  ID: string;
  WorkspaceID: string;
  Title: string;
  CreatedAt: string;
  UpdatedAt: string;
}

// PersistedMessage is one turn in a Conversation (GET /conversations/{id}/messages),
// also PascalCase for the same reason. Sources mirrors the query "sources"
// event's payload shape and is null for a user message.
export interface PersistedMessage {
  ID: string;
  ConversationID: string;
  Role: "user" | "assistant";
  Content: string;
  Sources: { results: SourceChunk[] } | null;
  CreatedAt: string;
}

// ChatMessage is one turn in a chat thread, as rendered by Chat.tsx. It's the
// in-memory view built from PersistedMessage history plus live-streaming
// patches (see ConversationProvider) — not sent to or received from the API
// directly.
export interface ChatMessage {
  role: "user" | "assistant";
  content: string;
  sources?: SourceChunk[];
  streaming?: boolean;
}
