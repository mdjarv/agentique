import { useMemo } from "react";
import type { SessionState } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";

export interface ActiveSessionRef {
  sessionId: string;
  state: SessionState;
  projectId: string;
  hasPending: boolean;
}

export function useProfileActiveSession(profileId: string): ActiveSessionRef | null {
  const key = useChatStore((s) => {
    for (const [id, data] of Object.entries(s.sessions)) {
      if (data.meta.agentProfileId === profileId && !data.meta.completedAt) {
        const pending = !!(data.meta.pendingApproval || data.meta.pendingQuestion);
        return `${id}|${data.meta.state}|${data.meta.projectId}|${pending}`;
      }
    }
    return null;
  });

  return useMemo(() => {
    if (!key) return null;
    const parts = key.split("|");
    return {
      sessionId: parts[0] ?? "",
      state: (parts[1] ?? "idle") as SessionState,
      projectId: parts[2] ?? "",
      hasPending: parts[3] === "true",
    };
  }, [key]);
}

const STATUS_CLASSES: Record<string, string> = {
  running: "bg-emerald-500 animate-pulse",
  idle: "bg-yellow-500",
  pending: "bg-orange-500 animate-pulse",
  merging: "bg-blue-500",
};

export function AgentStatusDot({ profileId }: { profileId: string }) {
  const active = useProfileActiveSession(profileId);
  if (!active) return null;
  const key = active.hasPending ? "pending" : active.state;
  const cls = STATUS_CLASSES[key] ?? "bg-muted-foreground/40";
  return <span className={`inline-block size-2 rounded-full shrink-0 ${cls}`} />;
}
