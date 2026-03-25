import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  Check,
  CheckCircle2,
  GitBranch,
  GitPullRequest,
  Square,
  Trash2,
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
  isSelected?: boolean;
  showCheckbox?: boolean;
  worktreeBranch?: string;
  hasDirtyWorktree?: boolean;
  worktreeMerged?: boolean;
  commitsAhead?: number;
  commitsBehind?: number;
  branchMissing?: boolean;
  hasUncommitted?: boolean;
  mergeStatus?: "clean" | "conflicts" | "unknown";
  prUrl?: string;
  onClick: () => void;
  onStop: (e: React.MouseEvent) => void;
  onDelete: (e: React.MouseEvent) => void;
  onSelect?: (e: React.MouseEvent) => void;
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
  isSelected,
  showCheckbox,
  worktreeBranch,
  hasDirtyWorktree,
  worktreeMerged,
  commitsAhead,
  commitsBehind,
  branchMissing,
  hasUncommitted,
  mergeStatus,
  prUrl,
  onClick,
  onStop,
  onDelete,
  onSelect,
}: SessionRowProps) {
  const canStop = !isTerminal(state);
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
        "flex items-center gap-1.5 rounded-md px-2 py-1.5 text-sm group/session hover:bg-sidebar-accent/50 transition-colors cursor-pointer",
        isActive && "bg-sidebar-accent/70",
        isSelected && "bg-sidebar-accent/50",
      )}
    >
      {onSelect && (
        <button
          type="button"
          aria-label={isSelected ? "Deselect session" : "Select session"}
          onClick={(e) => {
            e.stopPropagation();
            onSelect(e);
          }}
          className={cn(
            "size-3.5 rounded-sm border shrink-0 flex items-center justify-center transition-colors",
            isSelected
              ? "bg-primary border-primary text-primary-foreground"
              : "border-muted-foreground/40 hover:border-muted-foreground",
            !showCheckbox && !isSelected && "invisible group-hover/session:visible",
          )}
        >
          {isSelected && <Check className="size-2.5" strokeWidth={3} />}
        </button>
      )}
      <SessionStatusBadge
        state={state}
        connected={connected}
        hasUnseenCompletion={hasUnseenCompletion}
        hasPendingApproval={hasPendingApproval}
        isPlanning={isPlanning}
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
            mergeStatus={mergeStatus}
            prUrl={prUrl}
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
  const hasPr = !!prUrl;

  if (worktreeMerged) {
    return <span className="text-xs text-emerald-500/70">merged</span>;
  }
  if (branchMissing) {
    return <span className="text-xs text-[#f7768e]/80">missing</span>;
  }

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
