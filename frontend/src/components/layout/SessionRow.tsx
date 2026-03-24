import { ArrowUp, GitBranch, Square, Trash2 } from "lucide-react";
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
  branchMissing,
  hasUncommitted,
  onClick,
  onStop,
  onDelete,
}: SessionRowProps) {
  const canStop = !isTerminal(state) && state !== "draft";
  const faded = isTerminal(state) && worktreeMerged;
  const hasAttention = !worktreeMerged && isTerminal(state) && !!commitsAhead && commitsAhead > 0;

  return (
    <div
      className={cn(
        "flex items-center gap-1.5 rounded-md px-2 py-1 text-sm group/session hover:bg-accent/50 transition-colors",
        isActive && "bg-accent/70",
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
            "truncate",
            faded && "text-muted-foreground/40 line-through decoration-muted-foreground/20",
            hasAttention && "text-[#e0af68]/90",
          )}
          title={name}
        >
          {name}
        </span>

        {/* Right-aligned decorations */}
        <span className="ml-auto flex items-center gap-1 shrink-0">
          {/* Commits ahead indicator */}
          {!!commitsAhead && commitsAhead > 0 && !worktreeMerged && (
            <span
              className={cn(
                "flex items-center gap-0.5 text-[10px] font-medium",
                hasUncommitted ? "text-[#e0af68]/70" : "text-muted-foreground/60",
              )}
              title={`${commitsAhead} commit${commitsAhead > 1 ? "s" : ""} ahead${hasUncommitted ? ", uncommitted changes" : ""}`}
            >
              <ArrowUp className="size-2.5" />
              {commitsAhead}
            </span>
          )}
          {/* Branch indicator */}
          {worktreeBranch && (
            <span
              title={
                worktreeMerged
                  ? `${worktreeBranch} (merged)`
                  : branchMissing
                    ? `${worktreeBranch} (missing)`
                    : hasDirtyWorktree || hasUncommitted
                      ? `${worktreeBranch} (dirty)`
                      : worktreeBranch
              }
            >
              <GitBranch
                className={cn(
                  "size-3",
                  worktreeMerged
                    ? "text-emerald-500/40"
                    : hasDirtyWorktree || hasUncommitted
                      ? "text-[#e0af68]/60"
                      : branchMissing
                        ? "text-[#f7768e]/50"
                        : "text-muted-foreground/40",
                )}
              />
            </span>
          )}
        </span>
      </button>
      {canStop && (
        <button
          type="button"
          aria-label="Stop session"
          onClick={onStop}
          className="opacity-0 group-hover/session:opacity-100 p-0.5 rounded hover:bg-destructive hover:text-destructive-foreground transition-opacity shrink-0"
        >
          <Square className="h-3 w-3" />
        </button>
      )}
      <button
        type="button"
        aria-label="Delete session"
        onClick={onDelete}
        className="opacity-0 group-hover/session:opacity-100 p-0.5 rounded hover:bg-destructive hover:text-destructive-foreground transition-opacity shrink-0"
      >
        <Trash2 className="h-3 w-3" />
      </button>
    </div>
  );
}
