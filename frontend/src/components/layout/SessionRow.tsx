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
  onClick: () => void;
  onStop: (e: React.MouseEvent) => void;
  onDelete: (e: React.MouseEvent) => void;
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
  onClick,
  onStop,
  onDelete,
}: SessionRowProps) {
  const canStop = state !== "stopped" && state !== "done" && state !== "draft";

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
        <span className="truncate" title={name}>
          {name}
        </span>
        {worktreeBranch && (
          <span className="flex items-center gap-0.5 text-xs text-muted-foreground shrink-0 max-w-[8rem]">
            <GitBranch
              className={cn("h-3 w-3 shrink-0", hasDirtyWorktree && "text-yellow-600/70")}
            />
            <span className="truncate" title={worktreeBranch}>
              {worktreeBranch}
            </span>
          </span>
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
