/**
 * The compact usage indicator (docs/usage.md).
 *
 * One group per agent: a run of vertical meters, one per limit window, then
 * that vendor's mark. The point is peripheral glanceability — you should read
 * "how much room is left" without reading any text, so no numbers appear at
 * this level. Colour does the warning, the mark does the identification.
 *
 * Two rules that came from getting it wrong elsewhere:
 *   - A window at 0% still draws a visible stub. An empty track reads as a
 *     MISSING agent rather than an unused one.
 *   - When no agent has a usable window, the component renders nothing at all.
 *     A row of zeros is a worse lie than silence.
 */

import { agentColor, ProviderMark } from "~/components/usage/ProviderMark";
import type { UsageDocument } from "~/lib/generated-types";
import { drawFraction, isGauge, limitTier, meteredAgents, usableLimits } from "~/lib/usage-api";
import { cn } from "~/lib/utils";

/** A meter never drops below this, so "unused" and "absent" stay distinct. */
const STUB = 8;

const TIER_FILL: Record<string, string> = {
  warning: "var(--warning)",
  critical: "var(--destructive)",
};

export function UsageCluster({
  doc,
  className,
}: {
  doc: UsageDocument | null;
  className?: string;
}) {
  const agents = meteredAgents(doc);
  if (agents.length === 0) return null;

  return (
    <span className={cn("flex items-center gap-2.5", className)}>
      {agents.map((agent) => {
        const limits = usableLimits(agent);
        const gauge = isGauge(agent);
        const identity = agentColor(agent.id);
        return (
          <span key={agent.id} className="flex items-center gap-1.5" title={agent.name}>
            <span className="flex h-[13px] items-end gap-[3.5px]">
              {limits.map((limit) => {
                const tier = limitTier(limit, gauge);
                const pct = Math.max(Math.round(drawFraction(limit.percent) * 100), STUB);
                return (
                  <span
                    key={limit.label}
                    className="relative block w-[2.5px] overflow-hidden rounded-full bg-border/70"
                    style={{ height: "100%" }}
                  >
                    <span
                      className="absolute inset-x-0 bottom-0 block rounded-full"
                      style={{
                        height: `${pct}%`,
                        background: TIER_FILL[tier] ?? identity,
                        // A stub is a placeholder, not a reading — say so by
                        // holding it back rather than drawing it at full strength.
                        opacity: limit.percent <= 0 ? 0.4 : 1,
                      }}
                    />
                  </span>
                );
              })}
            </span>
            <ProviderMark
              id={agent.id}
              className="size-[11px] shrink-0 opacity-80"
              // The mark carries identity colour even when a window is hot:
              // severity belongs to the window that is hot, not to the vendor.
            />
          </span>
        );
      })}
    </span>
  );
}
