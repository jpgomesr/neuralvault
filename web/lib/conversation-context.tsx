"use client";

import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { useActiveWorkspace } from "@/lib/workspace-context";
import {
  useConversationList,
  useConversationMessages,
  useCreateConversationMutation,
} from "@/hooks/use-conversations";
import type { ChatMessage, PersistedMessage } from "./types";

export interface Conversation {
  id: string;
  title: string;
  messages: ChatMessage[];
}

interface ConversationContextValue {
  conversations: Conversation[];
  activeId: string;
  setActiveId: (id: string) => void;
  newConversation: () => void;
  updateMessages: (id: string, updater: (messages: ChatMessage[]) => ChatMessage[]) => void;
  /** refreshConversations re-lists the workspace's conversations, picking up
   * a just-derived title or a reordering by recency. Call once the backend
   * has actually persisted something new — e.g. as soon as the "sources"
   * event confirms the question was saved, not after the full answer. */
  refreshConversations: () => void;
  /** ensureConversation returns the active conversation id, lazily creating
   * one via POST /conversations first if the composer is blank (activeId ===
   * ""). Lets the input stay usable before the user has picked or started a
   * thread — the conversation is only actually created on first send. */
  ensureConversation: () => Promise<string>;
}

const ConversationContext = createContext<ConversationContextValue | null>(null);

function toChatMessage(m: PersistedMessage): ChatMessage {
  return { role: m.Role, content: m.Content, sources: m.Sources?.results };
}

/**
 * ConversationProvider tracks chat threads for the active workspace so the
 * sidebar can list multiple conversations, switch between them, and start
 * new ones. Conversation summaries and history are read from the API
 * (`useConversationList`/`useConversationMessages`); `messagesById` is a
 * local buffer seeded from that history once per conversation and mutated
 * directly while a reply streams in. This mirrors `watchSourceStatus`'s SSE
 * exception to the usual TanStack Query flow: a live token stream isn't a
 * cacheable request/response resource, so it's handled outside the query
 * cache rather than forced into it.
 */
export function ConversationProvider({ children }: { children: ReactNode }) {
  const { activeId: workspaceId } = useActiveWorkspace();
  const [activeId, setActiveIdState] = useState<string>("");
  const [messagesById, setMessagesById] = useState<Record<string, ChatMessage[]>>({});

  const { data: list = [], refetch: refetchList } = useConversationList(workspaceId);
  const createMutation = useCreateConversationMutation(workspaceId);

  const needsHistory = activeId !== "" && messagesById[activeId] === undefined;
  const { data: history } = useConversationMessages(activeId, needsHistory);

  // Conversations are scoped per workspace: drop the selection whenever the
  // workspace changes so a conversation from workspace A never ends up
  // active while workspace B's list is showing.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- resets selection on workspace switch
    setActiveIdState("");
  }, [workspaceId]);

  // Default to the workspace's most recently active conversation once its
  // list has loaded, unless something has already been explicitly selected.
  useEffect(() => {
    if (activeId === "" && list.length > 0) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- derives the default selection once the list arrives
      setActiveIdState(list[0].ID);
    }
  }, [activeId, list]);

  // Seed the local message buffer the first time a conversation's history loads.
  useEffect(() => {
    if (needsHistory && history) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- seeds the live buffer from persisted history
      setMessagesById((m) => ({ ...m, [activeId]: history.map(toChatMessage) }));
    }
  }, [needsHistory, history, activeId]);

  function setActiveId(id: string) {
    setActiveIdState(id);
  }

  function newConversation() {
    // Just clears the composer — no network call. The conversation itself is
    // only created (via ensureConversation) once the user actually sends a
    // message, so switching to "new chat" and changing your mind doesn't
    // litter the workspace with empty conversations.
    setActiveIdState("");
  }

  function updateMessages(id: string, updater: (messages: ChatMessage[]) => ChatMessage[]) {
    setMessagesById((m) => ({ ...m, [id]: updater(m[id] ?? []) }));
  }

  function refreshConversations() {
    void refetchList();
  }

  async function ensureConversation(): Promise<string> {
    if (activeId !== "") return activeId;
    const conv = await createMutation.mutateAsync();
    setMessagesById((m) => ({ ...m, [conv.ID]: [] }));
    setActiveIdState(conv.ID);
    return conv.ID;
  }

  const conversations: Conversation[] = list.map((c) => ({
    id: c.ID,
    title: c.Title,
    messages: messagesById[c.ID] ?? [],
  }));

  return (
    <ConversationContext.Provider
      value={{
        conversations,
        activeId,
        setActiveId,
        newConversation,
        updateMessages,
        refreshConversations,
        ensureConversation,
      }}
    >
      {children}
    </ConversationContext.Provider>
  );
}

/** useConversations reads and controls the active workspace's chat threads. */
export function useConversations() {
  const ctx = useContext(ConversationContext);
  if (!ctx) throw new Error("useConversations must be used within a ConversationProvider");
  return ctx;
}
