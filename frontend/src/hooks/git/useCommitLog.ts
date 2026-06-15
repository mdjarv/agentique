import { useGitResource } from "~/hooks/git/useGitResource";
import { type CommitLogEntry, getCommitLog } from "~/lib/session/actions";
import type { WsClient } from "~/lib/ws-client";
import { useChatStore } from "~/stores/chat-store";

// Module-level so the reference is stable across renders (useGitResource keys
// its memoized refetch on the fetch identity).
const fetchCommits = async (ws: WsClient, sessionId: string): Promise<CommitLogEntry[]> =>
  (await getCommitLog(ws, sessionId)).commits;

export function useCommitLog(sessionId: string) {
  const ahead = useChatStore((s) => s.sessions[sessionId]?.meta?.commitsAhead ?? 0);

  const { data, loading } = useGitResource<CommitLogEntry[]>({
    sessionId,
    fetch: fetchCommits,
    enabled: ahead > 0,
  });

  return { commitLog: data, commitLogLoading: loading };
}
