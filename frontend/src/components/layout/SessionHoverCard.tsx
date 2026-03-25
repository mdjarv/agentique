import {
  ArrowDown,
  ArrowUp,
  CircleCheck,
  ExternalLink,
  GitBranch,
  GitMerge,
  GitPullRequest,
  Pause,
  Pencil,
  RefreshCw,
  Trash2,
} from "lucide-react";
import { type ReactNode, useState } from "react";
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
import { Button } from "~/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog";
import {
  HoverCard,
  HoverCardArrow,
  HoverCardContent,
  HoverCardTrigger,
} from "~/components/ui/hover-card";
import { Input } from "~/components/ui/input";
import { Separator } from "~/components/ui/separator";
import { useWebSocket } from "~/hooks/useWebSocket";
import {
  createPR,
  deleteSession,
  interruptSession,
  markSessionDone,
  mergeSession,
  rebaseSession,
  renameSession,
} from "~/lib/session-actions";
import { cn } from "~/lib/utils";
import { useChatStore } from "~/stores/chat-store";

interface SessionHoverCardProps {
  sessionId: string;
  children: ReactNode;
}

const isTerminal = (state: string) => state === "done" || state === "stopped" || state === "failed";

export function SessionHoverCard({ sessionId, children }: SessionHoverCardProps) {
  const ws = useWebSocket();
  const session = useChatStore((s) => s.sessions[sessionId]);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [renameOpen, setRenameOpen] = useState(false);
  const [renameValue, setRenameValue] = useState("");

  if (!session) return <>{children}</>;

  const { meta } = session;
  const terminal = isTerminal(meta.state);
  const hasWorktree = !!meta.worktreePath;
  const notMerged = hasWorktree && !meta.worktreeMerged;
  const ahead = !!meta.commitsAhead && meta.commitsAhead > 0;
  const behind = !!meta.commitsBehind && meta.commitsBehind > 0;
  const dirty = meta.hasUncommitted || meta.hasDirtyWorktree;

  const canInterrupt = meta.state === "running";
  const canMarkDone = meta.state === "idle";
  const canCreatePR = notMerged && ahead && !meta.prUrl;
  const hasOpenPR = !!meta.prUrl;
  const canMerge = terminal && notMerged && ahead;
  const canRebase = notMerged && behind;

  const hasStateActions = canInterrupt || canMarkDone;
  const hasGitActions = canCreatePR || hasOpenPR || canMerge || canRebase;

  const handleInterrupt = async () => {
    try {
      await interruptSession(ws, sessionId);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to interrupt");
    }
  };

  const handleMarkDone = async () => {
    try {
      await markSessionDone(ws, sessionId);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to mark done");
    }
  };

  const handleCreatePR = async () => {
    try {
      const result = await createPR(ws, sessionId);
      if (result.status === "created" || result.status === "existing") {
        toast.success("PR created");
      } else {
        toast.error(result.error);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to create PR");
    }
  };

  const handleOpenPR = () => {
    if (meta.prUrl) window.open(meta.prUrl, "_blank", "noopener");
  };

  const handleMerge = async () => {
    try {
      const result = await mergeSession(ws, sessionId, true);
      if (result.status === "merged") {
        toast.success("Merged successfully");
      } else if (result.status === "error") {
        toast.error(result.error);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to merge");
    }
  };

  const handleRebase = async () => {
    try {
      const result = await rebaseSession(ws, sessionId);
      if (result.status === "rebased") {
        toast.success("Rebased successfully");
      } else if (result.status === "error") {
        toast.error(result.error);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to rebase");
    }
  };

  const handleRename = async () => {
    const trimmed = renameValue.trim();
    if (!trimmed) return;
    try {
      await renameSession(ws, sessionId, trimmed);
      setRenameOpen(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to rename");
    }
  };

  const handleDelete = async () => {
    try {
      await deleteSession(ws, sessionId);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to delete");
    } finally {
      setDeleteOpen(false);
    }
  };

  return (
    <>
      <HoverCard openDelay={300} closeDelay={150}>
        <HoverCardTrigger asChild>
          <div>{children}</div>
        </HoverCardTrigger>
        <HoverCardContent side="right" align="start" sideOffset={8} className="w-52 p-0">
          <HoverCardArrow width={10} height={5} />

          {/* Git info header */}
          {meta.worktreeBranch && (
            <div className="px-3 py-2 border-b">
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <GitBranch className="size-3 shrink-0" />
                <span className="truncate font-mono">{meta.worktreeBranch}</span>
              </div>
              {(ahead || behind || dirty) && (
                <div className="flex items-center gap-2 mt-1 text-xs text-muted-foreground">
                  {ahead && (
                    <span className="flex items-center gap-0.5">
                      <ArrowUp className="size-2.5" />
                      {meta.commitsAhead}
                    </span>
                  )}
                  {behind && (
                    <span className="flex items-center gap-0.5 text-[#7aa2f7]/80">
                      <ArrowDown className="size-2.5" />
                      {meta.commitsBehind}
                    </span>
                  )}
                  {dirty && <span className="text-[#e0af68]/80">uncommitted</span>}
                </div>
              )}
            </div>
          )}

          {/* Actions */}
          <div className="py-1">
            {canInterrupt && (
              <ActionItem icon={Pause} label="Interrupt" onClick={handleInterrupt} />
            )}
            {canMarkDone && (
              <ActionItem icon={CircleCheck} label="Mark done" onClick={handleMarkDone} />
            )}

            {hasStateActions && hasGitActions && <Separator className="my-1" />}

            {canCreatePR && (
              <ActionItem icon={GitPullRequest} label="Create PR" onClick={handleCreatePR} />
            )}
            {hasOpenPR && <ActionItem icon={ExternalLink} label="Open PR" onClick={handleOpenPR} />}
            {canMerge && <ActionItem icon={GitMerge} label="Merge" onClick={handleMerge} />}
            {canRebase && <ActionItem icon={RefreshCw} label="Rebase" onClick={handleRebase} />}

            {(hasStateActions || hasGitActions) && <Separator className="my-1" />}

            <ActionItem
              icon={Pencil}
              label="Rename"
              onClick={() => {
                setRenameValue(meta.name);
                setRenameOpen(true);
              }}
            />
            <ActionItem
              icon={Trash2}
              label="Delete"
              onClick={() => setDeleteOpen(true)}
              destructive
            />
          </div>
        </HoverCardContent>
      </HoverCard>

      {/* Delete confirmation */}
      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete session</AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div>
                <p>This will remove the session and its data. This cannot be undone.</p>
                {meta.worktreePath && !meta.worktreeMerged && (
                  <p className="mt-2 font-medium text-[#e0af68]">
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

      {/* Rename dialog */}
      <Dialog open={renameOpen} onOpenChange={setRenameOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Rename session</DialogTitle>
          </DialogHeader>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              handleRename();
            }}
          >
            <Input
              value={renameValue}
              onChange={(e) => setRenameValue(e.target.value)}
              placeholder="Session name"
              autoFocus
            />
            <DialogFooter className="mt-4">
              <DialogClose asChild>
                <Button type="button" variant="outline">
                  Cancel
                </Button>
              </DialogClose>
              <Button type="submit" disabled={!renameValue.trim()}>
                Rename
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}

function ActionItem({
  icon: Icon,
  label,
  onClick,
  destructive,
}: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  onClick: () => void;
  destructive?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation();
        onClick();
      }}
      className={cn(
        "flex w-full items-center gap-2 px-3 py-1.5 text-sm transition-colors cursor-pointer",
        destructive
          ? "text-destructive hover:bg-destructive/10"
          : "text-popover-foreground hover:bg-accent",
      )}
    >
      <Icon className="size-3.5" />
      {label}
    </button>
  );
}
