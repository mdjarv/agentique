import { ArrowDown, ArrowUp, GitBranch, Square, Trash2 } from "lucide-react";
import { cn } from "~/lib/utils";
import type { SessionState } from "~/stores/chat-store";
import { SessionStatusBadge } from "./SessionStatusBadge";

interface SessionRowProps {
  name: string;
  state: SessionState;
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
  onClick: () => void;
  onStop: (e: React.MouseEvent) => void;
  onDelete: (e: React.MouseEvent) => void;
}

const isTerminal = (state: SessionState) =>
  state === "done" || state === "stopped" || state === "failed";

export function SessionRow({
  name,
  state,
  hasUnseenCompletion,
  hasPendingApproval,
  isPlanning,
  isActive,
  worktreeBranch,
  hasDirtyWorktree,
  worktreeMerged,
  commitsAhead,
  commitsBehind,
  branchMissing,
  hasUncommitted,
  onClick,
  onStop,
  onDelete,
}: SessionRowProps) {
  const canStop = !isTerminal(state);
  const faded = isTerminal(state) && worktreeMerged;
  const hasAttention = !worktreeMerged && isTerminal(state) && !!commitsAhead && commitsAhead > 0;

  return (
    <div
      className={cn(
        "flex items-center gap-1.5 rounded-md px-2 py-1.5 text-sm group/session hover:bg-sidebar-accent/50 transition-colors",
        isActive && "bg-sidebar-accent/70",
      )}
    >
      <button
        type="button"
        className="flex items-center gap-1.5 flex-1 min-w-0 cursor-pointer bg-transparent border-0 p-0 text-left text-inherit"
        onClick={onClick}
      >
        <SessionStatusBadge
          state={state}
          hasUnseenCompletion={hasUnseenCompletion}
          hasPendingApproval={hasPendingApproval}
          isPlanning={isPlanning}
        />
        <span
          className={cn(
            "truncate text-sidebar-foreground",
            faded && "text-muted-foreground line-through decoration-muted-foreground/50",
            hasAttention && "text-[#e0af68]",
          )}
          title={worktreeBranch ? `${name}\n${worktreeBranch}` : name}
        >
          {name}
        </span>
      </button>
      {/* Right slot: git status by default, action buttons on hover */}
      <span className="relative ml-auto flex shrink-0 items-center">
        <span className="flex items-center gap-1.5 group-hover/session:invisible">
          <GitStatus
            worktreeMerged={worktreeMerged}
            branchMissing={branchMissing}
            commitsAhead={commitsAhead}
            commitsBehind={commitsBehind}
            hasUncommitted={hasUncommitted}
            hasDirtyWorktree={hasDirtyWorktree}
          />
        </span>
        <span className="absolute right-0 flex items-center gap-0.5 invisible group-hover/session:visible">
          {canStop && (
            <button
              type="button"
              aria-label="Stop session"
              onClick={onStop}
              className="p-0.5 rounded hover:bg-destructive hover:text-destructive-foreground transition-colors shrink-0"
            >
              <Square className="h-3 w-3" />
            </button>
          )}
          <button
            type="button"
            aria-label="Delete session"
            onClick={onDelete}
            className="p-0.5 rounded hover:bg-destructive hover:text-destructive-foreground transition-colors shrink-0"
          >
            <Trash2 className="h-3 w-3" />
          </button>
        </span>
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
}: {
  worktreeMerged?: boolean;
  branchMissing?: boolean;
  commitsAhead?: number;
  commitsBehind?: number;
  hasUncommitted?: boolean;
  hasDirtyWorktree?: boolean;
}) {
  if (worktreeMerged) {
    return <span className="text-xs text-emerald-500/70">merged</span>;
  }
  if (branchMissing) {
    return <span className="text-xs text-[#f7768e]/80">missing</span>;
  }

  const ahead = !!commitsAhead && commitsAhead > 0;
  const behind = !!commitsBehind && commitsBehind > 0;
  const dirty = hasUncommitted || hasDirtyWorktree;
  if (!ahead && !behind && !dirty) return null;

  return (
    <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
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
