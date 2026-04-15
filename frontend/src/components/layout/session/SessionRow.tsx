import { forwardRef, memo, type Ref } from "react";
import { ProjectPill } from "~/components/ui/project-pill";
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
  isDragging?: boolean;
  time?: string;
  projectSlug?: string;
  agentProfileName?: string;
  agentProfileAvatar?: string;
  todoDone?: number;
  todoTotal?: number;
  onClick: () => void;
}

type SessionRowElement = HTMLButtonElement;

const isTerminal = (state: SessionState) =>
  state === "done" || state === "stopped" || state === "failed";

function getAccentColor(isActive: boolean): string | undefined {
  if (isActive) return "border-l-primary";
  return undefined;
}

export const SessionRow = memo(
  forwardRef(function SessionRow(
    {
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
      isDragging,
      time,
      projectSlug,
      agentProfileName,
      agentProfileAvatar,
      todoDone = 0,
      todoTotal = 0,
      onClick,
      ...rest
    }: SessionRowProps & React.HTMLAttributes<SessionRowElement>,
    ref: Ref<SessionRowElement>,
  ) {
    const faded = isTerminal(state) && worktreeMerged;
    const hasAttention = !worktreeMerged && isTerminal(state) && !!commitsAhead && commitsAhead > 0;
    const hasMeta = !!(time || projectSlug || agentProfileName);
    const hasSubContent = hasMeta || todoTotal > 0;

    const accentColor = getAccentColor(isActive);

    return (
      <button
        ref={ref}
        type="button"
        onClick={onClick}
        className={cn(
          "flex gap-1.5 px-2 py-1.5 max-md:py-2.5 text-sm",
          "hover:bg-sidebar-accent/50 transition-colors cursor-pointer text-left w-full",
          hasSubContent ? "items-start" : "items-center",
          accentColor ? "border-l-2 rounded-r-md" : "rounded-md",
          isActive ? "bg-sidebar-accent" : accentColor && "bg-sidebar-accent/30",
          accentColor,
          isDragging && "opacity-50",
        )}
        {...rest}
      >
        <span className={hasSubContent ? "mt-0.5" : undefined}>
          <SessionStatusBadge
            state={state}
            connected={connected}
            hasUnseenCompletion={hasUnseenCompletion}
            hasPendingApproval={hasPendingApproval}
            isPlanning={isPlanning}
            gitOperation={gitOperation}
          />
        </span>
        <div className="min-w-0 flex-1">
          <span
            className={cn(
              "block truncate text-sidebar-foreground",
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
          {hasMeta && (
            <span className="flex items-center gap-1 text-[10px] text-muted-foreground-faint">
              {projectSlug && (
                <ProjectPill slug={projectSlug} background={false} className="truncate" />
              )}
              {agentProfileName && (
                <span className="truncate shrink-0">
                  {agentProfileAvatar && <span className="mr-0.5">{agentProfileAvatar}</span>}
                  {agentProfileName}
                </span>
              )}
              {time && (
                <span className="shrink-0 tabular-nums text-muted-foreground-faint ml-auto">
                  {time}
                </span>
              )}
            </span>
          )}
          {todoTotal > 0 && (
            <div className="mt-1 flex items-center gap-1.5">
              <div className="h-1 flex-1 rounded-full bg-muted/50 overflow-hidden">
                <div
                  className={cn(
                    "h-full rounded-full transition-all duration-300",
                    todoDone === todoTotal ? "bg-emerald-500/70" : "bg-primary/40",
                  )}
                  style={{ width: `${(todoDone / todoTotal) * 100}%` }}
                />
              </div>
              <span className="text-[9px] tabular-nums text-muted-foreground-faint shrink-0">
                {todoDone}/{todoTotal}
              </span>
            </div>
          )}
        </div>
      </button>
    );
  }),
);
