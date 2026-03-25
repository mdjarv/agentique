import { useCallback, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { commitSession } from "~/lib/session-actions";

export function useCommitSession(sessionId: string) {
  const ws = useWebSocket();
  const [committing, setCommitting] = useState(false);

  const handleCommit = useCallback(
    async (message: string) => {
      setCommitting(true);
      try {
        const result = await commitSession(ws, sessionId, message);
        toast.success(`Committed (${result.commitHash.slice(0, 7)})`);
        return true;
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Commit failed");
        return false;
      } finally {
        setCommitting(false);
      }
    },
    [ws, sessionId],
  );

  return { committing, handleCommit };
}
