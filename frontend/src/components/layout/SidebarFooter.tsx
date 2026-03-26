import { useMemo } from "react";
import { cn } from "~/lib/utils";
import { useChatStore } from "~/stores/chat-store";
import { ConnectionIndicator } from "./ConnectionIndicator";

type DisplayState =
  | "approval"
  | "unseen"
  | "running"
  | "merging"
  | "idle"
  | "done"
  | "stopped"
  | "failed";

const stateConfig: Record<DisplayState, { color: string; pulse?: boolean; label: string }> = {
  approval: { color: "bg-[#ff9e64]", pulse: true, label: "waiting for approval" },
  unseen: { color: "bg-[#e0af68]", label: "new response" },
  running: { color: "bg-[#73daca]", label: "running" },
  merging: { color: "bg-[#7aa2f7]", label: "merging" },
  idle: { color: "bg-[#9ece6a]", label: "idle" },
  done: { color: "bg-[#7dcfff]", label: "done" },
  stopped: { color: "bg-[#a9b1d6]", label: "stopped" },
  failed: { color: "bg-[#f7768e]", label: "failed" },
};

const displayOrder: DisplayState[] = [
  "approval",
  "unseen",
  "running",
  "merging",
  "idle",
  "done",
  "stopped",
  "failed",
];

export function SidebarFooter() {
  const sessions = useChatStore((s) => s.sessions);

  const counts = useMemo(() => {
    const c: Record<DisplayState, number> = {
      approval: 0,
      unseen: 0,
      running: 0,
      merging: 0,
      idle: 0,
      done: 0,
      stopped: 0,
      failed: 0,
    };

    for (const session of Object.values(sessions)) {
      if (session.meta.worktreeMerged) continue;

      if (session.pendingApproval || session.pendingQuestion) {
        c.approval++;
      } else if (session.hasUnseenCompletion && session.meta.state === "idle") {
        c.unseen++;
      } else {
        const state = session.meta.state as DisplayState;
        if (state in c) c[state]++;
      }
    }

    return c;
  }, [sessions]);

  const hasAny = displayOrder.some((s) => counts[s] > 0);

  return (
    <div className="px-3 py-2 border-t border-sidebar-border">
      {hasAny && (
        <div className="flex items-center gap-3">
          {displayOrder.map((state) => {
            const count = counts[state];
            if (count === 0) return null;
            const cfg = stateConfig[state];
            return (
              <span
                key={state}
                className="flex items-center gap-1 text-xs"
                title={`${count} ${cfg.label}`}
              >
                <span
                  className={cn(
                    "inline-block size-2 rounded-full",
                    cfg.color,
                    cfg.pulse && "animate-pulse",
                  )}
                />
                <span className="text-muted-foreground">{count}</span>
              </span>
            );
          })}
        </div>
      )}
      <ConnectionIndicator />
    </div>
  );
}
