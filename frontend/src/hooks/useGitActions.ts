import { useCleanSession } from "~/hooks/useCleanSession";
import { useCommitSession } from "~/hooks/useCommitSession";
import { useCreatePR } from "~/hooks/useCreatePR";
import { useMergeSession } from "~/hooks/useMergeSession";
import { useRebaseSession } from "~/hooks/useRebaseSession";
import { useRefreshGit } from "~/hooks/useRefreshGit";
import { useSessionDiff } from "~/hooks/useSessionDiff";
import { useUncommittedDiff } from "~/hooks/useUncommittedDiff";
import { useUncommittedFiles } from "~/hooks/useUncommittedFiles";

export function useGitActions(sessionId: string) {
  const diff = useSessionDiff(sessionId);
  const merge = useMergeSession(sessionId);
  const rebase = useRebaseSession(sessionId);
  const commit = useCommitSession(sessionId);
  const pr = useCreatePR(sessionId);
  const refresh = useRefreshGit(sessionId, diff.fetchDiff);
  const clean = useCleanSession(sessionId);
  const uncommitted = useUncommittedFiles(sessionId);
  const uncommittedDiff = useUncommittedDiff(sessionId);

  return {
    ...diff,
    ...merge,
    ...rebase,
    ...commit,
    ...pr,
    ...refresh,
    ...clean,
    ...uncommitted,
    ...uncommittedDiff,
  };
}
