"use client";

import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { useActiveWorkspace } from "@/lib/workspace-context";
import type { ChatMessage } from "./types";

export interface Conversation {
  id: string;
  messages: ChatMessage[];
}

interface ConversationContextValue {
  conversations: Conversation[];
  activeId: string;
  setActiveId: (id: string) => void;
  newConversation: () => void;
  updateMessages: (id: string, updater: (messages: ChatMessage[]) => ChatMessage[]) => void;
}

const ConversationContext = createContext<ConversationContextValue | null>(null);

function freshConversation(): Conversation {
  return { id: crypto.randomUUID(), messages: [] };
}

/**
 * ConversationProvider tracks chat threads for the active workspace so the
 * chat list can show more than one conversation and switch between them.
 * Threads live in memory only for now — persisting them server-side is
 * tracked separately (see the "Conversation memory" roadmap item), and
 * this is the seam where that would plug in: swap the local state below
 * for API-backed reads/writes without changing how Chat/ChatList consume it.
 */
export function ConversationProvider({ children }: { children: ReactNode }) {
  const { activeId: workspaceId } = useActiveWorkspace();
  const [conversations, setConversations] = useState<Conversation[]>(() => [freshConversation()]);
  const [activeId, setActiveId] = useState<string>(() => conversations[0].id);

  // Start over with a single fresh thread whenever the workspace changes —
  // conversations are scoped per workspace, same as sources.
  useEffect(() => {
    const initial = freshConversation();
    // eslint-disable-next-line react-hooks/set-state-in-effect -- resets chat threads on workspace switch
    setConversations([initial]);
    setActiveId(initial.id);
  }, [workspaceId]);

  function newConversation() {
    const conv = freshConversation();
    setConversations((cs) => [conv, ...cs]);
    setActiveId(conv.id);
  }

  function updateMessages(id: string, updater: (messages: ChatMessage[]) => ChatMessage[]) {
    setConversations((cs) =>
      cs.map((c) => (c.id === id ? { ...c, messages: updater(c.messages) } : c)),
    );
  }

  return (
    <ConversationContext.Provider
      value={{ conversations, activeId, setActiveId, newConversation, updateMessages }}
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
