import { useCallback, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { rebaseSession, refreshGitStatus } from "~/lib/session-actions";
import { getErrorMessage } from "~/lib/utils";

export function useRebaseSession(sessionId: string) {
  const ws = useWebSocket();
  const [rebasing, setRebasing] = useState(false);

  const handleRebase = useCallback(async () => {
    setRebasing(true);
    try {
      const result = await rebaseSession(ws, sessionId);
      switch (result.status) {
        case "rebased":
          toast.success("Rebased onto main");
          break;
        case "conflict":
          toast.error("Rebase conflicts detected");
          break;
        case "error":
          toast.error(result.error);
          break;
      }
      // Sync state from response in case push event was lost
      refreshGitStatus(ws, sessionId).catch(() => {});
    } catch (err) {
      toast.error(getErrorMessage(err, "Rebase failed"));
    } finally {
      setRebasing(false);
    }
  }, [ws, sessionId]);

  return { rebasing, handleRebase };
}
