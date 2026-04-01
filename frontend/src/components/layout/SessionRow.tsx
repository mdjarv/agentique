import { memo } from "react";
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
  hasDraft?: boolean;
  worktreeMerged?: boolean;
  commitsAhead?: number;
  gitOperation?: string;
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
  isPlanning,
  isActive,
  hasDraft,
  worktreeMerged,
  commitsAhead,
  gitOperation,
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
        title={name || "Untitled"}
      >
        {name || "Untitled"}
      </span>
    </div>
  );
});
