import { useCallback, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { mergeSession, refreshGitStatus } from "~/lib/session-actions";

export function useMergeSession(sessionId: string) {
  const ws = useWebSocket();
  const [merging, setMerging] = useState(false);

  const handleMerge = useCallback(
    async (cleanup: boolean) => {
      setMerging(true);
      try {
        const result = await mergeSession(ws, sessionId, cleanup);
        switch (result.status) {
          case "merged":
            toast.success(`Merged (${result.commitHash.slice(0, 7)})`);
            break;
          case "conflict":
            toast.error("Merge conflicts detected");
            break;
          case "error":
            toast.error(result.error);
            break;
        }
        // Sync state from response in case push event was lost
        refreshGitStatus(ws, sessionId).catch(() => {});
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Merge failed");
      } finally {
        setMerging(false);
      }
    },
    [ws, sessionId],
  );

  return { merging, handleMerge };
}
