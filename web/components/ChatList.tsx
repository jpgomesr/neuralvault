"use client";

import { Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useConversations } from "@/lib/conversation-context";
import { cn } from "@/lib/utils";

/**
 * ChatList shows the active workspace's chat threads and lets the user
 * switch between them or start a new one. Titles are derived server-side
 * from each conversation's first message (see internal/conversations).
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
      {conversations.length === 0 && (
        <p className="px-2 text-sm text-muted-foreground">No conversations yet.</p>
      )}
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
              {c.title || "New chat"}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}
