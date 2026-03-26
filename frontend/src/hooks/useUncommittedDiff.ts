import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { type DiffResult, getUncommittedDiff } from "~/lib/session-actions";
import { useChatStore } from "~/stores/chat-store";

export function useUncommittedDiff(sessionId: string) {
  const ws = useWebSocket();
  const meta = useChatStore((s) => s.sessions[sessionId]?.meta);
  const isRunning = meta?.state === "running";
  const isDirty = meta?.hasUncommitted || meta?.hasDirtyWorktree;
  const gitVersion = meta?.gitVersion ?? 0;

  const [uncommittedDiffResult, setUncommittedDiffResult] = useState<DiffResult | null>(null);
  const [loadingUncommittedDiff, setLoadingUncommittedDiff] = useState(false);

  // Reset on session switch.
  const prevSessionId = useRef(sessionId);
  const wasRunning = useRef(isRunning);
  const prevVersion = useRef(gitVersion);
  if (prevSessionId.current !== sessionId) {
    prevSessionId.current = sessionId;
    wasRunning.current = isRunning;
    prevVersion.current = gitVersion;
    setUncommittedDiffResult(null);
    setLoadingUncommittedDiff(false);
  }

  const fetchUncommittedDiff = useCallback(async () => {
    if (!isDirty) {
      setUncommittedDiffResult(null);
      return null;
    }
    setLoadingUncommittedDiff(true);
    try {
      const result = await getUncommittedDiff(ws, sessionId);
      if (prevSessionId.current !== sessionId) return null;
      setUncommittedDiffResult(result);
      return result;
    } catch (err) {
      if (prevSessionId.current === sessionId) {
        toast.error(err instanceof Error ? err.message : "Failed to load uncommitted diff");
      }
      return null;
    } finally {
      if (prevSessionId.current === sessionId) {
        setLoadingUncommittedDiff(false);
      }
    }
  }, [ws, sessionId, isDirty]);

  // Fetch when session stops running and has dirty files.
  useEffect(() => {
    if (wasRunning.current && !isRunning && isDirty) {
      fetchUncommittedDiff();
    }
    if (!isDirty) {
      setUncommittedDiffResult(null);
    }
    wasRunning.current = isRunning;
  }, [isRunning, isDirty, fetchUncommittedDiff]);

  // Re-fetch when gitVersion changes (covers commit, merge, rebase, clean).
  useEffect(() => {
    if (gitVersion !== prevVersion.current && !isRunning) {
      prevVersion.current = gitVersion;
      if (!isDirty) {
        setUncommittedDiffResult(null);
      } else {
        fetchUncommittedDiff();
      }
    }
  }, [gitVersion, isRunning, isDirty, fetchUncommittedDiff]);

  const uncommittedDiffTotals = useMemo(
    () =>
      uncommittedDiffResult?.files.reduce<{ add: number; del: number }>(
        (acc, f) => ({ add: acc.add + f.insertions, del: acc.del + f.deletions }),
        { add: 0, del: 0 },
      ),
    [uncommittedDiffResult],
  );

  return {
    uncommittedDiffResult,
    uncommittedDiffTotals,
    loadingUncommittedDiff,
    fetchUncommittedDiff,
  };
}
