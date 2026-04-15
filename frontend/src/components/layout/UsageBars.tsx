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

function getEffectiveUtilization(entry: RateLimitEntry | undefined): number {
  if (!entry) return 0;
  if (entry.resetsAt > 0 && Date.now() > entry.resetsAt * 1000) return 0;
  return entry.utilization;
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
  const pct = Math.round(util * 100);
  const tier = getTier(util);
  const colors = tierColors[tier];
  const isRejected = entry?.status === "rejected";
  const resetLabel = entry?.resetsAt ? formatResetTime(entry.resetsAt) : null;
  const statusLabel = entry ? (statusLabels[entry.status] ?? entry.status) : "No data";

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="flex flex-col">
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
                "text-[10px] tabular-nums w-7 text-right shrink-0",
                tier === "critical" ? "text-destructive" : "text-muted-foreground",
              )}
            >
              {pct}%
            </span>
          </div>
          {resetLabel && (
            <span className="text-[9px] text-muted-foreground-faint text-right leading-tight">
              {resetLabel}
            </span>
          )}
        </div>
      </TooltipTrigger>
      <TooltipContent side="top">
        <span>
          {statusLabel} &middot; {pct}% utilization
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

  // Tick every 60s so reset countdowns update live
  useEffect(() => {
    if (fiveHourReset <= 0 && sevenDayReset <= 0) return;
    const id = setInterval(() => setTick((t) => t + 1), 60_000);
    return () => clearInterval(id);
  }, [fiveHourReset, sevenDayReset]);

  // Only show bars when we have real utilization data from the CLI.
  // The CLI omits utilization for "allowed" status, so we hide rather than guess.
  const fiveHourUtil = getEffectiveUtilization(fiveHour);
  const sevenDayUtil = getEffectiveUtilization(sevenDay);
  const showFiveHour = fiveHourUtil > 0;
  const showSevenDay = sevenDayUtil > 0;

  if (!showFiveHour && !showSevenDay) return null;

  return (
    <div className="flex flex-col gap-1 mb-1.5">
      {showFiveHour && <UsageBar label="5h" entry={fiveHour} />}
      {showSevenDay && <UsageBar label="7d" entry={sevenDay} />}
    </div>
  );
}
