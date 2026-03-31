import { ArrowUpCircle } from "lucide-react";
import { useEffect, useState } from "react";
import { type VersionInfo, fetchVersion } from "~/lib/api";

export function UpdateIndicator() {
  const [info, setInfo] = useState<VersionInfo | null>(null);

  useEffect(() => {
    let cancelled = false;

    const check = async () => {
      try {
        const v = await fetchVersion();
        if (!cancelled) setInfo(v);
      } catch {
        // Silently ignore — not critical.
      }
    };

    // Initial check after short delay.
    const timeout = setTimeout(check, 5_000);

    // Re-check every 30 minutes.
    const interval = setInterval(check, 30 * 60_000);

    return () => {
      cancelled = true;
      clearTimeout(timeout);
      clearInterval(interval);
    };
  }, []);

  if (!info?.updateAvailable) return null;

  return (
    <div
      className="flex items-center gap-1.5 text-xs text-warning"
      title={`Update available: ${info.latestVersion}\nCurrent: ${info.version}\n\nRun: agentique upgrade`}
    >
      <ArrowUpCircle className="size-3 shrink-0" />
      <span>{info.latestVersion} available</span>
    </div>
  );
}
