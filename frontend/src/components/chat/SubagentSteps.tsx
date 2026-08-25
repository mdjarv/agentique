/**
 * A subagent's forwarded narration, as compact transcript lines.
 *
 * Shared by the two places a subagent's own work is readable: nested under the
 * Task block in the transcript, and behind an opened row in the Agents roster.
 * One renderer, because a step that reads as thinking in one place must not read
 * as text in the other.
 *
 * These events exist only when the provider forwards subagent output — Claude
 * with `[claude] forward-subagent-text`. Without it the CLI emits no subagent
 * messages at all and every list here is empty.
 */
import { memo } from "react";
import { ToolIcon } from "~/components/chat/ToolIcons";
import type { ChatEvent } from "~/stores/chat-store";

/**
 * The forwarded events that read as a step. A subagent's own `tool_result` is
 * deliberately excluded: it belongs to its `tool_use` row, and on its own it is
 * a wall of output with nothing naming it.
 */
export function subagentSteps(events: readonly ChatEvent[] | undefined): ChatEvent[] {
  return (events ?? []).filter(
    (e) => e.type === "text" || e.type === "thinking" || e.type === "tool_use",
  );
}

/**
 * How many lines render at once. A single agent can spend thousands of steps,
 * and the tail is the part a reader wants — so the head is dropped, and said to
 * be dropped rather than silently trimmed.
 */
export const MAX_RENDERED_STEPS = 300;

/** One forwarded subagent event, rendered as a compact transcript line. */
export const SubagentStepLine = memo(function SubagentStepLine({ event }: { event: ChatEvent }) {
  if (event.type === "text" || event.type === "thinking") {
    return (
      <div
        className={
          event.type === "thinking"
            ? "whitespace-pre-wrap text-muted-foreground-faint italic"
            : "whitespace-pre-wrap text-muted-foreground-dim"
        }
      >
        {event.content}
      </div>
    );
  }
  if (event.type === "tool_use") {
    return (
      <div className="flex items-center gap-1.5 text-muted-foreground-faint min-w-0">
        <ToolIcon name={event.toolName} />
        <span className="truncate">{event.toolName}</span>
      </div>
    );
  }
  return null;
});

/** The steps themselves, oldest first, capped at {@link MAX_RENDERED_STEPS}. */
export function SubagentSteps({ steps }: { steps: readonly ChatEvent[] }) {
  const dropped = Math.max(0, steps.length - MAX_RENDERED_STEPS);
  const shown = dropped > 0 ? steps.slice(dropped) : steps;
  return (
    <div className="space-y-1">
      {dropped > 0 && (
        <div className="text-muted-foreground-faint italic">
          {dropped} earlier step{dropped === 1 ? "" : "s"} not shown
        </div>
      )}
      {shown.map((e) => (
        <SubagentStepLine key={e.id} event={e} />
      ))}
    </div>
  );
}
