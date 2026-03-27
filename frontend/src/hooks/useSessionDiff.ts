import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { type DiffResult, getSessionDiff } from "~/lib/session-actions";
import { getErrorMessage } from "~/lib/utils";
import { useChatStore } from "~/stores/chat-store";

export function useSessionDiff(sessionId: string) {
  const ws = useWebSocket();
  const meta = useChatStore((s) => s.sessions[sessionId]?.meta);
  const isRunning = meta?.state === "running";
  const gitVersion = meta?.gitVersion ?? 0;
  const isMerged = meta?.worktreeMerged ?? false;

  const [diffResult, setDiffResult] = useState<DiffResult | null>(null);
  const [loadingDiff, setLoadingDiff] = useState(false);

  // Reset state on session switch
  const prevSessionId = useRef(sessionId);
  const wasRunning = useRef(isRunning);
  const prevVersion = useRef(gitVersion);
  if (prevSessionId.current !== sessionId) {
    prevSessionId.current = sessionId;
    wasRunning.current = isRunning;
    prevVersion.current = gitVersion;
    setDiffResult(null);
    setLoadingDiff(false);
  }

  const fetchDiff = useCallback(async () => {
    if (isMerged) {
      setDiffResult(null);
      return null;
    }
    setLoadingDiff(true);
    try {
      const result = await getSessionDiff(ws, sessionId);
      if (prevSessionId.current !== sessionId) return null;
      setDiffResult(result);
      return result;
    } catch (err) {
      if (prevSessionId.current === sessionId) {
        toast.error(getErrorMessage(err, "Failed to load diff"));
      }
      return null;
    } finally {
      if (prevSessionId.current === sessionId) {
        setLoadingDiff(false);
      }
    }
  }, [ws, sessionId, isMerged]);

  // Fetch diff when session transitions from running to idle (not on every mount)
  useEffect(() => {
    if (wasRunning.current && !isRunning) {
      fetchDiff();
    }
    wasRunning.current = isRunning;
  }, [isRunning, fetchDiff]);

  // Re-fetch when gitVersion changes (covers commit, merge, rebase, clean).
  useEffect(() => {
    if (gitVersion !== prevVersion.current && !isRunning) {
      prevVersion.current = gitVersion;
      if (isMerged) {
        setDiffResult(null);
      } else {
        fetchDiff();
      }
    }
  }, [gitVersion, isRunning, isMerged, fetchDiff]);

  const diffTotals = useMemo(
    () =>
      diffResult?.files.reduce<{ add: number; del: number }>(
        (acc, f) => ({ add: acc.add + f.insertions, del: acc.del + f.deletions }),
        { add: 0, del: 0 },
      ),
    [diffResult],
  );

  return { diffResult, loadingDiff, fetchDiff, diffTotals };
}
