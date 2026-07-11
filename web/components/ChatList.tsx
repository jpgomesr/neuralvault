"use client";

import { Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useConversations } from "@/lib/conversation-context";
import { cn } from "@/lib/utils";

const TITLE_MAX = 40;

function titleFor(messages: { role: string; content: string }[]): string {
  const first = messages.find((m) => m.role === "user")?.content.trim();
  if (!first) return "New chat";
  return first.length > TITLE_MAX ? `${first.slice(0, TITLE_MAX)}…` : first;
}

/**
 * ChatList shows the active workspace's chat threads and lets the user
 * switch between them or start a new one. Threads aren't persisted yet
 * (see ConversationProvider) — this is the UI shell that will read from a
 * real endpoint once that lands.
 */
export default function ChatList() {
  const { conversations, activeId, setActiveId, newConversation } = useConversations();

  return (
    <div>
      <Button
        type="button"
        variant="secondary"
        size="sm"
        className="mb-2 w-full justify-start gap-1.5"
        onClick={newConversation}
      >
        <Plus className="size-3.5" />
        New chat
      </Button>
      <ul className="flex flex-col gap-1">
        {conversations.map((c) => (
          <li key={c.id}>
            <button
              type="button"
              onClick={() => setActiveId(c.id)}
              className={cn(
                "w-full truncate rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent",
                c.id === activeId && "bg-accent font-medium",
              )}
            >
              {titleFor(c.messages)}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}
