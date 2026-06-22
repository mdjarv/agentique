import { Brain, Check, Flag, Loader2, ThumbsUp } from "lucide-react";
import { memo, useCallback, useState } from "react";
import { toast } from "sonner";
import { confirmMemory, flagMemory } from "~/lib/brain-api";
import type { BrainFact } from "~/lib/prompt-parsing";
import { cn } from "~/lib/utils";

// BrainCard renders a `<brain>` recall envelope (parsed by splitByPromptBlocks) as a
// dedicated "Recalled from memory" card — visually distinct from the user's own
// message and from agent output (the agent-purple accent). Each fact carries the
// human side of the outcome loop: "Helpful" confirms it as ground truth, "Outdated"
// flags it for review. The agent still receives the raw <brain> text + ids in the
// prompt, so the model-driven MemoryUsed/MemoryFlag loop is untouched.

type FactStatus = "idle" | "confirming" | "flagging" | "helpful" | "flagged";

/** Render a fact's text with inline `code` spans styled, without pulling the full
 *  markdown renderer into this not-prose card. Splits on backtick pairs; odd segments
 *  are code. Facts are single-line sentences, so this covers the realistic case. */
function FactText({ text }: { text: string }) {
  const parts = text.split("`");
  return (
    <>
      {parts.map((part, i) =>
        i % 2 === 1 ? (
          // biome-ignore lint/suspicious/noArrayIndexKey: stable split of static text
          <code key={i} className="font-mono text-[0.85em] text-info">
            {part}
          </code>
        ) : (
          // biome-ignore lint/suspicious/noArrayIndexKey: stable split of static text
          <span key={i}>{part}</span>
        ),
      )}
    </>
  );
}

function shortId(id: string): string {
  if (id.length <= 9) return id;
  return `${id.slice(0, 4)}…${id.slice(-4)}`;
}

function FactRow({ fact }: { fact: BrainFact }) {
  const [status, setStatus] = useState<FactStatus>("idle");
  const busy = status === "confirming" || status === "flagging";
  const settled = status === "helpful" || status === "flagged";

  const act = useCallback(
    async (kind: "helpful" | "flag") => {
      if (busy || settled || !fact.id) return;
      setStatus(kind === "helpful" ? "confirming" : "flagging");
      try {
        if (kind === "helpful") {
          await confirmMemory(fact.id);
          setStatus("helpful");
          toast.success("Marked helpful — confirmed in memory");
        } else {
          await flagMemory(fact.id);
          setStatus("flagged");
          toast.success("Flagged as outdated for review");
        }
      } catch (err) {
        setStatus("idle");
        toast.error(err instanceof Error ? err.message : "Failed to update memory");
      }
    },
    [busy, settled, fact.id],
  );

  return (
    <div className="group/fact flex gap-2.5 py-2 border-t border-border/30 first:border-t-0">
      <span className="mt-[7px] h-1.5 w-1.5 shrink-0 rounded-full bg-agent shadow-[0_0_8px_var(--agent)]" />
      <div className="min-w-0 flex-1 text-[13px] leading-relaxed text-foreground">
        <FactText text={fact.text} />
        {fact.id && (
          <span
            className="ml-1.5 inline-block whitespace-nowrap rounded bg-muted-foreground/10 px-1.5 font-mono text-[10px] text-muted-foreground-faint align-middle"
            title={`Memory id: ${fact.id}`}
          >
            {shortId(fact.id)}
          </span>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-1">
        {status === "helpful" ? (
          <span className="flex items-center gap-1 text-[11px] text-success">
            <Check className="h-3 w-3" /> Helpful
          </span>
        ) : status === "flagged" ? (
          <span className="flex items-center gap-1 text-[11px] text-destructive">
            <Flag className="h-3 w-3" /> Flagged
          </span>
        ) : (
          <div
            className={cn(
              "flex items-center gap-0.5 transition-opacity",
              busy
                ? "opacity-100"
                : "opacity-0 group-hover/fact:opacity-100 focus-within:opacity-100",
            )}
          >
            <button
              type="button"
              disabled={busy || !fact.id}
              onClick={() => act("helpful")}
              className="rounded p-1 text-muted-foreground hover:bg-success/10 hover:text-success disabled:opacity-50"
              aria-label="Mark this memory as helpful"
              title="Helpful — confirm this memory"
            >
              {status === "confirming" ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                <ThumbsUp className="h-3 w-3" />
              )}
            </button>
            <button
              type="button"
              disabled={busy || !fact.id}
              onClick={() => act("flag")}
              className="rounded p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive disabled:opacity-50"
              aria-label="Flag this memory as outdated"
              title="Outdated — flag this memory for review"
            >
              {status === "flagging" ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                <Flag className="h-3 w-3" />
              )}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

export const BrainCard = memo(function BrainCard({ facts }: { facts: BrainFact[] }) {
  if (facts.length === 0) return null;
  return (
    <div className="not-prose relative my-3 overflow-hidden rounded-xl border border-agent/25 bg-card shadow-[0_18px_40px_-24px_rgba(0,0,0,0.8)]">
      {/* gradient wash + left accent rail mark this as a brain artifact */}
      <div className="pointer-events-none absolute inset-0 bg-gradient-to-b from-agent/[0.06] to-transparent to-40%" />
      <div className="absolute inset-y-0 left-0 w-[3px] bg-gradient-to-b from-agent to-primary" />
      <div className="relative">
        <div className="flex items-center gap-2.5 px-4 pt-3 pb-2">
          <span className="flex h-[26px] w-[26px] shrink-0 items-center justify-center rounded-lg bg-gradient-to-br from-agent to-primary text-background shadow-[0_4px_12px_-4px_var(--agent)]">
            <Brain className="h-[15px] w-[15px]" />
          </span>
          <span className="text-[13.5px] font-semibold text-foreground-bright">
            Recalled from memory
          </span>
          <span className="rounded-full border border-agent/25 bg-agent/10 px-1.5 font-mono text-[10.5px] text-agent">
            {facts.length}
          </span>
          <span className="ml-auto text-[11px] text-muted-foreground-faint">
            background context
          </span>
        </div>
        <div className="px-4 pb-3">
          {facts.map((fact, i) => (
            <FactRow key={fact.id || `fact-${i}`} fact={fact} />
          ))}
        </div>
      </div>
    </div>
  );
});
