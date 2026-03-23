import { Square, Trash2 } from "lucide-react";
import { cn } from "~/lib/utils";
import type { SessionState } from "~/stores/chat-store";
import { SessionStatusBadge } from "./SessionStatusBadge";

interface SessionRowProps {
  name: string;
  state: SessionState;
  hasUnseenCompletion?: boolean;
  isActive: boolean;
  onClick: () => void;
  onStop: (e: React.MouseEvent) => void;
  onDelete: (e: React.MouseEvent) => void;
}

export function SessionRow({
  name,
  state,
  hasUnseenCompletion,
  isActive,
  onClick,
  onStop,
  onDelete,
}: SessionRowProps) {
  const canStop = state !== "stopped" && state !== "done";

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
        <SessionStatusBadge state={state} hasUnseenCompletion={hasUnseenCompletion} />
        <span className="truncate" title={name}>
          {name}
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
