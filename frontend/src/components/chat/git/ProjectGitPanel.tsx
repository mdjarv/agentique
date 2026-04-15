import {
  ArrowDown,
  ArrowUp,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  FileMinus,
  FilePlus,
  FileQuestion,
  FileText,
  GitBranch,
  GitCommitHorizontal,
  Loader2,
  Trash2,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
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
import { useProjectGitActions } from "~/hooks/git/useProjectGitActions";
import { ANIMATE_DEFAULT, useAutoAnimate } from "~/hooks/useAutoAnimate";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { FileStatus } from "~/lib/generated-types";
import {
  commitProject,
  discardProjectChanges,
  getProjectUncommittedFiles,
} from "~/lib/project-actions";
import { cn, getErrorMessage } from "~/lib/utils";
import { type ProjectGitStatus, useAppStore } from "~/stores/app-store";

const CARD = "rounded-md border border-border/50 overflow-hidden text-xs";
const CARD_HEADER = "flex items-center gap-1.5 px-3 py-2 bg-muted/30";
const CARD_ROW = "flex items-center gap-2 px-3 py-2 border-t border-border/30";

// --- File icons (mirrored from GitView) ---

const fileIconMap = {
  modified: FileText,
  added: FilePlus,
  deleted: FileMinus,
  renamed: FileText,
  untracked: FileQuestion,
} as const;

const fileColorMap: Record<string, string> = {
  modified: "text-warning/70",
  added: "text-success/70",
  deleted: "text-destructive/70",
  renamed: "text-primary/70",
  untracked: "text-muted-foreground-faint",
};

function UncommittedFileIcon({ status }: { status: string }) {
  const Icon = fileIconMap[status as keyof typeof fileIconMap] ?? FileText;
  const color = fileColorMap[status] ?? "text-muted-foreground-faint";
  return <Icon className={`h-3 w-3 shrink-0 ${color}`} />;
}

// --- Branch context card ---

function BranchContextCard({ gitStatus }: { gitStatus: ProjectGitStatus }) {
  return (
    <div className={cn(CARD, "flex items-center gap-1.5 px-3 py-2")}>
      <GitBranch className="h-3 w-3 shrink-0 text-muted-foreground-dim" />
      <span className="font-mono truncate min-w-0 text-foreground/80">{gitStatus.branch}</span>
      {gitStatus.hasRemote && (
        <span className="text-muted-foreground-faint">{"\u2192"} origin</span>
      )}
      {!gitStatus.hasRemote && <span className="text-muted-foreground-faint">local only</span>}
    </div>
  );
}

// --- Uncommitted files card ---

function UncommittedFilesCard({
  gitStatus,
  projectId,
}: {
  gitStatus: ProjectGitStatus;
  projectId: string;
}) {
  const ws = useWebSocket();
  const [expanded, setExpanded] = useState(false);
  const [files, setFiles] = useState<FileStatus[]>([]);
  const [loading, setLoading] = useState(false);
  const [committing, setCommitting] = useState(false);
  const [filesRef] = useAutoAnimate<HTMLUListElement>(ANIMATE_DEFAULT);

  const count = gitStatus.uncommittedCount;

  useEffect(() => {
    if (!expanded || count === 0) return;
    let cancelled = false;
    setLoading(true);
    getProjectUncommittedFiles(ws, projectId)
      .then((r) => {
        if (!cancelled) setFiles(r.files);
      })
      .catch((err) => {
        console.error("getProjectUncommittedFiles failed", err);
        toast.error(getErrorMessage(err, "Failed to load uncommitted files"));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [ws, projectId, expanded, count]);

  if (count === 0) return null;

  const handleCommit = async () => {
    setCommitting(true);
    try {
      await commitProject(ws, projectId, "");
      toast.success("Committed");
      setFiles([]);
    } catch (err) {
      toast.error(getErrorMessage(err, "Commit failed"));
    } finally {
      setCommitting(false);
    }
  };

  return (
    <div className={CARD}>
      <div className={CARD_HEADER}>
        <button
          type="button"
          onClick={() => setExpanded((e) => !e)}
          className="flex items-center gap-1.5 text-warning/80 hover:text-warning transition-colors min-w-0"
        >
          {expanded ? (
            <ChevronDown className="h-3 w-3 shrink-0" />
          ) : (
            <ChevronRight className="h-3 w-3 shrink-0" />
          )}
          <span>
            {count} uncommitted {count === 1 ? "file" : "files"}
          </span>
        </button>
        <Button
          variant="ghost"
          size="xs"
          className="ml-auto shrink-0"
          onClick={handleCommit}
          disabled={committing}
        >
          {committing ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <GitCommitHorizontal className="h-3 w-3" />
          )}
          Commit
        </Button>
      </div>
      {expanded && (
        <ul ref={filesRef} className="border-t border-border/30 px-3 py-2 space-y-1">
          {loading ? (
            <li className="flex items-center gap-1.5 text-muted-foreground">
              <Loader2 className="h-3 w-3 animate-spin" />
              <span className="text-[11px]">Loading...</span>
            </li>
          ) : (
            files.map((f) => (
              <li key={f.path} className="flex items-center gap-1.5 text-muted-foreground">
                <UncommittedFileIcon status={f.status} />
                <span className="font-mono truncate min-w-0 text-[11px]">{f.path}</span>
              </li>
            ))
          )}
        </ul>
      )}
    </div>
  );
}

// --- Remote sync card (mirrors RemoteActions from GitView) ---

function RemoteSyncCard({
  gitStatus,
  actions,
}: {
  gitStatus: ProjectGitStatus;
  actions: ReturnType<typeof useProjectGitActions>;
}) {
  if (!gitStatus.hasRemote) return null;

  const canPush = gitStatus.aheadRemote > 0;
  const canPull = gitStatus.behindRemote > 0;

  return (
    <div className={CARD}>
      <div className={CARD_HEADER}>
        <GitBranch className="h-3 w-3 shrink-0 text-muted-foreground-dim" />
        <span className="font-mono text-foreground/80">{gitStatus.branch}</span>
        <span className="text-muted-foreground-faint">{"\u2192"} origin</span>
        <button
          type="button"
          onClick={actions.handleFetch}
          disabled={actions.fetching}
          className="ml-auto text-[11px] text-muted-foreground hover:text-foreground transition-colors disabled:opacity-50 cursor-pointer"
        >
          {actions.fetching ? "Fetching\u2026" : "Fetch"}
        </button>
      </div>
      {canPush && (
        <div className={CARD_ROW}>
          <ArrowUp className="h-3 w-3 text-success/60 shrink-0" />
          <span className="text-muted-foreground">
            {gitStatus.aheadRemote} {gitStatus.aheadRemote === 1 ? "commit" : "commits"} ahead
          </span>
          <button
            type="button"
            onClick={actions.handlePush}
            disabled={actions.pushing}
            className="ml-auto inline-flex items-center gap-1 text-[11px] font-medium px-2 py-0.5 rounded-md bg-success/15 text-success hover:bg-success/25 transition-colors disabled:opacity-50 cursor-pointer"
          >
            {actions.pushing ? "Pushing\u2026" : "Push"}
          </button>
        </div>
      )}
      {canPull && (
        <div className={CARD_ROW}>
          <ArrowDown className="h-3 w-3 text-orange/60 shrink-0" />
          <span className="text-muted-foreground">
            {gitStatus.behindRemote} {gitStatus.behindRemote === 1 ? "commit" : "commits"} behind
          </span>
          <button
            type="button"
            onClick={actions.handlePull}
            disabled={actions.pulling}
            className="ml-auto inline-flex items-center gap-1 text-[11px] font-medium px-2 py-0.5 rounded-md bg-orange/15 text-orange hover:bg-orange/25 transition-colors disabled:opacity-50 cursor-pointer"
          >
            {actions.pulling ? "Pulling\u2026" : "Pull"}
          </button>
        </div>
      )}
      {!canPush && !canPull && (
        <div className={cn(CARD_ROW, "text-muted-foreground-faint")}>
          <CheckCircle2 className="h-3 w-3 shrink-0" />
          Up to date
        </div>
      )}
    </div>
  );
}

// --- Discard card ---

function DiscardCard({ projectId }: { projectId: string }) {
  const ws = useWebSocket();
  const [discarding, setDiscarding] = useState(false);
  const [showConfirm, setShowConfirm] = useState(false);

  const handleDiscard = useCallback(async () => {
    setDiscarding(true);
    try {
      const status = await discardProjectChanges(ws, projectId);
      useAppStore.getState().setProjectGitStatus(status);
      toast.success("Changes discarded");
    } catch (err) {
      toast.error(getErrorMessage(err, "Discard failed"));
    } finally {
      setDiscarding(false);
      setShowConfirm(false);
    }
  }, [ws, projectId]);

  return (
    <>
      <div className={cn(CARD, "px-3 py-2")}>
        <Button
          variant="ghost"
          size="xs"
          className="w-full justify-start text-destructive/70 hover:text-destructive hover:bg-destructive/10"
          onClick={() => setShowConfirm(true)}
          disabled={discarding}
        >
          {discarding ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <Trash2 className="h-3 w-3" />
          )}
          Discard all changes
        </Button>
      </div>
      <AlertDialog open={showConfirm} onOpenChange={setShowConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Discard all changes</AlertDialogTitle>
            <AlertDialogDescription>
              This will reset all tracked files and remove untracked files. This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleDiscard} disabled={discarding}>
              {discarding ? "Discarding..." : "Discard"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

// --- Main panel ---

export function ProjectGitPanel({
  projectId,
  gitStatus,
}: {
  projectId: string;
  gitStatus: ProjectGitStatus;
}) {
  const actions = useProjectGitActions(projectId);
  const hasUncommitted = gitStatus.uncommittedCount > 0;

  return (
    <div className="max-w-lg space-y-3">
      <BranchContextCard gitStatus={gitStatus} />
      <UncommittedFilesCard gitStatus={gitStatus} projectId={projectId} />
      <RemoteSyncCard gitStatus={gitStatus} actions={actions} />
      {hasUncommitted && <DiscardCard projectId={projectId} />}
    </div>
  );
}
