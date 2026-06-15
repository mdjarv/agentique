import { useEffect } from "react";
import type { useWebSocket } from "~/hooks/useWebSocket";
import { fromWireAttachment } from "~/lib/attachment-utils";
import type { ChannelMessage } from "~/lib/channel-actions";
import { parseServerEvent } from "~/lib/events";
import { useChannelStore } from "~/stores/channel-store";
import { useChatStore } from "~/stores/chat-store";
import { applyEvent } from "~/stores/event-orchestrator";
import { useStreamingStore } from "~/stores/streaming-store";

/** Subscribes to session.event and session.turn-started WS events. */
export function useSessionEventSubscription(ws: ReturnType<typeof useWebSocket>) {
  useEffect(() => {
    const unsubEvent = ws.subscribe("session.event", (payload) => {
      const raw = payload.event as Record<string, unknown>;
      const event = parseServerEvent(raw);
      if (!event) return;
      // The orchestrator owns all cross-store sequencing (chat-store +
      // streaming-store + toolBlockIndex). The hook just parses and delegates.
      applyEvent(payload.sessionId, event, raw);
    });

    const unsubChannelMessage = ws.subscribe("channel.message", (payload) => {
      const msg = payload as ChannelMessage;
      useChannelStore.getState().appendTimelineEvent(msg.channelId, msg);

      // Mark member sessions as having unread channel messages.
      const chatStore = useChatStore.getState();
      const channel = useChannelStore.getState().channels[msg.channelId];
      if (channel) {
        for (const member of channel.members) {
          if (member.sessionId !== chatStore.activeSessionId) {
            chatStore.setUnreadChannelMessage(member.sessionId, true);
          }
        }
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
      unsubChannelMessage();
      unsubTurnStarted();
    };
  }, [ws]);
}
