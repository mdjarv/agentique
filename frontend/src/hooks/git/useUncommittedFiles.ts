import { useCallback, useEffect, useRef, useState } from "react";
import { useWebSocket } from "~/hooks/useWebSocket";
import { type FileStatus, getUncommittedFiles } from "~/lib/session/actions";
import { useChatStore } from "~/stores/chat-store";

export function useUncommittedFiles(sessionId: string) {
  const ws = useWebSocket();
  const meta = useChatStore((s) => s.sessions[sessionId]?.meta);
  const isDirty = meta?.hasUncommitted || meta?.hasDirtyWorktree;
  const isRunning = meta?.state === "running";
  const gitVersion = meta?.gitVersion ?? 0;

  const [files, setFiles] = useState<FileStatus[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [expanded, setExpanded] = useState(false);

  const prevVersion = useRef(gitVersion);

  const prevSessionId = useRef(sessionId);
  if (prevSessionId.current !== sessionId) {
    prevSessionId.current = sessionId;
    prevVersion.current = gitVersion;
    setFiles(null);
    setLoading(false);
    setExpanded(false);
  }

  const fetchFiles = useCallback(async () => {
    setLoading(true);
    try {
      const result = await getUncommittedFiles(ws, sessionId);
      if (prevSessionId.current !== sessionId) return;
      setFiles(result.files);
    } catch {
      if (prevSessionId.current === sessionId) setFiles(null);
    } finally {
      if (prevSessionId.current === sessionId) setLoading(false);
    }
  }, [ws, sessionId]);

  // Auto-fetch when session stops running and is dirty.
  useEffect(() => {
    if (!isRunning && isDirty) {
      fetchFiles();
    } else if (!isDirty) {
      setFiles(null);
      setExpanded(false);
    }
  }, [isRunning, isDirty, fetchFiles]);

  // Re-fetch when gitVersion changes (covers commit, merge, rebase, clean).
  useEffect(() => {
    if (gitVersion !== prevVersion.current && !isRunning) {
      prevVersion.current = gitVersion;
      if (!isDirty) {
        setFiles(null);
        setExpanded(false);
      } else {
        fetchFiles();
      }
    }
  }, [gitVersion, isRunning, isDirty, fetchFiles]);

  const toggleExpanded = useCallback(() => {
    setExpanded((prev) => !prev);
  }, []);

  return {
    uncommittedFiles: files,
    uncommittedLoading: loading,
    uncommittedExpanded: expanded,
    toggleUncommittedExpanded: toggleExpanded,
    fetchUncommittedFiles: fetchFiles,
  };
}
