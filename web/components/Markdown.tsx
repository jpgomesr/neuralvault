"use client";

import { useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Check, Copy } from "lucide-react";
import { cn } from "@/lib/utils";

// CodeBlock wraps a rendered <pre> with a copy-to-clipboard button, shown on
// hover. Used as react-markdown's `pre` renderer so it applies to every
// fenced code block, in both chat answers and file previews.
function CodeBlock({ children, ...props }: React.ComponentPropsWithoutRef<"pre">) {
  const preRef = useRef<HTMLPreElement>(null);
  const resetTimeout = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [copied, setCopied] = useState(false);

  async function onCopy() {
    const text = preRef.current?.textContent ?? "";
    await navigator.clipboard.writeText(text);
    setCopied(true);
    if (resetTimeout.current) clearTimeout(resetTimeout.current);
    resetTimeout.current = setTimeout(() => setCopied(false), 1500);
  }

  return (
    <div className="markdown-pre-wrap group/code relative">
      <pre ref={preRef} {...props}>
        {children}
      </pre>
      <button
        type="button"
        onClick={() => void onCopy()}
        title={copied ? "Copied!" : "Copy code"}
        aria-label={copied ? "Copied" : "Copy code"}
        className="absolute top-2 right-2 rounded-md border border-border bg-muted p-1 text-muted-foreground opacity-70 transition-opacity hover:text-foreground hover:opacity-100 group-hover/code:opacity-100"
      >
        {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
      </button>
    </div>
  );
}

/**
 * Markdown renders GitHub-flavored markdown. It is used both for assistant chat
 * answers and for previewing markdown source files. Styling lives in the
 * `.markdown` block in globals.css so it stays consistent across both callers.
 */
export default function Markdown({
  children,
  className,
}: {
  children: string;
  className?: string;
}) {
  return (
    <div className={cn("markdown", className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={{ pre: CodeBlock }}>
        {children}
      </ReactMarkdown>
    </div>
  );
}
