import { useMemo } from "react";
import { useGitResource } from "~/hooks/git/useGitResource";
import { type DiffResult, getUncommittedDiff } from "~/lib/session/actions";
import { useChatStore } from "~/stores/chat-store";

export function useUncommittedDiff(sessionId: string) {
  const isDirty = useChatStore((s) => {
    const meta = s.sessions[sessionId]?.meta;
    return Boolean(meta?.hasUncommitted || meta?.hasDirtyWorktree);
  });

  const { data, loading, refetch } = useGitResource<DiffResult>({
    sessionId,
    fetch: getUncommittedDiff,
    enabled: isDirty,
    fetchOnIdle: true,
    errorMessage: "Failed to load uncommitted diff",
  });

  const uncommittedDiffTotals = useMemo(
    () =>
      data?.files.reduce<{ add: number; del: number }>(
        (acc, f) => ({ add: acc.add + f.insertions, del: acc.del + f.deletions }),
        { add: 0, del: 0 },
      ),
    [data],
  );

  return {
    uncommittedDiffResult: data,
    uncommittedDiffTotals,
    loadingUncommittedDiff: loading,
    fetchUncommittedDiff: refetch,
  };
}
