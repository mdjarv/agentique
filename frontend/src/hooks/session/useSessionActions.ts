import { useCallback, useState } from "react";
import { toast } from "sonner";
import {
  cleanSession,
  deleteSession,
  markSessionDone,
  renameSession,
  resetConversation,
  restartSession,
  setSessionIcon,
  stopSession,
} from "~/lib/session/actions";
import { getErrorMessage } from "~/lib/utils";
import type { WsClient } from "~/lib/ws-client";
import type { SessionMetadata } from "~/stores/chat-store";

/**
 * WS-backed action handlers for a single session, with toast wiring and the
 * small bits of loading state (rename/delete/clean) the header surfaces.
 *
 * Handlers that drive a dialog (rename, delete) resolve to `true` on success so
 * the caller can close the dialog; the hook owns the loading flag but not the
 * dialog open state.
 */
export function useSessionActions(ws: WsClient, meta: SessionMetadata) {
  const sessionId = meta.id;
  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [cleaning, setCleaning] = useState(false);

  /** Fire-and-forget rename used by the inline name editor (no spinner). */
  const rename = useCallback(
    (name: string) => {
      renameSession(ws, sessionId, name).catch((err) => {
        toast.error(getErrorMessage(err, "Rename failed"));
      });
    },
    [ws, sessionId],
  );

  const handleRename = useCallback(
    async (name: string): Promise<boolean> => {
      setRenaming(true);
      try {
        await renameSession(ws, sessionId, name);
        return true;
      } catch (err) {
        toast.error(getErrorMessage(err, "Rename failed"));
        return false;
      } finally {
        setRenaming(false);
      }
    },
    [ws, sessionId],
  );

  const handleDelete = useCallback(async (): Promise<boolean> => {
    setDeleting(true);
    try {
      await deleteSession(ws, sessionId);
      return true;
    } catch (err) {
      setDeleting(false);
      toast.error(getErrorMessage(err, "Delete failed"));
      return false;
    }
  }, [ws, sessionId]);

  const handleClean = useCallback(async () => {
    setCleaning(true);
    try {
      const r = await cleanSession(ws, sessionId);
      if (r.status === "cleaned") {
        toast.success("Cleaned");
      } else {
        toast.error(r.error ?? "Clean failed");
      }
    } catch (err) {
      toast.error(getErrorMessage(err, "Clean failed"));
    } finally {
      setCleaning(false);
    }
  }, [ws, sessionId]);

  const handleStop = useCallback(async () => {
    try {
      await stopSession(ws, sessionId);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to stop"));
    }
  }, [ws, sessionId]);

  const handleRestart = useCallback(async () => {
    try {
      await restartSession(ws, sessionId);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to restart"));
    }
  }, [ws, sessionId]);

  const handleResetConversation = useCallback(async () => {
    try {
      await resetConversation(ws, sessionId);
      toast.success("Conversation reset — next message starts fresh");
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to reset conversation"));
    }
  }, [ws, sessionId]);

  const handleIconChange = useCallback(
    (icon: string | undefined) => {
      setSessionIcon(ws, sessionId, icon).catch((err) => {
        toast.error(getErrorMessage(err, "Failed to set icon"));
      });
    },
    [ws, sessionId],
  );

  const handleMarkDone = useCallback(() => {
    markSessionDone(ws, sessionId).catch((err) => {
      toast.error(getErrorMessage(err, "Failed to mark done"));
    });
  }, [ws, sessionId]);

  return {
    renaming,
    deleting,
    cleaning,
    rename,
    handleRename,
    handleDelete,
    handleClean,
    handleStop,
    handleRestart,
    handleResetConversation,
    handleIconChange,
    handleMarkDone,
  };
}
