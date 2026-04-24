import {
  AlertTriangle,
  ArrowDown,
  CheckCircle2,
  GitBranch,
  GitPullRequestArrow,
  Loader2,
  RefreshCw,
  Sparkles,
} from "lucide-react";
import { useCallback } from "react";
import { MergeDropdown } from "~/components/chat/MergeDropdown";
import { Button } from "~/components/ui/button";
import type { useGitActions } from "~/hooks/git/useGitActions";
import { useIsMobile } from "~/hooks/useIsMobile";
import { cn } from "~/lib/utils";
import type { ProjectGitStatus } from "~/stores/app-store";
import type { SessionMetadata } from "~/stores/chat-store";

interface GitStatusBarProps {
  meta: SessionMetadata;
  git: ReturnType<typeof useGitActions>;
  mainBranch?: string;
  projectGitStatus?: ProjectGitStatus;
  onSendMessage: (prompt: string) => void;
  onOpenDialog: (dialog: "pr" | "commit") => void;
}

export function GitStatusBar({
  meta,
  git,
  mainBranch,
  projectGitStatus,
  onSendMessage,
  onOpenDialog,
}: GitStatusBarProps) {
  const isMobile = useIsMobile();
  const isWorktree = !!meta.worktreeBranch;
  const isBusy = meta.state === "running";
  const isMerged =
    meta.worktreeMerged && (meta.commitsAhead ?? 0) === 0 && (meta.commitsBehind ?? 0) === 0;
  const ahead = meta.commitsAhead ?? 0;
  const behind = meta.commitsBehind ?? 0;
  const main = mainBranch || "main";
  const hasUncommitted = !!git.uncommittedFiles && git.uncommittedFiles.length > 0;
  const uncommittedCount = git.uncommittedFiles?.length ?? 0;
  const projectDirty = !!projectGitStatus && projectGitStatus.uncommittedCount > 0;
  const canRebase = isWorktree && behind > 0 && meta.mergeStatus !== "conflicts";

  const handleCommit = useCallback(() => {
    onSendMessage(
      "Commit all changes. Stage everything and write a clear commit message based on what you changed.",
    );
  }, [onSendMessage]);

  const actionButtons = (
    <>
      {canRebase && !isBusy && (
        <Button
          variant="outline"
          size="sm"
          onClick={git.handleRebase}
          disabled={git.rebasing}
          className="border-orange/30 text-orange hover:bg-orange/10"
        >
          {git.rebasing ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <RefreshCw className="h-3.5 w-3.5" />
          )}
          Rebase
        </Button>
      )}

      {hasUncommitted && !isBusy && (
        <Button variant="outline" size="sm" onClick={handleCommit} disabled={git.committing}>
          {git.committing ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Sparkles className="h-3.5 w-3.5 text-primary" />
          )}
          Commit
        </Button>
      )}

      {isWorktree && !meta.branchMissing && !isMerged && ahead > 0 && !isBusy && (
        <MergeDropdown
          git={git}
          projectDirty={projectDirty}
          className={cn(
            "border",
            meta.mergeStatus === "clean" && !hasUncommitted
              ? "bg-success/10 text-success border-success/30 hover:bg-success/20"
              : "hover:bg-accent",
          )}
        />
      )}

      {isWorktree && !meta.branchMissing && !isMerged && !meta.prUrl && !isBusy && (
        <Button
          variant="outline"
          size="sm"
          onClick={() => onOpenDialog("pr")}
          disabled={git.creatingPR}
        >
          {git.creatingPR ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <GitPullRequestArrow className="h-3.5 w-3.5" />
          )}
          PR
        </Button>
      )}
    </>
  );

  return (
    <div className="border-b shrink-0 text-xs">
      {/* Branch info row */}
      <div className="flex items-center gap-2 px-3 py-2.5">
        <GitBranch className="h-3.5 w-3.5 shrink-0 text-muted-foreground-faint" />
        {isWorktree ? (
          <>
            <span className="font-mono truncate text-foreground font-medium">
              {meta.worktreeBranch}
            </span>
            <span className="text-muted-foreground-faint">{"\u2192"}</span>
            <span className="font-mono text-muted-foreground">{main}</span>
          </>
        ) : (
          <span className="font-mono text-foreground font-medium">
            {projectGitStatus?.branch ?? main}
          </span>
        )}

        {/* Badges */}
        <span className="flex items-center gap-2 text-[11px]">
          {isMerged && (
            <span className="flex items-center gap-0.5 text-success">
              <CheckCircle2 className="h-3 w-3" />
              Merged
            </span>
          )}
          {hasUncommitted && (
            <span className="flex items-center gap-0.5 text-warning">
              <span className="inline-block h-1.5 w-1.5 rounded-full bg-warning" />
              {uncommittedCount}
            </span>
          )}
          {canRebase && (
            <span className="flex items-center gap-0.5 text-orange">
              <ArrowDown className="h-2.5 w-2.5" />
              {behind}
            </span>
          )}
          {meta.mergeStatus === "conflicts" && (
            <span className="text-warning">
              <AlertTriangle className="h-2.5 w-2.5" />
            </span>
          )}
        </span>

        <div className="flex-1 min-w-2" />

        {/* Refresh — always on branch row */}
        <Button
          variant="ghost"
          size="icon-xs"
          onClick={git.handleRefreshGit}
          disabled={git.refreshingGit}
          className="text-muted-foreground-dim hover:text-foreground"
        >
          <RefreshCw className={cn("h-3 w-3", git.refreshingGit && "animate-spin")} />
        </Button>
      </div>

      {/* Action buttons row */}
      {!isBusy && !isMerged && (hasUncommitted || (isWorktree && ahead > 0) || canRebase) && (
        <div className={cn("flex items-center gap-1.5 px-3 pb-2.5 -mt-1", isMobile && "flex-wrap")}>
          {actionButtons}
        </div>
      )}
    </div>
  );
}
