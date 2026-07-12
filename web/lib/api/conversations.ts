// Thin client over the Go API's conversation endpoints.

import type { Conversation, PersistedMessage } from "../types";

/** listConversations returns a workspace's conversations, most recently active first. */
export async function listConversations(workspaceId: string): Promise<Conversation[]> {
  const res = await fetch(`/api/conversations?workspace_id=${encodeURIComponent(workspaceId)}`);
  if (!res.ok) throw new Error(`list conversations failed: ${res.status}`);
  return (await res.json()) ?? [];
}

/** createConversation starts a new, untitled conversation in a workspace. */
export async function createConversation(workspaceId: string): Promise<Conversation> {
  const res = await fetch("/api/conversations", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ workspace_id: workspaceId }),
  });
  if (!res.ok) throw new Error(`create conversation failed: ${res.status}`);
  return res.json();
}

/** listConversationMessages returns a conversation's messages, oldest first. */
export async function listConversationMessages(conversationId: string): Promise<PersistedMessage[]> {
  const res = await fetch(`/api/conversations/${encodeURIComponent(conversationId)}/messages`);
  if (!res.ok) throw new Error(`list messages failed: ${res.status}`);
  return (await res.json()) ?? [];
}
