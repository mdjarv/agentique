import { useEffect, useMemo, useRef } from "react";
import { collectLatestWorkflow } from "~/lib/workflow-events";
import { useChatStore } from "~/stores/chat-store";
import { useUIStore } from "~/stores/ui-store";

/**
 * Pops the dock open on `Work` when a session launches a **running** dynamic
 * workflow — once per run, and only while the dock is shut.
 *
 * Gating on "running" (no terminal notification yet) is what keeps reopening an
 * old session from popping the dock at a workflow that finished last week. The
 * run id is recorded once opened, so a manual close is respected rather than
 * fought.
 */
export function useAutoOpenDock(sessionId: string | null) {
  const turns = useChatStore((s) => (sessionId ? s.sessions[sessionId]?.turns : undefined));
  const streamingEvents = useChatStore((s) =>
    sessionId ? s.sessions[sessionId]?.streamingEvents : undefined,
  );
  const seenRef = useRef<string | null>(null);

  const { toolUseId, running } = useMemo(() => {
    const events = collectLatestWorkflow(turns, streamingEvents);
    const id = events[0]?.toolUseId ?? null;
    const terminal = events.some((e) => e.taskSubtype === "task_notification" && e.taskStatus);
    return { toolUseId: id, running: id !== null && !terminal };
  }, [turns, streamingEvents]);

  useEffect(() => {
    if (!sessionId || !toolUseId || !running || seenRef.current === toolUseId) return;
    seenRef.current = toolUseId;
    const store = useUIStore.getState();
    if (!store.dock[sessionId]?.open) store.openDock(sessionId, "work");
  }, [sessionId, toolUseId, running]);
}
