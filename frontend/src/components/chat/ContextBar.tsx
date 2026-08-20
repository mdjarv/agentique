import { AlertTriangle } from "lucide-react";
import { Progress } from "~/components/ui/progress";
import { formatTokens } from "~/lib/format";
import { cn } from "~/lib/utils";
import type { ContextUsage } from "~/stores/chat-store";

interface ContextBarProps {
  usage?: ContextUsage | null;
  compacting?: boolean;
  /** Slim variant for mobile — thinner bar, tighter padding, smaller label. */
  compact?: boolean;
}

interface Tier {
  label: string;
  bar: string;
  track: string;
  text: string;
}

function getTier(pct: number): Tier {
  if (pct >= 95) {
    return {
      label: "Critical",
      bar: "[&>[data-slot=progress-indicator]]:bg-red-500",
      track: "bg-red-500/15",
      text: "text-red-500",
    };
  }
  if (pct >= 80) {
    return {
      label: "High Usage",
      bar: "[&>[data-slot=progress-indicator]]:bg-orange-500",
      track: "bg-orange-500/15",
      text: "text-orange-400",
    };
  }
  if (pct >= 60) {
    return {
      label: "",
      bar: "[&>[data-slot=progress-indicator]]:bg-amber-500",
      track: "bg-amber-500/10",
      text: "text-muted-foreground",
    };
  }
  return {
    label: "",
    bar: "[&>[data-slot=progress-indicator]]:bg-emerald-500",
    track: "bg-emerald-500/10",
    text: "text-muted-foreground",
  };
}

export function ContextBar({ usage, compacting, compact }: ContextBarProps) {
  const pad = compact ? "px-3 py-0.5" : "px-4 py-1";
  const barH = compact ? "h-1" : "h-1.5";
  const txt = compact ? "text-[10px]" : "text-[11px]";

  if (compacting) {
    return (
      <div className={cn("flex items-center gap-2 shrink-0", pad)}>
        <div className={cn("flex-1 rounded-full overflow-hidden compact-stripes", barH)} />
        <span className={cn("text-primary shrink-0", txt)}>Compacting...</span>
      </div>
    );
  }

  if (!usage) return null;

  const used = usage.inputTokens + usage.outputTokens;
  const pct = Math.min(Math.round((used / usage.contextWindow) * 100), 100);
  const tier = getTier(pct);

  return (
    <div className={cn("flex items-center gap-2 shrink-0", pad)}>
      {tier.label && (
        <span className={cn("inline-flex items-center gap-1 shrink-0", txt, tier.text)}>
          <AlertTriangle className="size-3" />
          {tier.label}
        </span>
      )}
      <Progress value={pct} className={cn("flex-1", barH, tier.track, tier.bar)} />
      <span className={cn("tabular-nums shrink-0", txt, tier.text)}>
        {pct}%
        <span className="text-muted-foreground-faint ml-1">
          {formatTokens(used)}/{formatTokens(usage.contextWindow)}
        </span>
      </span>
    </div>
  );
}
