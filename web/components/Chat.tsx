"use client";

import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { streamQuery, type ModelOverride } from "@/lib/api/query";
import { useConversations } from "@/lib/conversation-context";
import { useSources } from "@/hooks/use-sources";
import type { ChatMessage, SourceChunk } from "@/lib/types";
import Markdown from "@/components/Markdown";
import ModelPicker from "@/components/ModelPicker";
import SourceFilesDialog from "@/components/SourceFilesDialog";

// Stable fallback reference so the "no conversation found" case doesn't
// produce a new array on every render (which would retrigger effects keyed
// off `messages`).
const EMPTY_MESSAGES: ChatMessage[] = [];

/**
 * Chat renders the active conversation thread for the active workspace. On
 * submit it streams the answer from /query/stream, showing the grounding
 * sources as chips and appending tokens as they arrive.
 */
export default function Chat({ workspaceId }: { workspaceId: string }) {
  const { conversations, activeId, updateMessages, refreshConversations, ensureConversation } = useConversations();
  const messages = conversations.find((c) => c.id === activeId)?.messages ?? EMPTY_MESSAGES;
  const { data: sources = [] } = useSources(workspaceId);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  // A per-request model override. Null means the workspace's saved default,
  // which is the only thing that persists — picking a model here does not
  // change the workspace setting.
  const [model, setModel] = useState<ModelOverride | null>(null);
  const [preview, setPreview] = useState<{ sourceId: string; sourceName: string; file?: string } | null>(
    null,
  );
  const endRef = useRef<HTMLDivElement>(null);

  // openCitation resolves a grounding chunk back to its source and opens the
  // file viewer. Disabled until the API sends source_id on each chunk.
  function openCitation(chunk: SourceChunk) {
    if (!chunk.source_id) return;
    const source = sources.find((s) => s.ID === chunk.source_id);
    setPreview({ sourceId: chunk.source_id, sourceName: source?.Name ?? "Source", file: chunk.file_path });
  }

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    const question = input.trim();
    if (!question || busy) return;

    setInput("");
    setBusy(true);

    // Lazily creates the conversation on first send, so the composer stays
    // usable before the user has picked or started a thread. Held in a plain
    // local from here on, so a reply keeps landing in the thread it started
    // in even if the user switches to a different conversation before it
    // finishes.
    let conversationId: string;
    try {
      conversationId = await ensureConversation();
    } catch {
      setInput(question);
      setBusy(false);
      return;
    }

    // Append the user turn and an empty assistant turn we stream into.
    updateMessages(conversationId, (m) => [
      ...m,
      { role: "user", content: question },
      { role: "assistant", content: "", streaming: true },
    ]);

    const patchAssistant = (fn: (msg: ChatMessage) => ChatMessage) =>
      updateMessages(conversationId, (m) => {
        const next = [...m];
        for (let i = next.length - 1; i >= 0; i--) {
          if (next[i].role === "assistant") {
            next[i] = fn(next[i]);
            break;
          }
        }
        return next;
      });

    await streamQuery(
      workspaceId,
      question,
      {
        onSources: (chunks) => {
          patchAssistant((msg) => ({ ...msg, sources: chunks }));
          // The question (and its derived title, if this was the first
          // message) is already persisted by the time this event arrives —
          // no need to wait for the full answer to refresh the sidebar.
          refreshConversations();
        },
        onToken: (text) => patchAssistant((msg) => ({ ...msg, content: msg.content + text })),
        onDone: () => patchAssistant((msg) => ({ ...msg, streaming: false })),
        onError: (message) =>
          patchAssistant((msg) => ({
            ...msg,
            streaming: false,
            content: msg.content || `⚠️ ${message}`,
          })),
      },
      conversationId,
      undefined,
      model ?? undefined,
    );

    setBusy(false);
  }

  return (
    <section className="chat">
      <div className="messages">
        {messages.length === 0 && (
          <div className="hint">Ask a question about the sources in this workspace.</div>
        )}
        {messages.map((m, i) => (
          <div className={`msg ${m.role}`} key={i}>
            <div className="role">{m.role}</div>
            <div className="bubble">
              {m.role === "assistant" && m.content ? (
                <Markdown>{m.content}</Markdown>
              ) : (
                m.content || (m.streaming ? "…" : "")
              )}
            </div>
            {m.sources && m.sources.length > 0 && (
              <div className="sources">
                {m.sources.map((s) => (
                  <button
                    type="button"
                    className="chip"
                    key={s.chunk_id}
                    title={s.source_id ? "View source file" : s.content}
                    disabled={!s.source_id}
                    onClick={() => openCitation(s)}
                  >
                    {s.content.slice(0, 60)} · {s.score.toFixed(2)}
                  </button>
                ))}
              </div>
            )}
          </div>
        ))}
        <div ref={endRef} />
      </div>

      <form className="composer" onSubmit={onSubmit}>
        <ModelPicker
          workspaceId={workspaceId}
          value={model}
          onChange={setModel}
          disabled={busy}
        />
        <Input
          type="text"
          placeholder="Ask a question…"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          disabled={busy}
        />
        <Button type="submit" disabled={busy || !input.trim()}>
          {busy ? "…" : "Send"}
        </Button>
      </form>

      {preview && (
        <SourceFilesDialog
          sourceId={preview.sourceId}
          sourceName={preview.sourceName}
          initialFile={preview.file}
          open={preview !== null}
          onOpenChange={(o) => !o && setPreview(null)}
        />
      )}
    </section>
  );
}
