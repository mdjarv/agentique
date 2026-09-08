import { useMemo } from "react";
import { useGitResource } from "~/hooks/git/useGitResource";
import { type DiffResult, getSessionDiff } from "~/lib/session/actions";
import { useChatStore } from "~/stores/chat-store";

/**
 * `ready` holds the eager fetch back until the caller says the transcript
 * has painted: the diff is a git subprocess, and on a session's opening it
 * competed with the history request for the same cores. False means "not
 * yet", never "never" — the fetch fires the moment it flips.
 */
export function useSessionDiff(sessionId: string, ready = true) {
  const isMerged = useChatStore((s) => s.sessions[sessionId]?.meta?.worktreeMerged ?? false);

  const { data, loading, refetch } = useGitResource<DiffResult>({
    sessionId,
    fetch: getSessionDiff,
    enabled: !isMerged && ready,
    fetchOnIdle: true,
    errorMessage: "Failed to load diff",
  });

  const diffTotals = useMemo(
    () =>
      data?.files.reduce<{ add: number; del: number }>(
        (acc, f) => ({ add: acc.add + f.insertions, del: acc.del + f.deletions }),
        { add: 0, del: 0 },
      ),
    [data],
  );

  return { diffResult: data, loadingDiff: loading, fetchDiff: refetch, diffTotals };
}
