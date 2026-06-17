import { useEffect } from "react";
import { toast } from "sonner";
import type { useWebSocket } from "~/hooks/useWebSocket";
import type { ConsolidationJob } from "~/lib/brain-api";
import { useBrainStore } from "~/stores/brain-store";

// Subscribes to brain push events broadcast to every tab: consolidation job
// progress/result (so a long preview streams live and survives reopening) and
// memory-change notifications (which drive a list refresh + the nav "flare").
export function useBrainSubscriptions(ws: ReturnType<typeof useWebSocket>) {
  useEffect(() => {
    const unsubJob = ws.subscribe("brain.consolidation", (payload) => {
      const job = payload as ConsolidationJob;
      useBrainStore.getState().setJob(job);
      if (job.phase === "error") {
        toast.error(job.error || "Consolidation failed");
      }
    });
    const unsubUpdated = ws.subscribe("brain.updated", () => {
      useBrainStore.getState().onBrainUpdated();
    });
    return () => {
      unsubJob();
      unsubUpdated();
    };
  }, [ws]);
}
