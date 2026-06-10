import { Link } from "@tanstack/react-router";
import { HardDrive } from "lucide-react";
import { useEffect } from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "~/components/ui/tooltip";
import { cn, formatBytes } from "~/lib/utils";
import { useStorageStore } from "~/stores/storage-store";

type Tier = "normal" | "warning" | "critical";

const tierColors: Record<Tier, { track: string; fill: string; text: string }> = {
  normal: { track: "bg-primary/20", fill: "bg-primary", text: "text-muted-foreground" },
  warning: { track: "bg-warning/20", fill: "bg-warning", text: "text-warning" },
  critical: { track: "bg-destructive/20", fill: "bg-destructive", text: "text-destructive" },
};

const GB = 1024 ** 3;

function getTier(freeBytes: number, totalBytes: number): Tier {
  const frac = totalBytes > 0 ? freeBytes / totalBytes : 1;
  if (frac < 0.05 || freeBytes < 5 * GB) return "critical";
  if (frac < 0.1 || freeBytes < 10 * GB) return "warning";
  return "normal";
}

/**
 * Compact always-visible indicator of free space on the volume holding the data
 * directory. The fill grows (and reddens) as the disk fills; clicking opens the
 * Storage view. Polls the cheap /storage/disk endpoint every 60s.
 */
export function DiskUsageBar() {
  const disk = useStorageStore((s) => s.disk);
  const fetchDiskStats = useStorageStore((s) => s.fetchDiskStats);

  useEffect(() => {
    fetchDiskStats();
    const id = setInterval(fetchDiskStats, 60_000);
    return () => clearInterval(id);
  }, [fetchDiskStats]);

  if (!disk || disk.totalBytes === 0) return null;

  const tier = getTier(disk.freeBytes, disk.totalBytes);
  const colors = tierColors[tier];
  const usedPct = Math.min(Math.round(disk.usagePercent), 100);

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Link to="/storage" className="flex items-center gap-2 mb-1.5">
          <HardDrive className={cn("size-3 shrink-0", colors.text)} />
          <div className={cn("flex-1 h-1.5 rounded-full", colors.track)}>
            <div
              className={cn("h-full rounded-full transition-all duration-500", colors.fill)}
              style={{ width: `${usedPct}%` }}
            />
          </div>
          <span className={cn("text-[10px] tabular-nums text-right shrink-0", colors.text)}>
            {formatBytes(disk.freeBytes)} free
          </span>
        </Link>
      </TooltipTrigger>
      <TooltipContent side="top">
        <span>
          {formatBytes(disk.freeBytes)} free of {formatBytes(disk.totalBytes)} ({usedPct}% used)
        </span>
      </TooltipContent>
    </Tooltip>
  );
}
