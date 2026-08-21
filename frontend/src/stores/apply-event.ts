import { foldTaskList, isTaskListEvent } from "~/lib/event-extractors";
import type { ChatEvent, SessionData } from "~/stores/chat-types";

/**
 * Side-effect that must be executed by the store wrapper when a rate_limit event arrives.
 * Returned instead of being called here so apply-event stays pure.
 */
export interface RateLimitSideEffect {
  type: "rate_limit";
  rateLimitType: "five_hour" | "seven_day";
  status: string;
  utilization: number;
  resetsAt: number;
}

export type ApplyResult = { patch: Partial<SessionData>; sideEffect?: RateLimitSideEffect } | null;

/**
 * Pure function: given the current session, an incoming event, and whether the
 * user is currently viewing this session, returns the SessionData patch to apply
 * (or null if no update is needed).
 */
export function applyServerEvent(
  session: SessionData,
  event: ChatEvent,
  isViewing: boolean,
): ApplyResult {
  // --- Transient events: update session state without appending to turns ---

  if (event.type === "rate_limit") {
    const rlType = event.rateLimitType === "seven_day" ? "seven_day" : "five_hour";
    return {
      patch: {},
      sideEffect: {
        type: "rate_limit",
        rateLimitType: rlType,
        status: event.status ?? "",
        utilization: event.utilization ?? 0,
        resetsAt: event.resetsAt ?? 0,
      },
    };
  }

  if (
    event.type === "stream" ||
    event.type === "context_management" ||
    event.type === "tool_output_delta" ||
    event.type === "reasoning_delta" ||
    event.type === "tool_progress"
  )
    return null;

  // A dynamic workflow's launch emits a placeholder "running in the background"
  // result; the real answer arrives in a later (non-pending) result. Ignore the
  // placeholder entirely so it neither ends the turn nor renders as a message —
  // the session stays running and the workflow panel keeps streaming.
  if (event.type === "result" && event.workflowPending) return null;

  if (event.type === "message_delivery" && event.messageId) {
    const status = event.deliveryStatus === "cancelled" ? "cancelled" : "delivered";
    return { patch: applyMessageDelivery(session, event.messageId, status) };
  }

  // A live measurement of the real transcript. It supersedes the per-turn
  // numbers (which describe only the last API call and so survive compaction
  // unchanged) until the next turn or stream signal speaks.
  if (event.type === "context_usage") {
    if (event.contextWindow <= 0) return null;
    return {
      patch: {
        contextUsage: {
          contextWindow: event.contextWindow,
          usedTokens: event.usedTokens,
          inputTokens: session.contextUsage?.inputTokens ?? 0,
          outputTokens: session.contextUsage?.outputTokens ?? 0,
        },
      },
    };
  }

  if (event.type === "compact_status") {
    return { patch: { compacting: event.status === "compacting" } };
  }

  // --- Extract metadata from events regardless of whether turns are loaded ---

  const isResult = event.type === "result";
  const stamped = isResult && !event.timestamp ? { ...event, timestamp: Date.now() } : event;
  const patch: Partial<SessionData> = {};

  if (isTaskListEvent(event)) {
    // TaskCreate/TaskUpdate are incremental, so a single event isn't a full snapshot —
    // recompute the list from the whole stream. `event` isn't in turns/streamingEvents
    // yet, so append it; the fold pulls each TaskCreate's assigned id from its result.
    const stream: ChatEvent[] = [];
    for (const t of session.turns) stream.push(...t.events);
    stream.push(...session.streamingEvents, event);
    patch.todos = foldTaskList(stream);
  }

  if (isResult && event.type === "result") {
    patch.meta = { ...session.meta, state: "idle" };
    // Ambient-signal exclusion (docs/scheduled-loops.md, "Frontend — insight
    // without noise"): schedule-origin turns are second-class in the signal
    // layer — an hourly loop must not bold the sidebar row on every fire.
    // Runs that need the user are surfaced through schedule attention
    // (schedule.updated → inbox), not the unseen-completion flag.
    const completesScheduledTurn =
      session.turns[session.turns.length - 1]?.origin?.kind === "schedule";
    if (!completesScheduledTurn) patch.hasUnseenCompletion = !isViewing;
    if (event.contextWindow && event.contextWindow > 0) {
      const inputTokens = event.inputTokens ?? session.contextUsage?.inputTokens ?? 0;
      const outputTokens = event.outputTokens ?? session.contextUsage?.outputTokens ?? 0;
      // This turn's numbers are the freshest signal, so they claim usedTokens
      // from any earlier live measurement. A live one follows within a
      // round-trip (contextMeter refreshes on turn completion) and reclaims it.
      patch.contextUsage = {
        contextWindow: event.contextWindow,
        inputTokens,
        outputTokens,
        usedTokens: inputTokens + outputTokens,
      };
    }
  }

  if (event.type === "compact_boundary") {
    patch.compacting = false;
  }

  // --- Append event to the last turn's streaming buffer (or merge on result) ---

  const lastTurn = session.turns[session.turns.length - 1];
  if (lastTurn) {
    const appended =
      stamped.type === "user_message" && stamped.messageId
        ? {
            ...stamped,
            deliveryStatus: (stamped.queued ? "queued" : "sending") as "queued" | "sending",
          }
        : stamped;

    if (isResult) {
      // Turn complete — merge streaming buffer + result into the turn. Queued
      // messages target the NEXT turn (providers without native mid-turn
      // injection), so keep them in the streaming buffer: they survive this
      // boundary and are cleared when their replayed turn starts (submitQuery).
      const carryOver = session.streamingEvents.filter(
        (e) => e.type === "user_message" && e.deliveryStatus === "queued",
      );
      const merge = session.streamingEvents.filter(
        (e) => !(e.type === "user_message" && e.deliveryStatus === "queued"),
      );
      const mergedEvents = [...lastTurn.events, ...merge, appended];
      const turns = [...session.turns];
      turns[turns.length - 1] = { ...lastTurn, events: mergedEvents, complete: true };
      patch.turns = turns;
      patch.streamingEvents = carryOver;
    } else if (lastTurn.complete) {
      // Late-arriving event for an already-complete turn (rare).
      const turns = [...session.turns];
      turns[turns.length - 1] = {
        ...lastTurn,
        events: [...lastTurn.events, appended],
      };
      patch.turns = turns;
    } else {
      // Streaming: append to buffer, keep turns stable.
      const buf = session.streamingEvents;
      if (event.type === "task" && event.taskSubtype === "task_progress" && event.toolUseId) {
        // Upsert: replace previous progress for same toolUseId.
        const idx = buf.findIndex(
          (e) =>
            e.type === "task" &&
            e.taskSubtype === "task_progress" &&
            e.toolUseId === event.toolUseId,
        );
        if (idx >= 0) {
          const next = [...buf];
          next[idx] = appended;
          patch.streamingEvents = next;
        } else {
          patch.streamingEvents = [...buf, appended];
        }
      } else {
        patch.streamingEvents = [...buf, appended];
      }
    }
  }

  return { patch };
}

// --- Helpers ---

function applyMessageDelivery(
  session: SessionData,
  messageId: string,
  status: "delivered" | "cancelled",
): Partial<SessionData> {
  // Check streamingEvents first (most likely location for recent messages).
  const bufIdx = session.streamingEvents.findIndex(
    (e) => e.type === "user_message" && e.messageId === messageId,
  );
  if (bufIdx >= 0) {
    const streamingEvents = session.streamingEvents.map((e, i) =>
      i === bufIdx ? { ...e, deliveryStatus: status } : e,
    );
    return { streamingEvents };
  }
  // Fallback: search committed turn events.
  const turns = session.turns.map((turn) => {
    const idx = turn.events.findIndex(
      (e) => e.type === "user_message" && e.messageId === messageId,
    );
    if (idx < 0) return turn;
    const events = turn.events.map((e, i) => (i === idx ? { ...e, deliveryStatus: status } : e));
    return { ...turn, events };
  });
  return { turns };
}
