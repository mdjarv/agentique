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
      } else if (job.kind === "all" && job.phase === "done") {
        const n = job.changes ?? 0;
        toast.success(`Tidied all scopes — ${n} change${n === 1 ? "" : "s"}`);
      }
    });
    const unsubUpdated = ws.subscribe("brain.updated", () => {
      useBrainStore.getState().onBrainUpdated();
    });
    // On (re)connect, resync the consolidation job. If the backend restarted mid
    // preview the job is gone, so this returns null and clears a stale "Analyzing…"
    // spinner instead of leaving it hung forever.
    const unsubConnect = ws.onConnect(() => {
      useBrainStore.getState().hydrateJob();
    });
    return () => {
      unsubJob();
      unsubUpdated();
      unsubConnect();
    };
  }, [ws]);
}
