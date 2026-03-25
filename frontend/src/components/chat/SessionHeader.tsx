import {
  ArrowDown,
  Check,
  ChevronDown,
  ExternalLink,
  FileDiff,
  FolderOpen,
  GitBranch,
  GitCommitHorizontal,
  GitMerge,
  Loader2,
  Pencil,
  Trash2,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { CommitDialog } from "~/components/chat/CommitDialog";
import { ConflictPanel } from "~/components/chat/ConflictPanel";
import { CreatePRDialog } from "~/components/chat/CreatePRDialog";
import { DiffView } from "~/components/chat/DiffView";
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
import {
  type DiffResult,
  MODELS,
  MODEL_LABELS,
  type ModelId,
  commitSession,
  createPR,
  deleteSession,
  getSessionDiff,
  markSessionDone,
  mergeSession,
  rebaseSession,
  renameSession,
  setSessionModel,
} from "~/lib/session-actions";
import { cn } from "~/lib/utils";
import type { SessionData } from "~/stores/chat-store";

interface SessionHeaderProps {
  session: SessionData;
  onSendMessage?: (prompt: string) => void;
}

export function SessionHeader({ session, onSendMessage }: SessionHeaderProps) {
  const { meta } = session;
  const ws = useWebSocket();
  const isRunning = meta.state === "running";
  const isWorktree = !!meta.worktreeBranch;
  const currentModel = (meta.model ?? "opus") as ModelId;
  const isBusy = isRunning;

  type ActiveDialog = "none" | "delete" | "pr" | "commit";
  const [activeDialog, setActiveDialog] = useState<ActiveDialog>("none");
  const [deleting, setDeleting] = useState(false);
  const [diffResult, setDiffResult] = useState<DiffResult | null>(null);
  const [showDiff, setShowDiff] = useState(false);
  const [loadingDiff, setLoadingDiff] = useState(false);
  const [conflictFiles, setConflictFiles] = useState<string[] | null>(null);
  const [merging, setMerging] = useState(false);
  const [rebasing, setRebasing] = useState(false);
  const [creatingPR, setCreatingPR] = useState(false);
  const [committing, setCommitting] = useState(false);
  const [editing, setEditing] = useState(false);
  const [editName, setEditName] = useState(meta.name);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (editing) {
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [editing]);

  // Keep editName in sync if name changes externally (e.g. auto-name)
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

  const handleModelChange = useCallback(
    (model: ModelId) => {
      if (model === currentModel) return;
      setSessionModel(ws, meta.id, model).catch((err) => {
        toast.error(err instanceof Error ? err.message : "Failed to change model");
      });
    },
    [ws, meta.id, currentModel],
  );

  const fetchDiff = useCallback(async () => {
    setLoadingDiff(true);
    try {
      const result = await getSessionDiff(ws, meta.id);
      setDiffResult(result);
      return result;
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load diff");
      return null;
    } finally {
      setLoadingDiff(false);
    }
  }, [ws, meta.id]);

  // Auto-fetch diff stats when session is not running
  useEffect(() => {
    if (!isRunning) fetchDiff();
  }, [isRunning, fetchDiff]);

  const handleViewDiff = async () => {
    if (showDiff) {
      setShowDiff(false);
      return;
    }
    const result = diffResult ?? (await fetchDiff());
    if (result) setShowDiff(true);
  };

  const diffTotals = diffResult?.files.reduce<{ add: number; del: number }>(
    (acc, f) => ({ add: acc.add + f.insertions, del: acc.del + f.deletions }),
    { add: 0, del: 0 },
  );

  const handleMerge = async (cleanup: boolean) => {
    setMerging(true);
    try {
      const result = await mergeSession(ws, meta.id, cleanup);
      if (result.status === "merged") {
        toast.success(`Merged (${result.commitHash?.slice(0, 7)})`);
      } else if (result.status === "conflict") {
        setConflictFiles(result.conflictFiles ?? []);
      } else {
        toast.error(result.error ?? "Merge failed");
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Merge failed");
    } finally {
      setMerging(false);
    }
  };

  const handleRebase = async () => {
    setRebasing(true);
    try {
      const result = await rebaseSession(ws, meta.id);
      if (result.status === "rebased") {
        toast.success("Rebased onto main");
      } else if (result.status === "conflict") {
        setConflictFiles(result.conflictFiles ?? []);
      } else {
        toast.error(result.error ?? "Rebase failed");
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Rebase failed");
    } finally {
      setRebasing(false);
    }
  };

  const handlePRSubmit = async (title: string, body: string) => {
    setCreatingPR(true);
    try {
      const result = await createPR(ws, meta.id, title, body);
      if (result.status === "created" || result.status === "existing") {
        toast.success(
          <span>
            PR {result.status}:{" "}
            <a href={result.url} target="_blank" rel="noreferrer" className="underline">
              {result.url}
            </a>
          </span>,
        );
        setActiveDialog("none");
      } else {
        toast.error(result.error ?? "PR creation failed");
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "PR creation failed");
    } finally {
      setCreatingPR(false);
    }
  };

  const handleCommit = async (message: string) => {
    setCommitting(true);
    try {
      const result = await commitSession(ws, meta.id, message);
      toast.success(`Committed (${result.commitHash.slice(0, 7)})`);
      setActiveDialog("none");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Commit failed");
    } finally {
      setCommitting(false);
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

  return (
    <>
      <div className="border-b px-4 py-2 flex items-center gap-2 text-sm shrink-0">
        <SessionStatusDot
          state={meta.state}
          connected={meta.connected}
          hasUnseenCompletion={session.hasUnseenCompletion}
          hasPendingApproval={!!session.pendingApproval || !!session.pendingQuestion}
        />
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
          <button
            type="button"
            onClick={() => setEditing(true)}
            className="group/name flex items-center gap-1 font-medium truncate hover:text-foreground"
          >
            <span className={cn("truncate", !meta.name && "italic text-muted-foreground")}>
              {meta.name || "Untitled"}
            </span>
            <Pencil className="h-3 w-3 opacity-0 group-hover/name:opacity-50 transition-opacity shrink-0" />
          </button>
        )}
        {meta.worktreeBranch ? (
          <span
            className={cn(
              "flex items-center gap-1 text-xs shrink-0",
              meta.hasDirtyWorktree
                ? "text-[#e0af68]/80"
                : meta.worktreeMerged
                  ? "text-[#9ece6a]/80"
                  : "text-muted-foreground",
            )}
            title={
              meta.hasDirtyWorktree
                ? `${meta.worktreeBranch} (dirty)`
                : meta.worktreeMerged
                  ? `${meta.worktreeBranch} (merged)`
                  : meta.worktreeBranch
            }
          >
            <GitBranch className="h-3 w-3" />
            {meta.worktreeBranch}
          </span>
        ) : (
          <span className="flex items-center gap-1 text-xs text-muted-foreground shrink-0">
            <FolderOpen className="h-3 w-3" />
            Local
          </span>
        )}

        <div className="ml-auto flex items-center gap-1.5">
          {/* Diff button */}
          <Button
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-xs"
            onClick={handleViewDiff}
            disabled={loadingDiff}
          >
            {loadingDiff ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <FileDiff className="h-3.5 w-3.5" />
            )}
            Changes
            {diffTotals && (diffTotals.add > 0 || diffTotals.del > 0) && (
              <span className="ml-0.5 tabular-nums">
                <span className="text-green-500">+{diffTotals.add}</span>{" "}
                <span className="text-red-500">-{diffTotals.del}</span>
              </span>
            )}
          </Button>

          {/* Commit button — non-worktree, non-busy */}
          {!isWorktree && !isBusy && (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 px-2 text-xs"
              onClick={() => setActiveDialog("commit")}
              disabled={committing}
            >
              {committing ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <GitCommitHorizontal className="h-3.5 w-3.5" />
              )}
              Commit
            </Button>
          )}

          {/* Rebase button — only when behind main */}
          {isWorktree && !isBusy && !!meta.commitsBehind && meta.commitsBehind > 0 && (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 px-2 text-xs"
              onClick={handleRebase}
              disabled={rebasing}
            >
              {rebasing ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <ArrowDown className="h-3.5 w-3.5" />
              )}
              Rebase ({meta.commitsBehind})
            </Button>
          )}

          {/* Merge dropdown */}
          {isWorktree && !isBusy && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="sm" className="h-7 px-2 text-xs" disabled={merging}>
                  {merging ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <GitMerge className="h-3.5 w-3.5" />
                  )}
                  Merge
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => handleMerge(false)} className="text-xs">
                  Merge
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => handleMerge(true)} className="text-xs">
                  Merge & clean up
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          )}

          {/* PR link (when created) or Create PR button */}
          {meta.prUrl ? (
            <a
              href={meta.prUrl}
              target="_blank"
              rel="noreferrer"
              className="flex items-center gap-1 h-7 px-2 text-xs text-[#7aa2f7]/80 hover:text-[#7aa2f7] transition-colors"
              title={meta.prUrl}
            >
              <ExternalLink className="h-3.5 w-3.5" />
              PR
            </a>
          ) : (
            isWorktree &&
            !isBusy && (
              <Button
                variant="ghost"
                size="sm"
                className="h-7 px-2 text-xs"
                onClick={() => setActiveDialog("pr")}
                disabled={creatingPR}
              >
                {creatingPR ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <ExternalLink className="h-3.5 w-3.5" />
                )}
                PR
              </Button>
            )
          )}

          {/* Model picker */}
          <DropdownMenu>
            <DropdownMenuTrigger
              disabled={isRunning}
              className={cn(
                "flex items-center gap-1 text-xs rounded border border-border px-1.5 py-0.5 text-muted-foreground transition-colors",
                "hover:bg-accent hover:text-accent-foreground",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                "disabled:opacity-50 disabled:pointer-events-none",
              )}
            >
              {MODEL_LABELS[currentModel]}
              <ChevronDown className="h-3 w-3" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              {MODELS.map((m) => (
                <DropdownMenuItem
                  key={m}
                  onClick={() => handleModelChange(m)}
                  className="text-xs gap-2"
                >
                  <Check
                    className={cn("h-3 w-3", m === currentModel ? "opacity-100" : "opacity-0")}
                  />
                  {MODEL_LABELS[m]}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>

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

          {/* Delete */}
          <Button
            variant="ghost"
            size="sm"
            className="h-7 px-1.5 text-xs text-muted-foreground hover:text-destructive"
            onClick={() => setActiveDialog("delete")}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>

          {/* State label */}
          <span className="text-xs text-muted-foreground capitalize">{meta.state}</span>
        </div>
      </div>

      {/* Diff panel */}
      {showDiff && diffResult && <DiffView result={diffResult} />}

      {/* Conflict panel */}
      {conflictFiles && (
        <ConflictPanel
          files={conflictFiles}
          onDismiss={() => setConflictFiles(null)}
          onAskResolve={
            onSendMessage
              ? () => {
                  const fileList = conflictFiles.join(", ");
                  onSendMessage(
                    `There are merge conflicts in the following files: ${fileList}. Please resolve them.`,
                  );
                }
              : undefined
          }
        />
      )}

      {/* Create PR dialog */}
      <CreatePRDialog
        open={activeDialog === "pr"}
        onOpenChange={(open) => setActiveDialog(open ? "pr" : "none")}
        sessionId={meta.id}
        defaultTitle={meta.name}
        onSubmit={handlePRSubmit}
        loading={creatingPR}
      />

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

      {/* Commit dialog */}
      <CommitDialog
        open={activeDialog === "commit"}
        onOpenChange={(open) => setActiveDialog(open ? "commit" : "none")}
        sessionId={meta.id}
        onSubmit={handleCommit}
        loading={committing}
      />
    </>
  );
}
