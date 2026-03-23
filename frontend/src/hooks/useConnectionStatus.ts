import { useSyncExternalStore } from "react";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { ConnectionState } from "~/lib/ws-client";

export function useConnectionStatus(): ConnectionState {
  const ws = useWebSocket();
  return useSyncExternalStore(
    (cb) => ws.onConnectionStateChange(cb),
    () => ws.connectionState,
  );
}
