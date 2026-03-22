import { GitBranch, Plus, X } from "lucide-react";
import { useShallow } from "zustand/shallow";
import { Badge } from "~/components/ui/badge";
import { cn } from "~/lib/utils";
import { useChatStore } from "~/stores/chat-store";

interface SessionTabsProps {
  onCreateSession: () => void;
  onStopSession: (sessionId: string) => void;
}

export function SessionTabs({ onCreateSession, onStopSession }: SessionTabsProps) {
  const sessionIds = useChatStore(useShallow((s) => Object.keys(s.sessions)));
  const sessions = useChatStore((s) => s.sessions);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const setActiveSessionId = useChatStore((s) => s.setActiveSessionId);

  const badgeVariant = (state: string) => (state === "running" ? "outline" : "secondary");

  const badgeClass = (state: string) =>
    state === "running"
      ? "border-yellow-500 text-yellow-600"
      : state === "failed"
        ? "border-red-500 text-red-500"
        : "";

  return (
    <div className="border-b flex items-center gap-1 p-2 overflow-x-auto">
      {sessionIds.map((id) => {
        const session = sessions[id]?.meta;
        if (!session) return null;
        return (
          <div
            key={id}
            className={cn(
              "flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm shrink-0 cursor-pointer group",
              id === activeSessionId ? "bg-accent" : "hover:bg-accent/50",
            )}
            onClick={() => setActiveSessionId(id)}
            onKeyDown={(e) => e.key === "Enter" && setActiveSessionId(id)}
            role="tab"
            tabIndex={0}
            aria-selected={id === activeSessionId}
          >
            {session.worktreeBranch && <GitBranch className="h-3 w-3 text-muted-foreground" />}
            <span className="truncate max-w-32">{session.name}</span>
            <Badge
              variant={badgeVariant(session.state)}
              className={cn("text-xs", badgeClass(session.state))}
            >
              {session.state}
            </Badge>
            <button
              type="button"
              aria-label="Stop session"
              onClick={(e) => {
                e.stopPropagation();
                onStopSession(id);
              }}
              className="opacity-0 group-hover:opacity-100 p-0.5 rounded hover:bg-destructive hover:text-destructive-foreground transition-opacity"
            >
              <X className="h-3 w-3" />
            </button>
          </div>
        );
      })}
      <button
        type="button"
        aria-label="New session"
        onClick={onCreateSession}
        className="shrink-0 rounded-md p-1.5 hover:bg-accent transition-colors"
      >
        <Plus className="h-4 w-4" />
      </button>
    </div>
  );
}
