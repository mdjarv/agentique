import { GitBranch, Square, Trash2 } from "lucide-react";
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

function buildSummary(props: {
  state: SessionState;
  worktreeBranch?: string;
  worktreeMerged?: boolean;
  commitsAhead?: number;
  branchMissing?: boolean;
  hasUncommitted?: boolean;
}): { text: string; color: string } | null {
  if (!props.worktreeBranch) return null;
  if (props.state === "draft" || props.state === "running" || props.state === "starting")
    return null;

  if (props.worktreeMerged) {
    return { text: "merged", color: "text-[#9ece6a]/70" };
  }

  if (props.branchMissing) {
    return { text: "branch missing", color: "text-[#f7768e]/70" };
  }

  const parts: string[] = [];
  if (props.hasUncommitted) parts.push("uncommitted changes");
  if (props.commitsAhead && props.commitsAhead > 0) {
    parts.push(`${props.commitsAhead} commit${props.commitsAhead > 1 ? "s" : ""} ahead`);
  }

  if (parts.length > 0) {
    return { text: parts.join(" · "), color: "text-[#e0af68]/70" };
  }

  if (props.state === "idle") return null;
  return { text: "no changes", color: "text-muted-foreground/50" };
}

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
  const canStop = state !== "stopped" && state !== "done" && state !== "draft";
  const summary = buildSummary({
    state,
    worktreeBranch,
    worktreeMerged,
    commitsAhead,
    branchMissing,
    hasUncommitted,
  });

  return (
    <div
      className={cn(
        "flex items-center gap-1.5 rounded-md px-2 py-1 text-sm group/session hover:bg-accent/50 transition-colors",
        isActive && "bg-accent/70",
      )}
    >
      <button
        type="button"
        className="flex-1 min-w-0 cursor-pointer bg-transparent border-0 p-0 text-left text-inherit"
        onClick={onClick}
      >
        <div className="flex items-center gap-1.5">
          <SessionStatusBadge
            state={state}
            hasUnseenCompletion={hasUnseenCompletion}
            hasPendingApproval={hasPendingApproval}
            isPlanning={isPlanning}
          />
          <span className="truncate" title={name}>
            {name}
          </span>
          {worktreeBranch && (
            <span
              className={cn(
                "flex items-center gap-0.5 text-xs shrink-0 max-w-[8rem]",
                hasDirtyWorktree
                  ? "text-[#e0af68]/80"
                  : worktreeMerged
                    ? "text-[#9ece6a]/80"
                    : "text-muted-foreground",
              )}
              title={
                hasDirtyWorktree
                  ? `${worktreeBranch} (dirty)`
                  : worktreeMerged
                    ? `${worktreeBranch} (merged)`
                    : worktreeBranch
              }
            >
              <GitBranch className="h-3 w-3 shrink-0" />
              <span className="truncate">{worktreeBranch}</span>
            </span>
          )}
        </div>
        {summary && (
          <div className={cn("text-[10px] pl-[calc(1.25rem+0.375rem)] truncate", summary.color)}>
            {summary.text}
          </div>
        )}
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
