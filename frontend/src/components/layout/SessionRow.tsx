import { AlertTriangle, CheckCircle2, GitPullRequest, Users } from "lucide-react";
import { memo } from "react";
import { cn } from "~/lib/utils";
import type { SessionState } from "~/stores/chat-store";
import { GitIndicators } from "./GitIndicators";
import { SessionStatusBadge } from "./SessionStatusBadge";

interface SessionRowProps {
  name: string;
  state: SessionState;
  connected?: boolean;
  hasUnseenCompletion?: boolean;
  hasPendingApproval?: boolean;
  isCompacting?: boolean;
  isPlanning?: boolean;
  isActive: boolean;
  hasDraft?: boolean;
  worktreeBranch?: string;
  hasDirtyWorktree?: boolean;
  worktreeMerged?: boolean;
  commitsAhead?: number;
  commitsBehind?: number;
  branchMissing?: boolean;
  hasUncommitted?: boolean;
  mergeStatus?: "clean" | "conflicts" | "unknown";
  gitOperation?: string;
  prUrl?: string;
  teamId?: string;
  hasUnreadTeamMessage?: boolean;
  onClick: () => void;
}

const isTerminal = (state: SessionState) =>
  state === "done" || state === "stopped" || state === "failed";

export const SessionRow = memo(function SessionRow({
  name,
  state,
  connected,
  hasUnseenCompletion,
  hasPendingApproval,
  isCompacting,
  isPlanning,
  isActive,
  hasDraft,
  worktreeMerged,
  commitsAhead,
  commitsBehind,
  branchMissing,
  hasUncommitted,
  hasDirtyWorktree,
  mergeStatus,
  gitOperation,
  prUrl,
  teamId,
  hasUnreadTeamMessage,
  worktreeBranch,
  onClick,
}: SessionRowProps) {
  const faded = isTerminal(state) && worktreeMerged;
  const hasAttention = !worktreeMerged && isTerminal(state) && !!commitsAhead && commitsAhead > 0;

  return (
    // biome-ignore lint/a11y/useSemanticElements: div with role=button avoids nested button HTML issues with action buttons
    <div
      role="button"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onClick();
        }
      }}
      className={cn(
        "flex items-center gap-1.5 rounded-md px-2 py-1.5 max-md:py-2.5 text-sm hover:bg-sidebar-accent/50 transition-colors cursor-pointer",
        isActive && "bg-sidebar-accent/70",
      )}
    >
      <SessionStatusBadge
        state={state}
        connected={connected}
        hasPendingApproval={hasPendingApproval}
        isCompacting={isCompacting}
        isPlanning={isPlanning}
        gitOperation={gitOperation}
      />
      <span
        className={cn(
          "truncate text-sidebar-foreground",
          !name && "italic text-muted-foreground",
          hasDraft && name && "italic",
          faded && "text-muted-foreground line-through decoration-muted-foreground/50",
          hasAttention && "text-warning",
          hasUnseenCompletion && "font-semibold text-foreground-bright",
        )}
        title={worktreeBranch ? `${name || "Untitled"}\n${worktreeBranch}` : name || "Untitled"}
      >
        {name || "Untitled"}
      </span>
      {teamId && (
        <Users
          className={cn(
            "size-3 shrink-0",
            hasUnreadTeamMessage ? "text-warning" : "text-muted-foreground/60",
          )}
        />
      )}
      <span className="ml-auto flex shrink-0 items-center">
        <SessionGitStatus
          worktreeMerged={worktreeMerged}
          branchMissing={branchMissing}
          commitsAhead={commitsAhead}
          commitsBehind={commitsBehind}
          hasUncommitted={hasUncommitted}
          hasDirtyWorktree={hasDirtyWorktree}
          mergeStatus={mergeStatus}
          prUrl={prUrl}
        />
      </span>
    </div>
  );
});

function SessionGitStatus({
  worktreeMerged,
  branchMissing,
  commitsAhead,
  commitsBehind,
  hasUncommitted,
  hasDirtyWorktree,
  mergeStatus,
  prUrl,
}: {
  worktreeMerged?: boolean;
  branchMissing?: boolean;
  commitsAhead?: number;
  commitsBehind?: number;
  hasUncommitted?: boolean;
  hasDirtyWorktree?: boolean;
  mergeStatus?: "clean" | "conflicts" | "unknown";
  prUrl?: string;
}) {
  if (worktreeMerged && !(commitsAhead && commitsAhead > 0)) {
    return <span className="text-xs text-success/70">merged</span>;
  }
  if (branchMissing) {
    return <span className="text-xs text-destructive/80">missing</span>;
  }

  const hasPr = !!prUrl;
  const ahead = !!commitsAhead && commitsAhead > 0;
  const dirty = hasUncommitted || hasDirtyWorktree;
  const hasConflicts = mergeStatus === "conflicts";
  const readyToMerge = !worktreeMerged && mergeStatus === "clean" && ahead && !branchMissing;
  if (!ahead && !dirty && !hasPr && !hasConflicts) return null;

  return (
    <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
      {hasConflicts && <AlertTriangle className="size-3 text-warning/80" />}
      {!hasConflicts && readyToMerge && <CheckCircle2 className="size-3 text-success/70" />}
      {hasPr && <GitPullRequest className="size-3 text-primary" />}
      <GitIndicators dirty={dirty} aheadCount={commitsAhead} behindCount={commitsBehind} />
    </span>
  );
}
