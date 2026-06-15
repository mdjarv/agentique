import { useChatStore } from "~/stores/chat-store";
import type { ChatEvent } from "~/stores/chat-types";
import { useStreamingStore } from "~/stores/streaming-store";

/**
 * Cross-store event orchestrator.
 *
 * Owns the single contract for applying a parsed `session.event` across the
 * chat-store (durable turns) and the streaming-store (in-flight deltas +
 * toolBlockIndex). Previously this sequencing was scattered through the
 * subscription hook; centralizing it here gives one testable seam where the
 * "a turn-complete clears chat-store + streaming-store together" invariant
 * lives.
 *
 * `apply-event.ts` stays a pure reducer (chat-store side). This orchestrator
 * wraps it (via `chatStore.handleServerEvent`) plus the streaming-store clears
 * so the two stores transition atomically per event.
 */

/**
 * Routes a raw provider `stream` event (partial-message deltas) into the
 * streaming-store. Reads nested provider fields not present on the parsed
 * ChatEvent, so it takes the raw wire object. Malformed shapes are ignored.
 */
function handleStreamDelta(sessionId: string, rawEvent: Record<string, unknown>) {
  try {
    const inner = rawEvent.event;
    if (inner == null || typeof inner !== "object") return;
    const evt = inner as Record<string, unknown>;

    const type = evt.type;
    if (typeof type !== "string") return;

    if (type === "message_start") {
      const message = evt.message;
      if (message == null || typeof message !== "object") return;
      const usage = (message as Record<string, unknown>).usage;
      if (usage == null || typeof usage !== "object") return;
      const u = usage as Record<string, unknown>;
      if (typeof u.input_tokens !== "number") return;
      const contextTokens =
        u.input_tokens +
        (typeof u.cache_read_input_tokens === "number" ? u.cache_read_input_tokens : 0) +
        (typeof u.cache_creation_input_tokens === "number" ? u.cache_creation_input_tokens : 0);
      useChatStore.getState().updateStreamingContextUsage(sessionId, {
        inputTokens: contextTokens,
      });
      return;
    }

    if (type === "message_delta") {
      const usage = evt.usage;
      if (usage == null || typeof usage !== "object") return;
      const u = usage as Record<string, unknown>;
      if (typeof u.output_tokens !== "number") return;
      useChatStore.getState().updateStreamingContextUsage(sessionId, {
        outputTokens: u.output_tokens,
      });
      return;
    }

    if (type === "content_block_start") {
      const contentBlock = evt.content_block;
      if (contentBlock == null || typeof contentBlock !== "object") return;
      const cb = contentBlock as Record<string, unknown>;
      if (cb.type === "tool_use") {
        if (typeof evt.index === "number" && typeof cb.id === "string") {
          useStreamingStore.getState().setToolBlockId(sessionId, evt.index, cb.id);
        }
      } else if (cb.type === "text") {
        const existing = useStreamingStore.getState().texts[sessionId];
        if (existing) {
          useStreamingStore.getState().appendText(sessionId, "\n\n");
        }
      }
      return;
    }

    if (type === "content_block_delta") {
      const delta = evt.delta;
      if (delta == null || typeof delta !== "object") return;
      const d = delta as Record<string, unknown>;
      if (d.type === "input_json_delta" && typeof d.partial_json === "string") {
        const toolId =
          typeof evt.index === "number"
            ? useStreamingStore.getState().toolBlockIndex[sessionId]?.[evt.index]
            : undefined;
        if (toolId) {
          useStreamingStore.getState().appendToolInput(sessionId, toolId, d.partial_json);
        }
      } else if (d.type === "text_delta" && typeof d.text === "string") {
        useStreamingStore.getState().appendText(sessionId, d.text);
      }
    }
  } catch {
    // Ignore malformed stream events
  }
}

/**
 * Applies a parsed session event across both stores.
 *
 * @param sessionId  Target session.
 * @param event      The parsed ChatEvent.
 * @param rawEvent   The raw wire object (needed for `stream` delta routing).
 */
export function applyEvent(
  sessionId: string,
  event: ChatEvent,
  rawEvent: Record<string, unknown>,
): void {
  const streaming = useStreamingStore.getState();

  // --- Streaming-only deltas: never touch the durable chat-store. ---
  if (event.type === "stream") {
    handleStreamDelta(sessionId, rawEvent);
    return;
  }
  if (event.type === "tool_output_delta") {
    streaming.appendToolOutput(sessionId, event.itemId, event.delta);
    return;
  }
  if (event.type === "reasoning_delta") {
    streaming.appendReasoning(sessionId, event.itemId, event.delta);
    return;
  }
  if (event.type === "tool_progress") {
    streaming.setToolProgress(sessionId, event.toolUseId, {
      elapsedMs: event.elapsedMs,
      toolName: event.toolName,
    });
    return;
  }

  // --- Durable apply (pure reducer + side effects) ---
  useChatStore.getState().handleServerEvent(sessionId, event);

  // --- Streaming-store clears that must accompany the durable transition ---
  if (event.type === "tool_use") {
    streaming.clearToolInput(sessionId, event.toolId);
  }
  if (event.type === "tool_result") {
    streaming.clearToolOutput(sessionId, event.toolId);
    streaming.clearToolProgress(sessionId, event.toolId);
  }
  if (event.type === "result") {
    // Turn complete: drain every streaming buffer for this session together
    // with the chat-store merge that handleServerEvent just performed. The
    // toolBlockIndex lives in the streaming-store (not a module Map) so it is
    // reclaimed here and can't leak across turns.
    streaming.clearText(sessionId);
    streaming.clearAllToolInputs(sessionId);
    streaming.clearAllReasoning(sessionId);
    streaming.clearToolBlockIndex(sessionId);
  }
}
