import type { ChatEvent, TaskEvent, Turn } from "~/stores/chat-types";

/**
 * Collect the task events of the most-recent dynamic workflow in a session,
 * grouped by the parent `Workflow` tool-use id. Returns `[]` when the session
 * has no workflow. Used both to feed the right-panel `WorkflowActivity` view and
 * to gate the header toggle's visibility.
 *
 * Callers should `useMemo` this over the session's `turns` + `streamingEvents`
 * (both referentially stable between store updates) — never call it inside a
 * Zustand selector, which would return a fresh array every render.
 */
export function collectLatestWorkflow(
  turns: Turn[] | undefined,
  streamingEvents: ChatEvent[] | undefined,
): TaskEvent[] {
  const byToolUse = new Map<string, TaskEvent[]>();
  const order: string[] = [];

  const consider = (ev: ChatEvent) => {
    if (ev.type !== "task" || !ev.toolUseId) return;
    let list = byToolUse.get(ev.toolUseId);
    if (!list) {
      list = [];
      byToolUse.set(ev.toolUseId, list);
      order.push(ev.toolUseId);
    }
    list.push(ev);
  };

  for (const t of turns ?? []) for (const ev of t.events) consider(ev);
  for (const ev of streamingEvents ?? []) consider(ev);

  // Walk newest-first; return the latest group that is a workflow.
  for (let i = order.length - 1; i >= 0; i--) {
    const id = order[i];
    const list = id ? byToolUse.get(id) : undefined;
    if (list?.some((e) => e.taskType === "local_workflow")) return list;
  }
  return [];
}

/** The parent tool-use id of the most-recent workflow, or null. */
export function latestWorkflowToolUseId(
  turns: Turn[] | undefined,
  streamingEvents: ChatEvent[] | undefined,
): string | null {
  return collectLatestWorkflow(turns, streamingEvents)[0]?.toolUseId ?? null;
}
