import { useCleanSession } from "~/hooks/session/useCleanSession";
import { useSessionDiff } from "~/hooks/session/useSessionDiff";
import { useCreatePR } from "~/hooks/useCreatePR";
import { useCommitLog } from "./useCommitLog";
import { useCommitSession } from "./useCommitSession";
import { useMergeSession } from "./useMergeSession";
import { useRebaseSession } from "./useRebaseSession";
import { useRefreshGit } from "./useRefreshGit";
import { useUncommittedDiff } from "./useUncommittedDiff";
import { useUncommittedFiles } from "./useUncommittedFiles";

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
  const commitLog = useCommitLog(sessionId);

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
    ...commitLog,
  };
}
