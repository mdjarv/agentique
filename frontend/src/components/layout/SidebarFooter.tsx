import { BellDot, Check, Circle, CircleHelp, Loader, Pause, TriangleAlert } from "lucide-react";
import type { ComponentType } from "react";
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

const stateConfig: Record<
  DisplayState,
  { icon: ComponentType<{ className?: string }>; color: string; pulse?: boolean; label: string }
> = {
  approval: {
    icon: CircleHelp,
    color: "text-[#ff9e64]",
    pulse: true,
    label: "waiting for approval",
  },
  unseen: { icon: BellDot, color: "text-[#e0af68]", label: "new response" },
  running: { icon: Loader, color: "text-[#73daca]", label: "running" },
  merging: { icon: Loader, color: "text-[#7aa2f7]", label: "merging" },
  idle: { icon: Circle, color: "text-[#9ece6a]", label: "idle" },
  done: { icon: Check, color: "text-emerald-500", label: "done" },
  stopped: { icon: Pause, color: "text-[#a9b1d6]/80", label: "stopped" },
  failed: { icon: TriangleAlert, color: "text-[#f7768e]", label: "failed" },
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
            const Icon = cfg.icon;
            return (
              <span
                key={state}
                className={cn("flex items-center gap-1 text-xs", cfg.color)}
                title={`${count} ${cfg.label}`}
              >
                <Icon
                  className={cn(
                    "size-3 shrink-0",
                    cfg.pulse && "animate-pulse",
                    (state === "running" || state === "merging") && "animate-spin",
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
