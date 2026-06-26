import type { ChatEvent, ContextUsage, TodoItem, Turn } from "~/stores/chat-types";

export function parseTodoItems(input: unknown): TodoItem[] | null {
  if (!input || typeof input !== "object") return null;
  const obj = input as Record<string, unknown>;
  if (!Array.isArray(obj.todos)) return null;
  const items: TodoItem[] = [];
  for (const item of obj.todos) {
    if (!item || typeof item !== "object") continue;
    const t = item as Record<string, unknown>;
    if (typeof t.content !== "string" || typeof t.status !== "string") continue;
    items.push({
      content: t.content,
      activeForm: typeof t.activeForm === "string" ? t.activeForm : undefined,
      status: t.status as TodoItem["status"],
    });
  }
  return items.length > 0 ? items : null;
}

export function extractTodosFromEvent(event: ChatEvent): TodoItem[] | null {
  if (event.type !== "tool_use" || event.toolName !== "TodoWrite") return null;
  return parseTodoItems(event.toolInput);
}

// Claude Code's task tools (TaskCreate / TaskUpdate) superseded the single-snapshot
// TodoWrite tool with an incremental, mutable task store. Unlike TodoWrite — where one
// tool_use carried the whole list — the task list has to be reconstructed by folding the
// create/update stream. A task's id is NOT in the TaskCreate input; the harness assigns it
// and returns it in the tool_result text ("Task #1 created successfully: …"), which later
// TaskUpdate calls reference by id.
const TASK_LIST_TOOLS = new Set(["TodoWrite", "TaskCreate", "TaskUpdate"]);
const CREATED_TASK_RE = /task #(\d+) created/i;
const TASK_STATUSES = new Set<TodoItem["status"]>(["pending", "in_progress", "completed"]);

/** True when an event mutates the task/todo list and should trigger a recompute. */
export function isTaskListEvent(event: ChatEvent): boolean {
  return event.type === "tool_use" && TASK_LIST_TOOLS.has(event.toolName);
}

/** The task id a TaskCreate's tool_result announces, or null for any other result. */
function createdTaskId(event: ChatEvent): string | null {
  if (event.type !== "tool_result" || !event.contentBlocks) return null;
  for (const block of event.contentBlocks) {
    // The id sits at the very start ("Task #N created…"); cap the scan so large
    // unrelated tool outputs don't get walked end-to-end.
    const match =
      typeof block?.text === "string" ? block.text.slice(0, 200).match(CREATED_TASK_RE) : null;
    if (match?.[1]) return match[1];
  }
  return null;
}

interface TaskRecord {
  content: string;
  activeForm?: string;
  status: TodoItem["status"];
  deleted: boolean;
}

/**
 * Reconstruct the current task list by folding a forward-ordered event stream.
 * Handles both the legacy TodoWrite snapshot and the incremental TaskCreate/TaskUpdate
 * tools. A session uses one mechanism or the other; if both appear the incremental store
 * wins (it is the newer tool) and the last TodoWrite snapshot is the fallback.
 */
export function foldTaskList(events: ChatEvent[]): TodoItem[] | null {
  const order: TaskRecord[] = [];
  const byToolId = new Map<string, TaskRecord>(); // create's toolId → record (before its id is known)
  const byId = new Map<string, TaskRecord>(); // assigned task id → record
  let legacy: TodoItem[] | null = null;

  for (const event of events) {
    if (event.type === "tool_result") {
      const id = createdTaskId(event);
      const rec = id ? byToolId.get(event.toolId) : undefined;
      if (id && rec) byId.set(id, rec);
      continue;
    }
    if (event.type !== "tool_use") continue;

    if (event.toolName === "TodoWrite") {
      legacy = parseTodoItems(event.toolInput);
      continue;
    }

    const input = (event.toolInput ?? {}) as Record<string, unknown>;

    if (event.toolName === "TaskCreate") {
      const subject = typeof input.subject === "string" ? input.subject : "";
      if (!subject) continue;
      const rec: TaskRecord = {
        content: subject,
        activeForm: typeof input.activeForm === "string" ? input.activeForm : undefined,
        status: "pending",
        deleted: false,
      };
      byToolId.set(event.toolId, rec);
      order.push(rec);
      continue;
    }

    if (event.toolName === "TaskUpdate") {
      const taskId = typeof input.taskId === "string" ? input.taskId : null;
      const rec = taskId ? byId.get(taskId) : undefined;
      if (!rec) continue; // update for a create we never saw (e.g. evicted/older turn)
      if (typeof input.subject === "string") rec.content = input.subject;
      if (typeof input.activeForm === "string") rec.activeForm = input.activeForm;
      const status = input.status;
      if (status === "deleted") rec.deleted = true;
      else if (typeof status === "string" && TASK_STATUSES.has(status as TodoItem["status"]))
        rec.status = status as TodoItem["status"];
    }
  }

  const items = order
    .filter((r) => !r.deleted)
    .map((r) => ({ content: r.content, activeForm: r.activeForm, status: r.status }));
  return items.length > 0 ? items : legacy;
}

export function extractTodosFromTurns(turns: Turn[]): TodoItem[] | null {
  const events: ChatEvent[] = [];
  for (const t of turns) {
    if (t?.events) events.push(...t.events);
  }
  return foldTaskList(events);
}

export function extractContextUsageFromTurns(turns: Turn[]): ContextUsage | null {
  for (let i = turns.length - 1; i >= 0; i--) {
    const events = turns[i]?.events;
    if (!events) continue;
    for (let j = events.length - 1; j >= 0; j--) {
      const event = events[j];
      if (event?.type === "compact_boundary") return null;
      if (event?.type === "result" && event.contextWindow && event.contextWindow > 0) {
        return {
          contextWindow: event.contextWindow,
          inputTokens: event.inputTokens ?? 0,
          outputTokens: event.outputTokens ?? 0,
        };
      }
    }
  }
  return null;
}
