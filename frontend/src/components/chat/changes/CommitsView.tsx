import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  ChevronDown,
  ChevronRight,
  ExternalLink,
  FileMinus,
  FilePlus,
  FileQuestion,
  FileText,
  GitCommitHorizontal,
  GitMerge,
  GitPullRequestArrow,
  Loader2,
  RefreshCw,
  Sparkles,
  Trash2,
  XCircle,
} from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { ProjectCommitDialog } from "~/components/layout/git/ProjectCommitDialog";
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
import type { useGitActions } from "~/hooks/git/useGitActions";
import type { useProjectGitActions } from "~/hooks/git/useProjectGitActions";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { FileStatus } from "~/lib/generated-types";
import { getProjectUncommittedFiles } from "~/lib/project-actions";
import { type CommitLogEntry, getPRStatus, type PRStatusResult } from "~/lib/session/actions";
import { cn, getErrorMessage, relativeTime } from "~/lib/utils";
import type { ProjectGitStatus } from "~/stores/app-store";
import type { SessionMetadata } from "~/stores/chat-store";
import { FilePath } from "../git/FilePath";

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

interface CommitsViewProps {
  meta: SessionMetadata;
  git: ReturnType<typeof useGitActions>;
  mainBranch: string;
  projectGitStatus?: ProjectGitStatus;
  projectGitActions?: ReturnType<typeof useProjectGitActions>;
  onSendMessage: (prompt: string) => void;
  onSelectFile?: (path: string) => void;
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

function SectionHeader({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-4 pt-4 pb-1.5 text-[10px] font-medium uppercase tracking-wider text-muted-foreground-dim">
      {children}
    </div>
  );
}

function extractPRNumber(url: string): string | null {
  const match = url.match(/\/pull\/(\d+)/);
  return match ? `#${match[1]}` : null;
}

function ChecksIndicator({ status }: { status: string }) {
  switch (status) {
    case "pass":
      return (
        <span title="Checks passing">
          <Loader2 className="h-3 w-3 text-success" />
        </span>
      );
    case "fail":
      return (
        <span title="Checks failing">
          <XCircle className="h-3 w-3 text-destructive" />
        </span>
      );
    case "pending":
      return (
        <span title="Checks pending">
          <Loader2 className="h-3 w-3 text-warning animate-spin" />
        </span>
      );
    default:
      return null;
  }
}

const uncommittedFileIconMap = {
  modified: FileText,
  added: FilePlus,
  deleted: FileMinus,
  renamed: FileText,
  untracked: FileQuestion,
} as const;

const uncommittedFileColorMap: Record<string, string> = {
  modified: "text-warning/70",
  added: "text-success/70",
  deleted: "text-destructive/70",
  renamed: "text-primary/70",
  untracked: "text-muted-foreground-faint",
};

function UncommittedFileIcon({ status }: { status: string }) {
  const Icon = uncommittedFileIconMap[status as keyof typeof uncommittedFileIconMap] ?? FileText;
  const color = uncommittedFileColorMap[status] ?? "text-muted-foreground-faint";
  return <Icon className={`h-3 w-3 shrink-0 ${color}`} />;
}

// ---------------------------------------------------------------------------
// Sections
// ---------------------------------------------------------------------------

function CommitRow({ commit }: { commit: CommitLogEntry }) {
  const [expanded, setExpanded] = useState(false);
  const hasBody = !!commit.body;

  return (
    <li>
      <button
        type="button"
        onClick={hasBody ? () => setExpanded((e) => !e) : undefined}
        className={cn(
          "flex items-center gap-2.5 py-1.5 text-[11px] w-full text-left",
          hasBody && "cursor-pointer hover:bg-muted/30 -mx-1.5 px-1.5 rounded",
        )}
      >
        <GitCommitHorizontal className="h-3 w-3 shrink-0 text-muted-foreground-faint" />
        <span className="font-mono text-muted-foreground-dim shrink-0">{commit.hash}</span>
        <span className="text-foreground-dim truncate min-w-0 flex-1">{commit.message}</span>
        <span className="text-muted-foreground-dim shrink-0">{relativeTime(commit.timestamp)}</span>
        {hasBody &&
          (expanded ? (
            <ChevronDown className="h-3 w-3 shrink-0 text-muted-foreground-faint" />
          ) : (
            <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground-faint" />
          ))}
      </button>
      {expanded && commit.body && (
        <pre className="ml-[22px] pl-2.5 mb-1 text-[11px] leading-relaxed text-muted-foreground whitespace-pre-wrap border-l-2 border-border">
          {commit.body}
        </pre>
      )}
    </li>
  );
}

function CommitLog({ git, ahead }: { git: ReturnType<typeof useGitActions>; ahead: number }) {
  if (ahead === 0) return null;

  return (
    <div>
      <SectionHeader>
        {ahead} {ahead === 1 ? "commit" : "commits"} ahead
      </SectionHeader>
      <ul className="px-4 pb-2 space-y-0.5">
        {git.commitLogLoading ? (
          <li className="flex items-center gap-2 text-muted-foreground py-1.5">
            <Loader2 className="h-3 w-3 animate-spin" />
            <span className="text-[11px]">Loading...</span>
          </li>
        ) : git.commitLog && git.commitLog.length > 0 ? (
          git.commitLog.map((c) => <CommitRow key={c.hash} commit={c} />)
        ) : (
          <li className="text-[11px] text-muted-foreground-dim py-1.5">No commits found</li>
        )}
      </ul>
    </div>
  );
}

function PRSection({ meta }: { meta: SessionMetadata }) {
  const ws = useWebSocket();
  const [prStatus, setPrStatus] = useState<PRStatusResult | null>(null);

  useEffect(() => {
    if (!meta.prUrl || !meta.id) return;
    let cancelled = false;
    getPRStatus(ws, meta.id)
      .then((r) => {
        if (!cancelled) setPrStatus(r);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [ws, meta.prUrl, meta.id]);

  if (!meta.prUrl) return null;

  const prNumber = prStatus ? `#${prStatus.number}` : extractPRNumber(meta.prUrl);

  return (
    <div className="px-4 py-2">
      <a
        href={meta.prUrl}
        target="_blank"
        rel="noreferrer"
        className="flex items-center gap-2 px-3 py-2 text-xs text-primary/80 hover:text-primary bg-primary/5 hover:bg-primary/10 rounded-lg transition-colors"
      >
        <GitPullRequestArrow className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate flex-1 font-medium">PR{prNumber ? ` ${prNumber}` : ""}</span>
        {prStatus?.isDraft && <span className="text-[11px] text-muted-foreground">Draft</span>}
        {prStatus && prStatus.state === "MERGED" && (
          <span className="flex items-center gap-0.5 text-[11px] text-purple-400">
            <GitMerge className="h-3 w-3" />
          </span>
        )}
        {prStatus && <ChecksIndicator status={prStatus.checksStatus} />}
        <ExternalLink className="h-3 w-3 shrink-0 opacity-40" />
      </a>
    </div>
  );
}

function ConflictsSection({
  meta,
  mainBranch,
  onSendMessage,
}: {
  meta: SessionMetadata;
  mainBranch: string;
  onSendMessage: (prompt: string) => void;
}) {
  if (meta.mergeStatus !== "conflicts") return null;
  const isBusy = meta.state === "running";
  const conflictCount = meta.mergeConflictFiles?.length ?? 0;

  return (
    <div className="mx-4 my-2 px-3 py-2.5 rounded-lg bg-warning/5 border border-warning/20">
      <div className="flex items-center gap-1.5 text-xs text-warning mb-1.5">
        <AlertTriangle className="h-3 w-3 shrink-0" />
        <span className="font-medium">
          {conflictCount} merge {conflictCount === 1 ? "conflict" : "conflicts"}
        </span>
      </div>
      {meta.mergeConflictFiles && meta.mergeConflictFiles.length > 0 && (
        <ul className="space-y-0.5 mb-2">
          {meta.mergeConflictFiles.map((f) => (
            <li key={f} className="font-mono truncate text-[11px] text-warning/70">
              {f}
            </li>
          ))}
        </ul>
      )}
      {!isBusy && (
        <>
          <Button
            variant="ghost"
            size="xs"
            className="text-warning hover:text-warning hover:bg-warning/10"
            onClick={() => {
              const files = meta.mergeConflictFiles?.join(", ") ?? "";
              onSendMessage(
                `This is a git worktree. Rebase onto the local project HEAD (not origin). Get it via: main_wt=$(git worktree list --porcelain | head -1 | sed 's/worktree //') && git rebase $(git -C "$main_wt" rev-parse HEAD). Resolve conflicts in: ${files}`,
              );
            }}
          >
            <Sparkles className="h-3 w-3 text-primary/60" />
            Resolve conflicts
          </Button>
          <p className="text-[10px] text-muted-foreground mt-1">
            Claude will rebase onto {mainBranch} and resolve the conflicting files
          </p>
        </>
      )}
    </div>
  );
}

function RebaseSection({
  meta,
  git,
  mainBranch,
}: {
  meta: SessionMetadata;
  git: ReturnType<typeof useGitActions>;
  mainBranch: string;
}) {
  const behind = meta.commitsBehind ?? 0;
  if (behind === 0 || meta.mergeStatus === "conflicts") return null;
  const isBusy = meta.state === "running";

  return (
    <div className="px-4 py-2">
      <div className="flex items-center gap-2 text-xs">
        <ArrowDown className="h-3 w-3 text-orange/50 shrink-0" />
        <span className="text-muted-foreground">
          {mainBranch} has {behind} new {behind === 1 ? "commit" : "commits"}
        </span>
        {!isBusy && (
          <Button
            variant="ghost"
            size="xs"
            className="ml-auto text-orange/80 hover:bg-orange/10"
            onClick={git.handleRebase}
            disabled={git.rebasing}
          >
            {git.rebasing ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <RefreshCw className="h-3 w-3" />
            )}
            Rebase
          </Button>
        )}
      </div>
    </div>
  );
}

function RemoteSection({
  gitStatus,
  actions,
}: {
  gitStatus: ProjectGitStatus;
  actions: ReturnType<typeof useProjectGitActions>;
}) {
  if (!gitStatus.hasRemote) return null;
  const canPush = gitStatus.aheadRemote > 0;
  const canPull = gitStatus.behindRemote > 0;
  if (!canPush && !canPull) return null;

  return (
    <div className="flex items-center gap-2 px-4 py-3">
      {canPush && (
        <button
          type="button"
          onClick={actions.handlePush}
          disabled={actions.pushing}
          className="flex items-center gap-1.5 text-xs font-medium h-8 px-3 rounded-md border border-success/30 bg-success/10 text-success hover:bg-success/20 transition-colors disabled:opacity-50 cursor-pointer"
        >
          <ArrowUp className="h-3.5 w-3.5" />
          {actions.pushing
            ? "Pushing\u2026"
            : `Push ${gitStatus.aheadRemote} ${gitStatus.aheadRemote === 1 ? "commit" : "commits"}`}
        </button>
      )}
      {canPull && (
        <button
          type="button"
          onClick={actions.handlePull}
          disabled={actions.pulling}
          title="git merge --ff-only from remote"
          className="flex items-center gap-1.5 text-xs font-medium h-8 px-3 rounded-md border border-orange/30 bg-orange/10 text-orange hover:bg-orange/20 transition-colors disabled:opacity-50 cursor-pointer"
        >
          <ArrowDown className="h-3.5 w-3.5" />
          {actions.pulling
            ? "Fast-forwarding\u2026"
            : `Fast-forward ${gitStatus.behindRemote} ${gitStatus.behindRemote === 1 ? "commit" : "commits"}`}
        </button>
      )}
    </div>
  );
}

function ProjectDirtySection({
  projectId,
  uncommittedCount,
  onCommitProject,
  committing,
  onDiscard,
  discarding,
  onSelectFile,
}: {
  projectId: string;
  uncommittedCount: number;
  onCommitProject: (message: string) => Promise<void>;
  committing: boolean;
  onDiscard: () => Promise<void>;
  discarding: boolean;
  onSelectFile?: (path: string) => void;
}) {
  const ws = useWebSocket();
  const [expanded, setExpanded] = useState(false);
  const [files, setFiles] = useState<FileStatus[]>([]);
  const [loading, setLoading] = useState(false);
  const [commitOpen, setCommitOpen] = useState(false);
  const [discardOpen, setDiscardOpen] = useState(false);

  useEffect(() => {
    if (!expanded || uncommittedCount === 0) return;
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
  }, [ws, projectId, expanded, uncommittedCount]);

  return (
    <>
      <div className="mx-4 my-2 rounded-lg bg-warning/5 border border-warning/20">
        <button
          type="button"
          onClick={() => setExpanded((e) => !e)}
          className="flex items-center gap-1.5 px-3 py-2 text-xs text-warning hover:text-warning/80 w-full transition-colors"
        >
          {expanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
          <AlertTriangle className="h-3 w-3" />
          <span>
            Project: {uncommittedCount} uncommitted {uncommittedCount === 1 ? "file" : "files"}
          </span>
          <span className="ml-auto text-[10px] text-warning/50">merge blocked</span>
        </button>

        {expanded && (
          <>
            <ul className="px-3 pb-1.5 space-y-1">
              {loading ? (
                <li className="flex items-center gap-1.5 text-muted-foreground">
                  <Loader2 className="h-3 w-3 animate-spin" />
                  <span className="text-[11px]">Loading...</span>
                </li>
              ) : (
                files.map((f) => (
                  <li key={f.path} className="flex items-center gap-1.5 text-muted-foreground">
                    <UncommittedFileIcon status={f.status} />
                    {onSelectFile ? (
                      <button
                        type="button"
                        onClick={() => onSelectFile(f.path)}
                        className="font-mono truncate min-w-0 text-[11px] hover:text-foreground transition-colors text-left flex"
                      >
                        <FilePath path={f.path} className="truncate min-w-0 flex" />
                      </button>
                    ) : (
                      <FilePath
                        path={f.path}
                        className="font-mono truncate min-w-0 text-[11px] flex"
                      />
                    )}
                  </li>
                ))
              )}
            </ul>
            <div className="flex items-center gap-1.5 px-3 py-2 border-t border-warning/20">
              <Button
                variant="ghost"
                size="xs"
                onClick={() => setCommitOpen(true)}
                disabled={committing}
              >
                {committing ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <GitCommitHorizontal className="h-3 w-3" />
                )}
                Commit
              </Button>
              <Button
                variant="ghost"
                size="xs"
                className="text-destructive/70 hover:text-destructive hover:bg-destructive/10"
                onClick={() => setDiscardOpen(true)}
                disabled={discarding}
              >
                {discarding ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <Trash2 className="h-3 w-3" />
                )}
                Discard
              </Button>
            </div>
          </>
        )}
      </div>

      <ProjectCommitDialog
        open={commitOpen}
        onOpenChange={setCommitOpen}
        projectId={projectId}
        onSubmit={(msg) => {
          onCommitProject(msg).then(() => setCommitOpen(false));
        }}
        loading={committing}
      />

      <AlertDialog open={discardOpen} onOpenChange={setDiscardOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Discard project changes</AlertDialogTitle>
            <AlertDialogDescription>
              This will reset all tracked files and remove untracked files in the project root. This
              cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                onDiscard().then(() => setDiscardOpen(false));
              }}
              disabled={discarding}
            >
              {discarding ? "Discarding..." : "Discard"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export function CommitsView({
  meta,
  git,
  mainBranch,
  projectGitStatus,
  projectGitActions,
  onSendMessage,
  onSelectFile,
}: CommitsViewProps) {
  const isWorktree = !!meta.worktreeBranch;
  const ahead = meta.commitsAhead ?? 0;
  const projectDirty = !!projectGitStatus && projectGitStatus.uncommittedCount > 0;

  return (
    <div className="flex-1 overflow-y-auto min-h-0">
      {!isWorktree && projectGitStatus && projectGitActions && (
        <RemoteSection gitStatus={projectGitStatus} actions={projectGitActions} />
      )}

      {isWorktree && (
        <>
          <CommitLog git={git} ahead={ahead} />
          <PRSection meta={meta} />
          <ConflictsSection meta={meta} mainBranch={mainBranch} onSendMessage={onSendMessage} />
          <RebaseSection meta={meta} git={git} mainBranch={mainBranch} />
        </>
      )}

      {isWorktree &&
        !meta.branchMissing &&
        ahead > 0 &&
        projectGitStatus &&
        projectDirty &&
        projectGitActions && (
          <ProjectDirtySection
            projectId={projectGitStatus.projectId}
            uncommittedCount={projectGitStatus.uncommittedCount}
            onCommitProject={projectGitActions.handleCommit}
            committing={projectGitActions.committing}
            onDiscard={projectGitActions.handleDiscard}
            discarding={projectGitActions.discarding}
            onSelectFile={onSelectFile}
          />
        )}
    </div>
  );
}
