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

export function extractTodosFromTurns(turns: Turn[]): TodoItem[] | null {
  for (let i = turns.length - 1; i >= 0; i--) {
    const events = turns[i]?.events;
    if (!events) continue;
    for (let j = events.length - 1; j >= 0; j--) {
      const event = events[j];
      if (!event) continue;
      const todos = extractTodosFromEvent(event);
      if (todos) return todos;
    }
  }
  return null;
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
