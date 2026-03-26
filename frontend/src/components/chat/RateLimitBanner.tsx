import { AlertTriangle } from "lucide-react";
import type { RateLimitInfo } from "~/stores/chat-store";

interface RateLimitBannerProps {
  rateLimit: RateLimitInfo;
}

const statusLabels: Record<string, string> = {
  allowed_warning: "Rate limit warning",
  rejected: "Rate limited",
};

function formatResetsIn(resetsAt: number): string | null {
  const diffMs = resetsAt * 1000 - Date.now();
  if (diffMs <= 0) return null;

  const totalMin = Math.ceil(diffMs / 60_000);
  if (totalMin < 60) return `resets in ${totalMin}m`;

  const h = Math.floor(totalMin / 60);
  const m = totalMin % 60;
  return m > 0 ? `resets in ${h}h ${m}m` : `resets in ${h}h`;
}

export function RateLimitBanner({ rateLimit }: RateLimitBannerProps) {
  const label = statusLabels[rateLimit.status] ?? "Rate limited";
  const pct = Math.round(rateLimit.utilization * 100);
  const resetLabel = rateLimit.resetsAt ? formatResetsIn(rateLimit.resetsAt) : null;

  return (
    <div className="mx-4 my-1 rounded-md border border-yellow-500/40 bg-yellow-500/10 px-3 py-1.5 shrink-0">
      <div className="flex items-center gap-2 text-xs text-yellow-700 dark:text-yellow-400">
        <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
        <span className="font-medium">{label}</span>
        <span className="text-yellow-600/70 dark:text-yellow-500/70">
          {pct}% utilization{resetLabel ? ` \u00b7 ${resetLabel}` : ""}
        </span>
      </div>
    </div>
  );
}
