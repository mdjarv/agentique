import { useCallback, useEffect, useState } from "react";
import { useWebSocket } from "~/hooks/useWebSocket";
import { type FileStatus, getUncommittedFiles } from "~/lib/session-actions";
import { useChatStore } from "~/stores/chat-store";

export function useUncommittedFiles(sessionId: string) {
  const ws = useWebSocket();
  const meta = useChatStore((s) => s.sessions[sessionId]?.meta);
  const isDirty = meta?.hasUncommitted || meta?.hasDirtyWorktree;
  const isRunning = meta?.state === "running";

  const [files, setFiles] = useState<FileStatus[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [expanded, setExpanded] = useState(false);

  const fetchFiles = useCallback(async () => {
    setLoading(true);
    try {
      const result = await getUncommittedFiles(ws, sessionId);
      setFiles(result.files);
    } catch {
      setFiles(null);
    } finally {
      setLoading(false);
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
