import {
  Check,
  Copy,
  Eraser,
  Loader2,
  MoreHorizontal,
  PanelRightOpen,
  Pencil,
  Trash2,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { SessionStatusDot } from "~/components/layout/SessionStatusDot";
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
import { Button } from "~/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { useWebSocket } from "~/hooks/useWebSocket";
import { cleanSession, deleteSession, markSessionDone, renameSession } from "~/lib/session-actions";
import { cn, copyToClipboard } from "~/lib/utils";
import type { SessionData } from "~/stores/chat-store";

interface SessionHeaderProps {
  session: SessionData;
  showPanelButton?: boolean;
  onOpenPanel?: () => void;
}

export function SessionHeader({ session, showPanelButton, onOpenPanel }: SessionHeaderProps) {
  const { meta } = session;
  const ws = useWebSocket();
  const isRunning = meta.state === "running";
  const isWorktree = !!meta.worktreeBranch;
  const isBusy = isRunning;

  const [activeDialog, setActiveDialog] = useState<"none" | "delete">("none");
  const [deleting, setDeleting] = useState(false);
  const [cleaning, setCleaning] = useState(false);
  const [editing, setEditing] = useState(false);
  const [editName, setEditName] = useState(meta.name);
  const [nameCopied, setNameCopied] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (editing) {
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [editing]);

  useEffect(() => {
    if (!editing) setEditName(meta.name);
  }, [meta.name, editing]);

  const commitRename = () => {
    const trimmed = editName.trim();
    setEditing(false);
    if (trimmed && trimmed !== meta.name) {
      renameSession(ws, meta.id, trimmed).catch((err) => {
        toast.error(err instanceof Error ? err.message : "Rename failed");
      });
    } else {
      setEditName(meta.name);
    }
  };

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await deleteSession(ws, meta.id);
      setActiveDialog("none");
    } catch (err) {
      setDeleting(false);
      toast.error(err instanceof Error ? err.message : "Delete failed");
    }
  };

  const handleClean = useCallback(async () => {
    setCleaning(true);
    try {
      const r = await cleanSession(ws, meta.id);
      if (r.status === "cleaned") {
        toast.success("Cleaned");
      } else {
        toast.error(r.error ?? "Clean failed");
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Clean failed");
    } finally {
      setCleaning(false);
    }
  }, [ws, meta.id]);

  return (
    <>
      <div className="border-b px-4 py-2 flex items-center gap-2 text-sm shrink-0">
        <SessionStatusDot
          state={meta.state}
          connected={meta.connected}
          hasUnseenCompletion={session.hasUnseenCompletion}
          hasPendingApproval={!!session.pendingApproval || !!session.pendingQuestion}
        />

        {/* Editable name */}
        {editing ? (
          <input
            ref={inputRef}
            value={editName}
            onChange={(e) => setEditName(e.target.value)}
            onBlur={commitRename}
            onKeyDown={(e) => {
              if (e.key === "Enter") commitRename();
              if (e.key === "Escape") {
                setEditName(meta.name);
                setEditing(false);
              }
            }}
            className="font-medium truncate bg-transparent border-b border-border outline-none px-0 py-0 text-sm w-48"
          />
        ) : (
          <div className="group/name flex items-center gap-1 font-medium truncate">
            <button
              type="button"
              onClick={() => setEditing(true)}
              className="flex items-center gap-1 truncate hover:text-foreground"
            >
              <span className={cn("truncate", !meta.name && "italic text-muted-foreground")}>
                {meta.name || "Untitled"}
              </span>
              <Pencil className="h-3 w-3 max-md:opacity-50 opacity-0 group-hover/name:opacity-50 transition-opacity shrink-0" />
            </button>
            <button
              type="button"
              onClick={() => {
                copyToClipboard(meta.name || "Untitled").then(() => {
                  setNameCopied(true);
                  setTimeout(() => setNameCopied(false), 1500);
                });
              }}
              className="p-0.5 rounded max-md:opacity-50 opacity-0 group-hover/name:opacity-50 hover:!opacity-100 text-muted-foreground transition-opacity shrink-0"
              aria-label="Copy session name"
            >
              {nameCopied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
            </button>
          </div>
        )}

        <div className="ml-auto flex items-center gap-1.5">
          {/* Session panel toggle (mobile) */}
          {showPanelButton && (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 px-1.5 text-xs text-muted-foreground"
              title="Session panel"
              onClick={onOpenPanel}
            >
              <PanelRightOpen className="h-3.5 w-3.5" />
            </Button>
          )}

          {/* Mark done */}
          {(meta.state === "idle" || meta.state === "stopped" || meta.state === "failed") && (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 px-1.5 text-xs text-muted-foreground hover:text-[#9ece6a]"
              title="Mark done"
              onClick={() => {
                markSessionDone(ws, meta.id).catch((err) => {
                  toast.error(err instanceof Error ? err.message : "Failed to mark done");
                });
              }}
            >
              <Check className="h-3.5 w-3.5" />
            </Button>
          )}

          {/* Overflow menu — clean + delete */}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                className="h-7 px-1.5 text-xs text-muted-foreground"
              >
                <MoreHorizontal className="h-3.5 w-3.5" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              {isWorktree && !isBusy && (
                <DropdownMenuItem
                  onClick={handleClean}
                  disabled={cleaning}
                  className="text-xs gap-2"
                >
                  {cleaning ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Eraser className="h-3.5 w-3.5" />
                  )}
                  Clean up worktree
                </DropdownMenuItem>
              )}
              <DropdownMenuItem
                onClick={() => setActiveDialog("delete")}
                className="text-xs gap-2 text-destructive focus:text-destructive"
              >
                <Trash2 className="h-3.5 w-3.5" />
                Delete session
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>

          {/* State label */}
          <span className="text-xs text-muted-foreground capitalize">{meta.state}</span>
        </div>
      </div>

      {/* Delete confirmation */}
      <AlertDialog
        open={activeDialog === "delete"}
        onOpenChange={(open) => setActiveDialog(open ? "delete" : "none")}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete session</AlertDialogTitle>
            <AlertDialogDescription>
              Delete &quot;{meta.name || "Untitled"}&quot;? This removes the worktree, branch, and
              all session data.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} disabled={deleting}>
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
