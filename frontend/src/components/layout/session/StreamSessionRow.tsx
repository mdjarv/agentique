import { useDraggable } from "@dnd-kit/core";

import { memo, useCallback, useState } from "react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "~/components/ui/context-menu";
import { useWebSocket } from "~/hooks/useWebSocket";
import {
  deleteSession,
  interruptSession,
  markSessionDone,
  renameSession,
} from "~/lib/session/actions";
import { getErrorMessage, relativeTime } from "~/lib/utils";
import { useChatStore } from "~/stores/chat-store";
import { useUIStore } from "~/stores/ui-store";

import { RenameDialog } from "./RenameDialog";
import { SessionHoverCard } from "./SessionHoverCard";
import { SessionRow } from "./SessionRow";

interface StreamSessionRowProps {
  sessionId: string;
  projectSlug: string;
  activeSessionId: string | undefined;
  isDragActive: boolean;
  onSessionClick: (sessionId: string) => void;
  hideProjectPill?: boolean;
}

export const StreamSessionRow = memo(function StreamSessionRow({
  sessionId,
  projectSlug,
  activeSessionId,
  isDragActive,
  onSessionClick,
  hideProjectPill,
}: StreamSessionRowProps) {
  const ws = useWebSocket();
  const meta = useChatStore((s) => s.sessions[sessionId]?.meta);
  const hasUnseenCompletion = useChatStore(
    (s) => s.sessions[sessionId]?.hasUnseenCompletion ?? false,
  );
  const hasPendingInput = useChatStore(
    (s) => !!(s.sessions[sessionId]?.pendingApproval || s.sessions[sessionId]?.pendingQuestion),
  );
  const isPlanning = useChatStore((s) => !!s.sessions[sessionId]?.planMode);
  const todoDone = useChatStore(
    (s) => s.sessions[sessionId]?.todos?.filter((t) => t.status === "completed").length ?? 0,
  );
  const todoTotal = useChatStore((s) => s.sessions[sessionId]?.todos?.length ?? 0);
  const hasDraft = useUIStore((s) => !!s.drafts[sessionId]);

  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({
    id: sessionId,
    data: { sessionId, channelId: meta?.channelIds?.[0] },
  });

  const dragStyle = transform
    ? { transform: `translate(${transform.x}px, ${transform.y}px)` }
    : undefined;

  const handleClick = useCallback(() => onSessionClick(sessionId), [onSessionClick, sessionId]);

  // Context menu state
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [renameOpen, setRenameOpen] = useState(false);

  const handleDelete = useCallback(async () => {
    try {
      await deleteSession(ws, sessionId);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to delete"));
    } finally {
      setDeleteOpen(false);
    }
  }, [ws, sessionId]);

  const handleRename = useCallback(
    async (newName: string) => {
      try {
        await renameSession(ws, sessionId, newName);
        setRenameOpen(false);
      } catch (err) {
        toast.error(getErrorMessage(err, "Failed to rename"));
      }
    },
    [ws, sessionId],
  );

  const handleInterrupt = useCallback(async () => {
    try {
      await interruptSession(ws, sessionId);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to interrupt"));
    }
  }, [ws, sessionId]);

  const handleMarkDone = useCallback(async () => {
    try {
      await markSessionDone(ws, sessionId);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to mark done"));
    }
  }, [ws, sessionId]);

  if (!meta) return null;

  const time = meta.completedAt
    ? relativeTime(meta.completedAt)
    : meta.lastQueryAt
      ? relativeTime(meta.lastQueryAt)
      : meta.updatedAt
        ? relativeTime(meta.updatedAt)
        : "";

  const canInterrupt = meta.state === "running";
  const canMarkDone = meta.state === "idle";

  const row = (
    <div className="group/stream-row">
      <SessionRow
        ref={setNodeRef}
        name={meta.name}
        state={meta.state}
        connected={meta.connected}
        hasUnseenCompletion={hasUnseenCompletion}
        hasPendingApproval={hasPendingInput}
        isPlanning={isPlanning}
        isActive={sessionId === activeSessionId}
        hasDraft={hasDraft}
        worktreeMerged={meta.worktreeMerged}
        commitsAhead={meta.commitsAhead}
        gitOperation={meta.gitOperation}
        isDragging={isDragging}
        time={time}
        projectSlug={hideProjectPill ? undefined : projectSlug}
        agentProfileName={meta.agentProfileName}
        agentProfileAvatar={meta.agentProfileAvatar}
        todoDone={todoDone}
        todoTotal={todoTotal}
        onClick={handleClick}
        style={dragStyle}
        {...listeners}
        {...attributes}
      />
    </div>
  );

  const wrappedRow = isDragActive ? (
    row
  ) : (
    <SessionHoverCard sessionId={sessionId}>{row}</SessionHoverCard>
  );

  return (
    <>
      <ContextMenu>
        <ContextMenuTrigger asChild>{wrappedRow}</ContextMenuTrigger>
        <ContextMenuContent>
          {canInterrupt && <ContextMenuItem onClick={handleInterrupt}>Interrupt</ContextMenuItem>}
          {canMarkDone && <ContextMenuItem onClick={handleMarkDone}>Mark done</ContextMenuItem>}
          {(canInterrupt || canMarkDone) && <ContextMenuSeparator />}
          <ContextMenuItem onClick={() => setRenameOpen(true)}>Rename</ContextMenuItem>
          <ContextMenuItem onClick={() => setDeleteOpen(true)} className="text-destructive">
            Delete session
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete session</AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div>
                <p>This will remove the session and its data. This cannot be undone.</p>
                {meta.worktreePath && !meta.worktreeMerged && (
                  <p className="mt-2 font-medium text-warning">
                    This session has a worktree that will be removed.
                  </p>
                )}
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete}>Delete</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <RenameDialog
        open={renameOpen}
        onOpenChange={setRenameOpen}
        currentName={meta.name}
        onRename={handleRename}
      />
    </>
  );
});
