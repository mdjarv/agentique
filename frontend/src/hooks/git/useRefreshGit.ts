import { useCallback, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { refreshGitStatus } from "~/lib/session/actions";
import { getErrorMessage } from "~/lib/utils";

export function useRefreshGit(sessionId: string, fetchDiff: () => Promise<unknown>) {
  const ws = useWebSocket();
  const [refreshingGit, setRefreshingGit] = useState(false);

  const handleRefreshGit = useCallback(async () => {
    setRefreshingGit(true);
    try {
      await refreshGitStatus(ws, sessionId);
      await fetchDiff();
    } catch (err) {
      toast.error(getErrorMessage(err, "Refresh failed"));
    } finally {
      setRefreshingGit(false);
    }
  }, [ws, sessionId, fetchDiff]);

  return { refreshingGit, handleRefreshGit };
}
