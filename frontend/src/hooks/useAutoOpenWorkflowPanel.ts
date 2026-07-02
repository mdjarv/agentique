import { useEffect, useMemo, useRef } from "react";
import { collectLatestWorkflow } from "~/lib/workflow-events";
import { useChatStore } from "~/stores/chat-store";
import { useUIStore } from "~/stores/ui-store";

/**
 * Auto-opens the shared right panel to the workflow view when a session has a
 * **running** dynamic workflow — once per run, and only when the panel is
 * currently collapsed. Gating on "running" (no terminal notification) means
 * reopening a session with an old, completed workflow does NOT pop the panel;
 * only a live run does. Once auto-opened, we record the run id so a manual close
 * is respected (we don't re-open the same run).
 */
export function useAutoOpenWorkflowPanel(sessionId: string | null) {
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
    if (!toolUseId || !running || seenRef.current === toolUseId) return;
    seenRef.current = toolUseId;
    if (useUIStore.getState().rightPanelCollapsed) {
      useUIStore.getState().setRightPanelView("workflow");
      useUIStore.getState().setRightPanelCollapsed(false);
    }
  }, [toolUseId, running]);
}
