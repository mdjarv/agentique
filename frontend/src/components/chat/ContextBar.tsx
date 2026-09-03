import { AlertTriangle } from "lucide-react";
import { Progress } from "~/components/ui/progress";
import { formatTokens } from "~/lib/format";
import { contextPercent, contextTier } from "~/lib/session/context-tier";
import { cn } from "~/lib/utils";
import type { ContextUsage } from "~/stores/chat-store";

interface ContextBarProps {
  usage?: ContextUsage | null;
  compacting?: boolean;
}

/**
 * The desktop reading: a full row with the bar, the percentage and the token
 * count. The phone does not get this — there the meter is the composer's top
 * edge (`ContextEdge`), because a row of its own was 17px spent on a number
 * nobody acts on below 80%.
 *
 * Both read `lib/session/context-tier`, so the two surfaces cannot disagree
 * about what a percentage looks like.
 */
export function ContextBar({ usage, compacting }: ContextBarProps) {
  if (compacting) {
    return (
      <div className="flex items-center gap-2 shrink-0 px-4 py-1">
        <div className="flex-1 h-1.5 rounded-full overflow-hidden compact-stripes" />
        <span className="text-primary shrink-0 text-[11px]">Compacting...</span>
      </div>
    );
  }

  if (!usage) return null;

  const pct = contextPercent(usage);
  const tier = contextTier(pct);
  const used = usage.usedTokens ?? usage.inputTokens + usage.outputTokens;

  return (
    <div className="flex items-center gap-2 shrink-0 px-4 py-1">
      {tier.label && (
        <span className={cn("inline-flex items-center gap-1 shrink-0 text-[11px]", tier.text)}>
          <AlertTriangle className="size-3" />
          {tier.label}
        </span>
      )}
      <Progress value={pct} className={cn("flex-1 h-1.5", tier.track, tier.indicator)} />
      <span className={cn("tabular-nums shrink-0 text-[11px]", tier.text)}>
        {pct}%
        <span className="text-muted-foreground-faint ml-1">
          {formatTokens(used)}/{formatTokens(usage.contextWindow)}
        </span>
      </span>
    </div>
  );
}
