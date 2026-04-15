import { useCallback, useRef, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import {
  type MergeMode,
  mergeSession,
  rebaseSession,
  refreshGitStatus,
} from "~/lib/session/actions";
import { getErrorMessage } from "~/lib/utils";

export function useMergeSession(sessionId: string) {
  const ws = useWebSocket();
  const [merging, setMerging] = useState(false);
  const pendingModeRef = useRef<MergeMode>("complete");

  const doRebaseThenMerge = useCallback(async () => {
    setMerging(true);
    try {
      const rebaseResult = await rebaseSession(ws, sessionId);
      if (rebaseResult.status !== "rebased") {
        const msg =
          rebaseResult.status === "conflict"
            ? "Rebase conflicts detected"
            : (rebaseResult.error ?? "Rebase failed");
        toast.error(msg);
        return;
      }
      const mergeResult = await mergeSession(ws, sessionId, pendingModeRef.current);
      if (mergeResult.status === "merged") {
        toast.success(`Rebased and merged (${mergeResult.commitHash.slice(0, 7)})`);
      } else {
        const msg =
          "error" in mergeResult ? mergeResult.error : `Merge failed: ${mergeResult.status}`;
        toast.error(msg);
      }
    } catch (err) {
      toast.error(getErrorMessage(err, "Rebase + merge failed"));
    } finally {
      setMerging(false);
      refreshGitStatus(ws, sessionId).catch(() => {});
    }
  }, [ws, sessionId]);

  const handleMerge = useCallback(
    async (mode: MergeMode) => {
      setMerging(true);
      try {
        const result = await mergeSession(ws, sessionId, mode);
        switch (result.status) {
          case "merged":
            toast.success(`Merged (${result.commitHash.slice(0, 7)})`);
            break;
          case "needs_rebase":
            pendingModeRef.current = mode;
            toast.warning("Branches have diverged — rebase needed before merge", {
              duration: 10000,
              action: { label: "Rebase & Merge", onClick: doRebaseThenMerge },
            });
            break;
          case "conflict":
            toast.error("Merge conflicts detected");
            break;
          case "dirty_worktree":
            toast.warning("Project has uncommitted changes — commit or stash them before merging");
            break;
          case "error":
            toast.error(result.error);
            break;
        }
        refreshGitStatus(ws, sessionId).catch((err) =>
          console.error("refreshGitStatus failed", err),
        );
      } catch (err) {
        toast.error(getErrorMessage(err, "Merge failed"));
      } finally {
        setMerging(false);
      }
    },
    [ws, sessionId, doRebaseThenMerge],
  );

  return { merging, handleMerge };
}
