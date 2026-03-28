import { AlertTriangle } from "lucide-react";
import { Progress } from "~/components/ui/progress";
import { cn } from "~/lib/utils";
import type { ContextUsage } from "~/stores/chat-store";

interface ContextBarProps {
  usage?: ContextUsage | null;
  compacting?: boolean;
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${Math.round(n / 1_000)}k`;
  return String(n);
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

export function ContextBar({ usage, compacting }: ContextBarProps) {
  if (compacting) {
    return (
      <div className="flex items-center gap-2 px-4 py-1 shrink-0">
        <div className="h-1.5 flex-1 rounded-full overflow-hidden compact-stripes" />
        <span className="text-[11px] text-primary shrink-0">Compacting...</span>
      </div>
    );
  }

  if (!usage) return null;

  const used = usage.inputTokens + usage.outputTokens;
  const pct = Math.min(Math.round((used / usage.contextWindow) * 100), 100);
  const tier = getTier(pct);

  return (
    <div className="flex items-center gap-2 px-4 py-1 shrink-0">
      {tier.label && (
        <span className={cn("inline-flex items-center gap-1 text-[11px] shrink-0", tier.text)}>
          <AlertTriangle className="size-3" />
          {tier.label}
        </span>
      )}
      <Progress value={pct} className={cn("h-1.5 flex-1", tier.track, tier.bar)} />
      <span className={cn("text-[11px] tabular-nums shrink-0", tier.text)}>
        {pct}%
        <span className="text-muted-foreground/60 ml-1">
          {formatTokens(used)}/{formatTokens(usage.contextWindow)}
        </span>
      </span>
    </div>
  );
}
