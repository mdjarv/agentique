import { useCallback, useEffect, useRef, useState } from "react";
import { useWebSocket } from "~/hooks/useWebSocket";
import { type CommitLogEntry, getCommitLog } from "~/lib/session/actions";
import { useChatStore } from "~/stores/chat-store";

export function useCommitLog(sessionId: string) {
  const ws = useWebSocket();
  const meta = useChatStore((s) => s.sessions[sessionId]?.meta);
  const ahead = meta?.commitsAhead ?? 0;
  const isRunning = meta?.state === "running";
  const gitVersion = meta?.gitVersion ?? 0;

  const [commits, setCommits] = useState<CommitLogEntry[] | null>(null);
  const [loading, setLoading] = useState(false);

  const prevVersion = useRef(gitVersion);

  const prevSessionId = useRef(sessionId);
  if (prevSessionId.current !== sessionId) {
    prevSessionId.current = sessionId;
    prevVersion.current = gitVersion;
    setCommits(null);
    setLoading(false);
  }

  const fetchLog = useCallback(async () => {
    setLoading(true);
    try {
      const result = await getCommitLog(ws, sessionId);
      if (prevSessionId.current !== sessionId) return;
      setCommits(result.commits);
    } catch {
      if (prevSessionId.current === sessionId) setCommits(null);
    } finally {
      if (prevSessionId.current === sessionId) setLoading(false);
    }
  }, [ws, sessionId]);

  // Fetch eagerly when ahead > 0 and not running.
  const didInitialFetch = useRef(false);
  useEffect(() => {
    if (prevSessionId.current !== sessionId) didInitialFetch.current = false;
  }, [sessionId]);
  useEffect(() => {
    if (!didInitialFetch.current && ahead > 0 && !isRunning) {
      didInitialFetch.current = true;
      fetchLog();
    }
  }, [ahead, isRunning, fetchLog]);

  // Re-fetch when gitVersion changes.
  useEffect(() => {
    if (gitVersion !== prevVersion.current && !isRunning) {
      prevVersion.current = gitVersion;
      if (ahead === 0) {
        setCommits(null);
      } else {
        fetchLog();
      }
    }
  }, [gitVersion, isRunning, ahead, fetchLog]);

  return {
    commitLog: commits,
    commitLogLoading: loading,
  };
}
