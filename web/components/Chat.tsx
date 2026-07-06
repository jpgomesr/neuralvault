"use client";

import { useEffect, useRef, useState } from "react";
import { streamQuery } from "@/lib/api";
import type { SourceChunk } from "@/lib/types";

interface Message {
  role: "user" | "assistant";
  content: string;
  sources?: SourceChunk[];
  streaming?: boolean;
}

/**
 * Chat renders a multi-turn conversation for the active workspace. On submit it
 * streams the answer from /query/stream, showing the grounding sources as chips
 * and appending tokens as they arrive.
 */
export default function Chat({ workspaceId }: { workspaceId: string }) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const endRef = useRef<HTMLDivElement>(null);

  // Reset the conversation when the workspace changes.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- resets local state on workspace switch
    setMessages([]);
  }, [workspaceId]);

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    const question = input.trim();
    if (!question || busy) return;

    setInput("");
    setBusy(true);
    // Append the user turn and an empty assistant turn we stream into.
    setMessages((m) => [
      ...m,
      { role: "user", content: question },
      { role: "assistant", content: "", streaming: true },
    ]);

    const patchAssistant = (fn: (msg: Message) => Message) =>
      setMessages((m) => {
        const next = [...m];
        for (let i = next.length - 1; i >= 0; i--) {
          if (next[i].role === "assistant") {
            next[i] = fn(next[i]);
            break;
          }
        }
        return next;
      });

    await streamQuery(workspaceId, question, {
      onSources: (chunks) => patchAssistant((msg) => ({ ...msg, sources: chunks })),
      onToken: (text) => patchAssistant((msg) => ({ ...msg, content: msg.content + text })),
      onDone: () => patchAssistant((msg) => ({ ...msg, streaming: false })),
      onError: (message) =>
        patchAssistant((msg) => ({
          ...msg,
          streaming: false,
          content: msg.content || `⚠️ ${message}`,
        })),
    });

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
              {m.content || (m.streaming ? "…" : "")}
            </div>
            {m.sources && m.sources.length > 0 && (
              <div className="sources">
                {m.sources.map((s) => (
                  <span className="chip" key={s.chunk_id} title={s.content}>
                    {s.content.slice(0, 60)} · {s.score.toFixed(2)}
                  </span>
                ))}
              </div>
            )}
          </div>
        ))}
        <div ref={endRef} />
      </div>

      <form className="composer" onSubmit={onSubmit}>
        <input
          type="text"
          placeholder="Ask a question…"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          disabled={busy}
        />
        <button className="btn" type="submit" disabled={busy || !input.trim()}>
          {busy ? "…" : "Send"}
        </button>
      </form>
    </section>
  );
}
