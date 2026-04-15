import { useEffect } from "react";
import type { useWebSocket } from "~/hooks/useWebSocket";
import { fromWireAttachment } from "~/lib/attachment-utils";
import { parseServerEvent } from "~/lib/events";
import { useChannelStore } from "~/stores/channel-store";
import { useChatStore } from "~/stores/chat-store";
import { useStreamingStore } from "~/stores/streaming-store";

const toolBlockIndex = new Map<string, Map<number, string>>();

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
        let sessionMap = toolBlockIndex.get(sessionId);
        if (!sessionMap) {
          sessionMap = new Map();
          toolBlockIndex.set(sessionId, sessionMap);
        }
        if (typeof evt.index === "number" && typeof cb.id === "string") {
          sessionMap.set(evt.index, cb.id);
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
          typeof evt.index === "number" ? toolBlockIndex.get(sessionId)?.get(evt.index) : undefined;
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

function clearToolBlockIndex(sessionId: string) {
  toolBlockIndex.delete(sessionId);
}

/** Subscribes to session.event and session.turn-started WS events. */
export function useSessionEventSubscription(ws: ReturnType<typeof useWebSocket>) {
  useEffect(() => {
    const unsubEvent = ws.subscribe("session.event", (payload) => {
      const event = parseServerEvent(payload.event as Record<string, unknown>);
      if (!event) return;
      const sid: string = payload.sessionId;
      const streaming = useStreamingStore.getState();

      if (event.type === "stream") {
        handleStreamDelta(sid, payload.event as Record<string, unknown>);
        return;
      }

      useChatStore.getState().handleServerEvent(sid, event);

      if (event.type === "agent_message") {
        const chatStore = useChatStore.getState();
        const channelIds = chatStore.sessions[sid]?.meta.channelIds;
        if (channelIds && channelIds.length > 0) {
          // Convert legacy agent_message event to ChannelMessage shape.
          const channelMessage = {
            id: `evt-${sid}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
            channelId: channelIds[0] ?? "",
            senderType: (event.fromUser ? "user" : "session") as "session" | "user",
            senderId: event.senderSessionId ?? "",
            senderName: event.senderName ?? "",
            content: event.content ?? "",
            messageType: event.messageType,
            metadata: {
              targetSessionId: event.targetSessionId ?? "",
              targetName: event.targetName ?? "",
            },
            createdAt: new Date().toISOString(),
          };
          for (const chId of channelIds) {
            useChannelStore.getState().appendTimelineEvent(chId, channelMessage);
          }
          if (chatStore.activeSessionId !== sid) {
            chatStore.setUnreadChannelMessage(sid, true);
          }
        }
      }

      if (event.type === "tool_use") {
        streaming.clearToolInput(sid, event.toolId);
      }
      if (event.type === "result") {
        streaming.clearText(sid);
        streaming.clearAllToolInputs(sid);
        clearToolBlockIndex(sid);
      }
    });

    const unsubTurnStarted = ws.subscribe("session.turn-started", (payload) => {
      const sid: string = payload.sessionId;
      useStreamingStore.getState().clearText(sid);
      const session = useChatStore.getState().sessions[sid];
      const lastTurn = session?.turns[session.turns.length - 1];
      if (
        lastTurn &&
        !lastTurn.complete &&
        lastTurn.events.length === 0 &&
        lastTurn.prompt === payload.prompt
      ) {
        return;
      }
      const attachments = payload.attachments?.map(fromWireAttachment);
      useChatStore.getState().submitQuery(sid, payload.prompt, attachments);
    });

    return () => {
      unsubEvent();
      unsubTurnStarted();
    };
  }, [ws]);
}
