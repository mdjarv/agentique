import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  CheckCircle2,
  GitBranch,
  GitPullRequest,
} from "lucide-react";
import { cn } from "~/lib/utils";
import type { SessionState } from "~/stores/chat-store";
import { SessionStatusBadge } from "./SessionStatusBadge";

interface SessionRowProps {
  name: string;
  state: SessionState;
  connected?: boolean;
  hasUnseenCompletion?: boolean;
  hasPendingApproval?: boolean;
  isPlanning?: boolean;
  isActive: boolean;
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
  onClick: () => void;
}

const isTerminal = (state: SessionState) =>
  state === "done" || state === "stopped" || state === "failed";

export function SessionRow({
  name,
  state,
  connected,
  hasUnseenCompletion,
  hasPendingApproval,
  isPlanning,
  isActive,
  worktreeMerged,
  commitsAhead,
  commitsBehind,
  branchMissing,
  hasUncommitted,
  hasDirtyWorktree,
  mergeStatus,
  gitOperation,
  prUrl,
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
        hasUnseenCompletion={hasUnseenCompletion}
        hasPendingApproval={hasPendingApproval}
        isPlanning={isPlanning}
        gitOperation={gitOperation}
      />
      <span
        className={cn(
          "truncate text-sidebar-foreground",
          !name && "italic text-muted-foreground",
          faded && "text-muted-foreground line-through decoration-muted-foreground/50",
          hasAttention && "text-[#e0af68]",
        )}
        title={worktreeBranch ? `${name || "Untitled"}\n${worktreeBranch}` : name || "Untitled"}
      >
        {name || "Untitled"}
      </span>
      <span className="ml-auto flex shrink-0 items-center">
        <GitStatus
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
}

function GitStatus({
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
    return <span className="text-xs text-emerald-500/70">merged</span>;
  }
  if (branchMissing) {
    return <span className="text-xs text-[#f7768e]/80">missing</span>;
  }

  const hasPr = !!prUrl;
  const ahead = !!commitsAhead && commitsAhead > 0;
  const behind = !!commitsBehind && commitsBehind > 0;
  const dirty = hasUncommitted || hasDirtyWorktree;
  const hasConflicts = mergeStatus === "conflicts";
  const readyToMerge = mergeStatus === "clean" && ahead;
  if (!ahead && !dirty && !hasPr && !hasConflicts) return null;

  return (
    <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
      {hasConflicts && <AlertTriangle className="size-3 text-amber-500/80" />}
      {!hasConflicts && readyToMerge && <CheckCircle2 className="size-3 text-[#9ece6a]/70" />}
      {hasPr && <GitPullRequest className="size-3 text-[#7aa2f7]" />}
      {ahead && (
        <span className="flex items-center gap-0.5">
          <ArrowUp className="size-2.5" />
          {commitsAhead}
        </span>
      )}
      {behind && (
        <span className="flex items-center gap-0.5 text-[#7aa2f7]/80">
          <ArrowDown className="size-2.5" />
          {commitsBehind}
        </span>
      )}
      {dirty && <GitBranch className="size-3 text-[#e0af68]/80" />}
    </span>
  );
}
