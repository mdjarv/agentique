import { ChevronDown, ChevronRight, Trash2 } from "lucide-react";
import { useState } from "react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "~/components/ui/alert-dialog";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "~/components/ui/context-menu";
import { useWebSocket } from "~/hooks/useWebSocket";
import { deleteSession, deleteSessionsBulk } from "~/lib/session/actions";
import { IconSlot } from "./IconSlot";
import { getTodoProgress, SessionContent } from "./SessionRow";
import { SidebarRow } from "./SidebarRow";
import type { SessionItem } from "./types";
import { SessionSidebarRow } from "./useActiveSessionId";

export function CompletedSessionsBlock({
  completed,
  onSessionClick,
  sessionLevel,
}: {
  completed: SessionItem[];
  onSessionClick: (id: string) => void;
  sessionLevel: number;
}) {
  const ws = useWebSocket();
  const [showCompleted, setShowCompleted] = useState(false);

  if (completed.length === 0) return null;

  const handleDeleteAll = () => {
    const ids = completed.map((s) => s.id);
    if (ids.length === 0) return;
    deleteSessionsBulk(ws, ids).catch(console.error);
    setShowCompleted(false);
  };

  return (
    <>
      <SidebarRow
        as="div"
        indent={sessionLevel}
        compact
        className="group/completed"
        onClick={() => setShowCompleted((v) => !v)}
      >
        <IconSlot>
          {showCompleted ? (
            <ChevronDown className="size-2.5 text-muted-foreground" />
          ) : (
            <ChevronRight className="size-2.5 text-muted-foreground" />
          )}
        </IconSlot>
        <span className="text-[10px] text-muted-foreground ml-1 flex-1">
          {completed.length} completed
        </span>
        {showCompleted && (
          <AlertDialog>
            <AlertDialogTrigger asChild>
              <button
                type="button"
                onClick={(e) => e.stopPropagation()}
                className="flex items-center gap-0.5 px-1 py-0.5 text-[10px] text-destructive/70 hover:text-destructive transition-colors cursor-pointer rounded hover:bg-destructive/10 opacity-0 group-hover/completed:opacity-100"
              >
                <Trash2 className="size-2.5" />
                Delete all
              </button>
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>Delete completed sessions?</AlertDialogTitle>
                <AlertDialogDescription>
                  This will permanently delete {completed.length} completed session
                  {completed.length !== 1 && "s"} from this project. This cannot be undone.
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>Cancel</AlertDialogCancel>
                <AlertDialogAction onClick={handleDeleteAll}>
                  Delete {completed.length} session{completed.length !== 1 && "s"}
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        )}
      </SidebarRow>

      {showCompleted &&
        completed.map(({ id, data }) => (
          <ContextMenu key={id}>
            <ContextMenuTrigger asChild>
              <SessionSidebarRow
                sessionId={id}
                indent={sessionLevel}
                onClick={() => onSessionClick(id)}
                todoProgress={getTodoProgress(data)}
              >
                <SessionContent data={data} />
              </SessionSidebarRow>
            </ContextMenuTrigger>
            <ContextMenuContent>
              <ContextMenuItem
                onClick={() => deleteSession(ws, id).catch(console.error)}
                className="text-destructive focus:text-destructive"
              >
                <Trash2 className="size-3.5" />
                <span>Delete session</span>
              </ContextMenuItem>
            </ContextMenuContent>
          </ContextMenu>
        ))}
    </>
  );
}
