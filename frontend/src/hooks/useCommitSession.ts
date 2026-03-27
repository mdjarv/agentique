import { useCallback, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { commitSession, refreshGitStatus } from "~/lib/session-actions";
import { getErrorMessage } from "~/lib/utils";

export function useCommitSession(sessionId: string) {
  const ws = useWebSocket();
  const [committing, setCommitting] = useState(false);

  const handleCommit = useCallback(
    async (message: string) => {
      setCommitting(true);
      try {
        const result = await commitSession(ws, sessionId, message);
        toast.success(`Committed (${result.commitHash.slice(0, 7)})`);
        refreshGitStatus(ws, sessionId).catch(() => {});
        return true;
      } catch (err) {
        toast.error(getErrorMessage(err, "Commit failed"));
        return false;
      } finally {
        setCommitting(false);
      }
    },
    [ws, sessionId],
  );

  return { committing, handleCommit };
}
