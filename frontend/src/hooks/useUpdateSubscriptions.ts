import { useEffect } from "react";
import type { useWebSocket } from "~/hooks/useWebSocket";
import { PRIMARY_MACHINE_KEY } from "~/lib/update-api";
import { useFeatureStore } from "~/stores/feature-store";
import { useUpdateStore } from "~/stores/update-store";

/**
 * Upgrade progress, from every machine (docs/upgrades.md).
 *
 * The routing facade fans subscriptions in from every machine's socket, so a
 * payload arriving here could be about any of them — which is why progress
 * carries `machineId`. The primary's own id lives in feature-store, and maps
 * back to the key the update store uses for it.
 */
export function useUpdateSubscriptions(ws: ReturnType<typeof useWebSocket>) {
  useEffect(() => {
    return ws.subscribe("update.progress", (payload) => {
      const primaryId = useFeatureStore.getState().machineId;
      const key = payload.machineId === primaryId ? PRIMARY_MACHINE_KEY : payload.machineId;
      if (!key) return;
      useUpdateStore.getState().applyProgress(key, payload);
    });
  }, [ws]);
}
