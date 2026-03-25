import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import {
  type DiffResult,
  cleanSession,
  commitSession,
  createPR,
  getSessionDiff,
  mergeSession,
  rebaseSession,
  refreshGitStatus,
} from "~/lib/session-actions";
import { useChatStore } from "~/stores/chat-store";

export function useGitActions(sessionId: string) {
  const ws = useWebSocket();
  const meta = useChatStore((s) => s.sessions[sessionId]?.meta);
  const isRunning = meta?.state === "running";

  // Diff
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

  // Merge
  const [merging, setMerging] = useState(false);
  const [conflictFiles, setConflictFiles] = useState<string[] | null>(null);

  const handleMerge = useCallback(
    async (cleanup: boolean) => {
      setMerging(true);
      try {
        const result = await mergeSession(ws, sessionId, cleanup);
        if (result.status === "merged") {
          toast.success(`Merged (${result.commitHash?.slice(0, 7)})`);
        } else if (result.status === "conflict") {
          setConflictFiles(result.conflictFiles ?? []);
        } else {
          toast.error(result.error ?? "Merge failed");
        }
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Merge failed");
      } finally {
        setMerging(false);
      }
    },
    [ws, sessionId],
  );

  // Rebase
  const [rebasing, setRebasing] = useState(false);

  const handleRebase = useCallback(async () => {
    setRebasing(true);
    try {
      const result = await rebaseSession(ws, sessionId);
      if (result.status === "rebased") {
        toast.success("Rebased onto main");
      } else if (result.status === "conflict") {
        setConflictFiles(result.conflictFiles ?? []);
      } else {
        toast.error(result.error ?? "Rebase failed");
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Rebase failed");
    } finally {
      setRebasing(false);
    }
  }, [ws, sessionId]);

  // Commit
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

  // PR
  const [creatingPR, setCreatingPR] = useState(false);

  const handlePRSubmit = useCallback(
    async (title: string, body: string) => {
      setCreatingPR(true);
      try {
        const result = await createPR(ws, sessionId, title, body);
        if (result.status === "created" || result.status === "existing") {
          toast.success(`PR ${result.status}: ${result.url}`);
          return true;
        }
        toast.error(result.error ?? "PR creation failed");
        return false;
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "PR creation failed");
        return false;
      } finally {
        setCreatingPR(false);
      }
    },
    [ws, sessionId],
  );

  // Refresh git status
  const [refreshingGit, setRefreshingGit] = useState(false);

  const handleRefreshGit = useCallback(async () => {
    setRefreshingGit(true);
    try {
      await refreshGitStatus(ws, sessionId);
      await fetchDiff();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Refresh failed");
    } finally {
      setRefreshingGit(false);
    }
  }, [ws, sessionId, fetchDiff]);

  // Clean
  const [cleaning, setCleaning] = useState(false);

  const handleClean = useCallback(async () => {
    setCleaning(true);
    try {
      const r = await cleanSession(ws, sessionId);
      if (r.status === "cleaned") {
        toast.success("Cleaned");
      } else {
        toast.error(r.error ?? "Clean failed");
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Clean failed");
    } finally {
      setCleaning(false);
    }
  }, [ws, sessionId]);

  return {
    // Diff
    diffResult,
    showDiff,
    loadingDiff,
    fetchDiff,
    toggleDiff,
    diffTotals,
    // Merge
    merging,
    handleMerge,
    conflictFiles,
    setConflictFiles,
    // Rebase
    rebasing,
    handleRebase,
    // Commit
    committing,
    handleCommit,
    // PR
    creatingPR,
    handlePRSubmit,
    // Refresh
    refreshingGit,
    handleRefreshGit,
    // Clean
    cleaning,
    handleClean,
  };
}
