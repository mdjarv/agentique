import { extractTodosFromEvent } from "~/lib/event-extractors";
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

  if (event.type === "stream" || event.type === "context_management") return null;

  if (event.type === "message_delivery" && event.messageId) {
    return { patch: applyMessageDelivery(session, event.messageId) };
  }

  if (event.type === "compact_status") {
    return { patch: { compacting: event.status === "compacting" } };
  }

  // --- Extract metadata from events regardless of whether turns are loaded ---

  const todos = extractTodosFromEvent(event);
  const isResult = event.type === "result";
  const stamped = isResult && !event.timestamp ? { ...event, timestamp: Date.now() } : event;
  const patch: Partial<SessionData> = {};

  if (todos) patch.todos = todos;

  if (isResult && event.type === "result") {
    patch.meta = { ...session.meta, state: "idle" };
    patch.hasUnseenCompletion = !isViewing;
    if (event.contextWindow && event.contextWindow > 0) {
      patch.contextUsage = {
        contextWindow: event.contextWindow,
        inputTokens: event.inputTokens ?? session.contextUsage?.inputTokens ?? 0,
        outputTokens: event.outputTokens ?? session.contextUsage?.outputTokens ?? 0,
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
        ? { ...stamped, deliveryStatus: "sending" as const }
        : stamped;

    if (isResult) {
      // Turn complete — merge streaming buffer + result into the turn.
      const mergedEvents = [...lastTurn.events, ...session.streamingEvents, appended];
      const turns = [...session.turns];
      turns[turns.length - 1] = { ...lastTurn, events: mergedEvents, complete: true };
      patch.turns = turns;
      patch.streamingEvents = [];
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

function applyMessageDelivery(session: SessionData, messageId: string): Partial<SessionData> {
  // Check streamingEvents first (most likely location for recent messages).
  const bufIdx = session.streamingEvents.findIndex(
    (e) => e.type === "user_message" && e.messageId === messageId,
  );
  if (bufIdx >= 0) {
    const streamingEvents = session.streamingEvents.map((e, i) =>
      i === bufIdx ? { ...e, deliveryStatus: "delivered" as const } : e,
    );
    return { streamingEvents };
  }
  // Fallback: search committed turn events.
  const turns = session.turns.map((turn) => {
    const idx = turn.events.findIndex(
      (e) => e.type === "user_message" && e.messageId === messageId,
    );
    if (idx < 0) return turn;
    const events = turn.events.map((e, i) =>
      i === idx ? { ...e, deliveryStatus: "delivered" as const } : e,
    );
    return { ...turn, events };
  });
  return { turns };
}
