/**
 * How full the context is, drawn as the composer's own top edge.
 *
 * The phone had a whole row for this — a bar, a percentage and a token count,
 * 17px of a 427px viewport — reporting a number nobody acts on below 80%. The
 * edge between the transcript and the composer is a line that has to be drawn
 * anyway, so the meter costs its extra pixel and nothing more.
 *
 * It escalates rather than reporting continuously, which is the split
 * `docs/usage.md` draws between a gauge and an allowance: a level that resets
 * (compaction) and that changes what you should type next has earned the right
 * to speak up. Below 80% it is 2px of colour; at 80 it grows a band and gets
 * its words back, and the exact token figure stays in the session sheet where
 * it was never urgent.
 *
 * The colour comes from `lib/session/context-tier`, the same table the
 * desktop's `ContextBar` reads, so 84% cannot look like two different things on
 * two surfaces. It is never drawn in the ink colour: a neutral 2px line reads
 * as a divider that happens to be short.
 */
import { AlertTriangle } from "lucide-react";
import { memo } from "react";
import { contextEscalated, contextPercent, contextTier } from "~/lib/session/context-tier";
import { cn } from "~/lib/utils";
import type { ContextUsage } from "~/stores/chat-store";

interface ContextEdgeProps {
  usage?: ContextUsage | null;
  /** A compaction is running — the length means nothing until it lands. */
  compacting?: boolean;
}

export const ContextEdge = memo(function ContextEdge({ usage, compacting }: ContextEdgeProps) {
  if (compacting) {
    return <div className="h-0.5 w-full shrink-0 compact-stripes" aria-hidden="true" />;
  }

  if (!usage) return null;

  const pct = contextPercent(usage);
  const tier = contextTier(pct);
  const loud = contextEscalated(pct);

  return (
    <div className="shrink-0">
      {/* Spoken rather than roled: a 2px line is a graphic, and `<meter>` is
          unstylable at this size. The words are the accessible reading. */}
      <span className="sr-only">Context {pct}% used</span>
      <div className={cn("h-0.5 w-full", tier.track)} aria-hidden="true">
        <div
          className={cn("h-full transition-[width] duration-500", tier.bar)}
          style={{ width: `${pct}%` }}
        />
      </div>
      {loud && (
        <div
          className={cn(
            "flex items-center gap-1.5 px-3 py-0.5 text-[10px] font-medium",
            tier.text,
            tier.track,
          )}
        >
          <AlertTriangle className="size-3 shrink-0" />
          <span>
            {tier.label} &middot; {pct}% of context
          </span>
        </div>
      )}
    </div>
  );
});
