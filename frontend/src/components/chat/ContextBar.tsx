import { Progress } from "~/components/ui/progress";
import { cn } from "~/lib/utils";
import type { ContextUsage } from "~/stores/chat-store";

interface ContextBarProps {
  usage: ContextUsage;
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${Math.round(n / 1_000)}k`;
  return String(n);
}

type Tier = { label: string; bar: string; track: string; text: string };

function getTier(pct: number): Tier {
  if (pct >= 95) {
    return {
      label: "compaction imminent",
      bar: "[&>[data-slot=progress-indicator]]:bg-red-500",
      track: "bg-red-500/15",
      text: "text-red-500",
    };
  }
  if (pct >= 80) {
    return {
      label: "compaction approaching",
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

export function ContextBar({ usage }: ContextBarProps) {
  const used = usage.inputTokens + usage.outputTokens;
  const pct = Math.min(Math.round((used / usage.contextWindow) * 100), 100);
  const tier = getTier(pct);

  return (
    <div className="flex items-center gap-2 px-4 py-1">
      <Progress value={pct} className={cn("h-1.5 flex-1", tier.track, tier.bar)} />
      <span className={cn("text-[11px] tabular-nums shrink-0", tier.text)}>
        {pct}%
        <span className="text-muted-foreground/60 ml-1">
          {formatTokens(used)}/{formatTokens(usage.contextWindow)}
        </span>
        {tier.label && <span className={cn("ml-1.5", tier.text)}>{tier.label}</span>}
      </span>
    </div>
  );
}
