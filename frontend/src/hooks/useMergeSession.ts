import { useCallback, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { type MergeMode, mergeSession, refreshGitStatus } from "~/lib/session-actions";
import { getErrorMessage } from "~/lib/utils";

export function useMergeSession(sessionId: string) {
  const ws = useWebSocket();
  const [merging, setMerging] = useState(false);

  const handleMerge = useCallback(
    async (mode: MergeMode) => {
      setMerging(true);
      try {
        const result = await mergeSession(ws, sessionId, mode);
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
        toast.error(getErrorMessage(err, "Merge failed"));
      } finally {
        setMerging(false);
      }
    },
    [ws, sessionId],
  );

  return { merging, handleMerge };
}
