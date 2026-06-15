import { useMemo } from "react";
import { useGitResource } from "~/hooks/git/useGitResource";
import { type DiffResult, getSessionDiff } from "~/lib/session/actions";
import { useChatStore } from "~/stores/chat-store";

export function useSessionDiff(sessionId: string) {
  const isMerged = useChatStore((s) => s.sessions[sessionId]?.meta?.worktreeMerged ?? false);

  const { data, loading, refetch } = useGitResource<DiffResult>({
    sessionId,
    fetch: getSessionDiff,
    enabled: !isMerged,
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
