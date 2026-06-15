import { useCallback, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { dissolveChannel } from "~/lib/channel-actions";
import { deleteSession } from "~/lib/session/actions";
import { getErrorMessage } from "~/lib/utils";

interface DeleteOpts {
  /** Session name, for the success toast. */
  name: string;
  /** Number of nested descendants deleted alongside it. */
  descendantCount: number;
}

interface DissolveOpts {
  /** Lead session name, for the success toast. */
  name: string;
  /** Channels this session leads — each is dissolved in turn. */
  channelIds: string[];
}

/**
 * Delete + dissolve teardown for a single session in the hierarchy view.
 * Owns the confirm-dialog open flags and in-flight pending state; the dialogs
 * themselves are rendered by the caller. Display-only context (name, counts,
 * lead channel ids) is passed to the run functions so the hook stays keyed by
 * sessionId alone.
 */
export function useSessionTeardown(sessionId: string) {
  const ws = useWebSocket();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [dissolveOpen, setDissolveOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [dissolving, setDissolving] = useState(false);

  const runDelete = useCallback(
    async ({ name, descendantCount }: DeleteOpts) => {
      setDeleting(true);
      try {
        await deleteSession(ws, sessionId);
        toast.success(
          descendantCount > 0
            ? `Deleted ${name} and ${descendantCount} descendant(s)`
            : `Deleted ${name}`,
        );
        setConfirmOpen(false);
      } catch (err) {
        toast.error(getErrorMessage(err, "Delete failed"));
      } finally {
        setDeleting(false);
      }
    },
    [ws, sessionId],
  );

  const runDissolve = useCallback(
    async ({ name, channelIds }: DissolveOpts) => {
      setDissolving(true);
      try {
        // Dissolve every channel this session leads. Typically there's only
        // one (the worker channel), but we don't want to silently ignore
        // additional lead memberships.
        for (const chId of channelIds) {
          await dissolveChannel(ws, chId);
        }
        toast.success(
          channelIds.length === 1
            ? `Dissolved ${name}'s channel`
            : `Dissolved ${channelIds.length} channels led by ${name}`,
        );
        setDissolveOpen(false);
      } catch (err) {
        toast.error(getErrorMessage(err, "Dissolve failed"));
      } finally {
        setDissolving(false);
      }
    },
    [ws],
  );

  return {
    confirmOpen,
    setConfirmOpen,
    dissolveOpen,
    setDissolveOpen,
    deleting,
    dissolving,
    runDelete,
    runDissolve,
  };
}
