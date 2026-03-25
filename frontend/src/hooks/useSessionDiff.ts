import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { type DiffResult, getSessionDiff } from "~/lib/session-actions";
import { useChatStore } from "~/stores/chat-store";

export function useSessionDiff(sessionId: string) {
  const ws = useWebSocket();
  const isRunning = useChatStore((s) => s.sessions[sessionId]?.meta.state === "running");

  const [diffResult, setDiffResult] = useState<DiffResult | null>(null);
  const [showDiff, setShowDiff] = useState(false);
  const [loadingDiff, setLoadingDiff] = useState(false);

  const fetchDiff = useCallback(async () => {
    setLoadingDiff(true);
    try {
      const result = await getSessionDiff(ws, sessionId);
      setDiffResult(result);
      return result;
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load diff");
      return null;
    } finally {
      setLoadingDiff(false);
    }
  }, [ws, sessionId]);

  useEffect(() => {
    if (!isRunning) fetchDiff();
  }, [isRunning, fetchDiff]);

  const toggleDiff = useCallback(async () => {
    if (showDiff) {
      setShowDiff(false);
      return;
    }
    const result = diffResult ?? (await fetchDiff());
    if (result) setShowDiff(true);
  }, [showDiff, diffResult, fetchDiff]);

  const diffTotals = diffResult?.files.reduce<{ add: number; del: number }>(
    (acc, f) => ({ add: acc.add + f.insertions, del: acc.del + f.deletions }),
    { add: 0, del: 0 },
  );

  return { diffResult, showDiff, loadingDiff, fetchDiff, toggleDiff, diffTotals };
}
