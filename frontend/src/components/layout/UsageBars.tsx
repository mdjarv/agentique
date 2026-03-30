import { useEffect, useState } from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "~/components/ui/tooltip";
import { cn } from "~/lib/utils";
import type { RateLimitEntry } from "~/stores/rate-limit-store";
import { useRateLimitStore } from "~/stores/rate-limit-store";

type Tier = "normal" | "warning" | "critical";

const tierColors: Record<Tier, { track: string; fill: string }> = {
  normal: { track: "bg-primary/20", fill: "bg-primary" },
  warning: { track: "bg-warning/20", fill: "bg-warning" },
  critical: { track: "bg-destructive/20", fill: "bg-destructive" },
};

function getTier(utilization: number): Tier {
  if (utilization >= 0.9) return "critical";
  if (utilization >= 0.7) return "warning";
  return "normal";
}

// When CLI doesn't send utilization (v2.1.87+), estimate from status.
const STATUS_UTILIZATION: Record<string, number> = {
  allowed: 0.35,
  allowed_warning: 0.8,
  rejected: 1.0,
};

function getEffectiveUtilization(entry: RateLimitEntry | undefined): number {
  if (!entry) return 0;
  if (entry.resetsAt > 0 && Date.now() > entry.resetsAt * 1000) return 0;
  if (entry.utilization > 0) return entry.utilization;
  return STATUS_UTILIZATION[entry.status] ?? 0;
}

function isEstimated(entry: RateLimitEntry | undefined): boolean {
  return !!entry && entry.utilization === 0 && entry.status in STATUS_UTILIZATION;
}

function formatResetTime(resetsAt: number): string | null {
  const diffMs = resetsAt * 1000 - Date.now();
  if (diffMs <= 0) return null;
  const totalMin = Math.ceil(diffMs / 60_000);
  if (totalMin < 60) return `resets in ${totalMin}m`;
  const h = Math.floor(totalMin / 60);
  const m = totalMin % 60;
  return m > 0 ? `resets in ${h}h ${m}m` : `resets in ${h}h`;
}

const statusLabels: Record<string, string> = {
  allowed: "OK",
  allowed_warning: "Warning",
  rejected: "Rate limited",
};

function UsageBar({ label, entry }: { label: string; entry: RateLimitEntry | undefined }) {
  const util = getEffectiveUtilization(entry);
  const estimated = isEstimated(entry);
  const pct = Math.round(util * 100);
  const tier = getTier(util);
  const colors = tierColors[tier];
  const isRejected = entry?.status === "rejected";
  const resetLabel = entry?.resetsAt ? formatResetTime(entry.resetsAt) : null;
  const statusLabel = entry ? (statusLabels[entry.status] ?? entry.status) : "No data";
  const pctPrefix = estimated ? "~" : "";

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="flex items-center gap-2">
          <span className="text-[10px] text-muted-foreground w-4 shrink-0">{label}</span>
          <div className={cn("flex-1 h-1.5 rounded-full", colors.track)}>
            <div
              className={cn(
                "h-full rounded-full transition-all duration-500",
                colors.fill,
                isRejected && "animate-pulse",
              )}
              style={{ width: `${Math.min(pct, 100)}%` }}
            />
          </div>
          <span
            className={cn(
              "text-[10px] tabular-nums w-9 text-right shrink-0",
              tier === "critical" ? "text-destructive" : "text-muted-foreground",
            )}
          >
            {pctPrefix}
            {pct}%
          </span>
        </div>
      </TooltipTrigger>
      <TooltipContent side="top">
        <span>
          {statusLabel} &middot; {pctPrefix}
          {pct}% utilization
          {resetLabel ? ` \u00b7 ${resetLabel}` : ""}
        </span>
      </TooltipContent>
    </Tooltip>
  );
}

export function UsageBars() {
  const fiveHour = useRateLimitStore((s) => s.entries.five_hour);
  const sevenDay = useRateLimitStore((s) => s.entries.seven_day);

  // Force re-render when a resetsAt time passes so getEffectiveUtilization re-evaluates
  const [, setTick] = useState(0);
  const fiveHourReset = fiveHour?.resetsAt ?? 0;
  const sevenDayReset = sevenDay?.resetsAt ?? 0;
  useEffect(() => {
    const timers: ReturnType<typeof setTimeout>[] = [];
    for (const resetsAt of [fiveHourReset, sevenDayReset]) {
      if (!resetsAt) continue;
      const ms = resetsAt * 1000 - Date.now();
      if (ms > 0 && ms < 6 * 3600_000) {
        timers.push(setTimeout(() => setTick((t) => t + 1), ms + 500));
      }
    }
    return () => timers.forEach(clearTimeout);
  }, [fiveHourReset, sevenDayReset]);

  const sevenDayUtil = getEffectiveUtilization(sevenDay);
  const showWeekly = sevenDay && sevenDayUtil > 0.7;

  // Don't render anything if we've never received rate limit data
  if (!fiveHour && !sevenDay) return null;

  return (
    <div className="flex flex-col gap-1 mb-1.5">
      <UsageBar label="5h" entry={fiveHour} />
      {showWeekly && <UsageBar label="7d" entry={sevenDay} />}
    </div>
  );
}
