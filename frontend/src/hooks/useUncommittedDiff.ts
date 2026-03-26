import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { type DiffResult, getUncommittedDiff } from "~/lib/session-actions";
import { useChatStore } from "~/stores/chat-store";

export function useUncommittedDiff(sessionId: string) {
  const ws = useWebSocket();
  const meta = useChatStore((s) => s.sessions[sessionId]?.meta);
  const isRunning = meta?.state === "running";
  const isDirty = meta?.hasUncommitted || meta?.hasDirtyWorktree;

  const [uncommittedDiffResult, setUncommittedDiffResult] = useState<DiffResult | null>(null);
  const [loadingUncommittedDiff, setLoadingUncommittedDiff] = useState(false);

  // Reset on session switch.
  const prevSessionId = useRef(sessionId);
  const wasRunning = useRef(isRunning);
  if (prevSessionId.current !== sessionId) {
    prevSessionId.current = sessionId;
    wasRunning.current = isRunning;
    setUncommittedDiffResult(null);
    setLoadingUncommittedDiff(false);
  }

  const fetchUncommittedDiff = useCallback(async () => {
    setLoadingUncommittedDiff(true);
    try {
      const result = await getUncommittedDiff(ws, sessionId);
      setUncommittedDiffResult(result);
      return result;
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load uncommitted diff");
      return null;
    } finally {
      setLoadingUncommittedDiff(false);
    }
  }, [ws, sessionId]);

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

  const uncommittedDiffTotals = uncommittedDiffResult?.files.reduce<{ add: number; del: number }>(
    (acc, f) => ({ add: acc.add + f.insertions, del: acc.del + f.deletions }),
    { add: 0, del: 0 },
  );

  return {
    uncommittedDiffResult,
    uncommittedDiffTotals,
    loadingUncommittedDiff,
    fetchUncommittedDiff,
  };
}
