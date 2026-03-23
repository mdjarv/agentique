import {
  Check,
  ChevronDown,
  ExternalLink,
  FileDiff,
  FolderOpen,
  GitBranch,
  GitCommitHorizontal,
  GitMerge,
  Loader2,
  Trash2,
} from "lucide-react";
import { useCallback, useState } from "react";
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
  type ModelId,
  commitSession,
  createPR,
  deleteSession,
  getSessionDiff,
  mergeSession,
  setSessionModel,
} from "~/lib/session-actions";
import { cn } from "~/lib/utils";
import type { SessionData } from "~/stores/chat-store";

interface SessionHeaderProps {
  session: SessionData;
}

const MODEL_LABELS: Record<ModelId, string> = {
  haiku: "Haiku",
  sonnet: "Sonnet",
  opus: "Opus",
};

export function SessionHeader({ session }: SessionHeaderProps) {
  const { meta } = session;
  const ws = useWebSocket();
  const isRunning = meta.state === "running" || meta.state === "starting";
  const isDraft = meta.state === "draft";
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
  const [creatingPR, setCreatingPR] = useState(false);
  const [committing, setCommitting] = useState(false);

  const handleModelChange = useCallback(
    (model: ModelId) => {
      if (isDraft || model === currentModel) return;
      setSessionModel(ws, meta.id, model).catch((err) => {
        toast.error(err instanceof Error ? err.message : "Failed to change model");
      });
    },
    [ws, meta.id, isDraft, currentModel],
  );

  const handleViewDiff = async () => {
    if (showDiff) {
      setShowDiff(false);
      return;
    }
    setLoadingDiff(true);
    try {
      const result = await getSessionDiff(ws, meta.id);
      setDiffResult(result);
      setShowDiff(true);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load diff");
    } finally {
      setLoadingDiff(false);
    }
  };

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
          hasUnseenCompletion={session.hasUnseenCompletion}
          hasPendingApproval={!!session.pendingApproval || !!session.pendingQuestion}
        />
        <span className="font-medium truncate">{meta.name}</span>
        {meta.worktreeBranch ? (
          <span className="flex items-center gap-1 text-xs text-muted-foreground shrink-0">
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
          {/* Diff button — available for all non-draft sessions */}
          {!isDraft && (
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
            </Button>
          )}

          {/* Commit button — non-worktree, non-draft, non-busy */}
          {!isWorktree && !isDraft && !isBusy && (
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

          {/* Create PR */}
          {isWorktree && !isBusy && (
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
          )}

          {/* Model picker */}
          {!isDraft && (
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
        <ConflictPanel files={conflictFiles} onDismiss={() => setConflictFiles(null)} />
      )}

      {/* Create PR dialog */}
      <CreatePRDialog
        open={activeDialog === "pr"}
        onOpenChange={(open) => setActiveDialog(open ? "pr" : "none")}
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
              Delete &quot;{meta.name}&quot;? This removes the worktree, branch, and all session
              data.
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
        onSubmit={handleCommit}
        loading={committing}
      />
    </>
  );
}
