import { useConnectionStatus } from "~/hooks/useConnectionStatus";

export function ConnectionIndicator() {
  const state = useConnectionStatus();

  if (state === "connected") return null;

  const isReconnecting = state === "reconnecting";

  return (
    <div className="flex items-center gap-2 text-xs text-muted-foreground">
      <span
        className={`inline-block h-2 w-2 rounded-full shrink-0 ${isReconnecting ? "bg-[#e0af68] animate-pulse" : "bg-[#f7768e]"}`}
      />
      {isReconnecting ? "Reconnecting..." : "Disconnected"}
    </div>
  );
}
