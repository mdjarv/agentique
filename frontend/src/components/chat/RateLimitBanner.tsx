import { AlertTriangle } from "lucide-react";
import type { RateLimitInfo } from "~/stores/chat-store";

interface RateLimitBannerProps {
  rateLimit: RateLimitInfo;
}

const statusLabels: Record<string, string> = {
  approaching: "Rate limit approaching",
  active: "Rate limit active",
  exceeded: "Rate limit exceeded",
};

export function RateLimitBanner({ rateLimit }: RateLimitBannerProps) {
  const label = statusLabels[rateLimit.status] ?? `Rate limit: ${rateLimit.status}`;
  const pct = Math.round(rateLimit.utilization * 100);

  return (
    <div className="mx-4 mb-2 rounded-md border border-yellow-500/40 bg-yellow-500/10 px-3 py-1.5">
      <div className="flex items-center gap-2 text-xs text-yellow-700 dark:text-yellow-400">
        <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
        <span className="font-medium">{label}</span>
        <span className="text-yellow-600/70 dark:text-yellow-500/70">{pct}% utilization</span>
      </div>
    </div>
  );
}
