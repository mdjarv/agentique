import { useEffect } from "react";
import type { useWebSocket } from "~/hooks/useWebSocket";
import { fromWireAttachment } from "~/lib/attachment-utils";
import type { ChannelMessage } from "~/lib/channel-actions";
import { extractBrainBlock } from "~/lib/prompt-parsing";
import { ingestSessionEvent } from "~/lib/session/ingest";
import { useChannelStore } from "~/stores/channel-store";
import { useChatStore } from "~/stores/chat-store";
import { useStreamingStore } from "~/stores/streaming-store";

/** Subscribes to session.event and session.turn-started WS events. */
export function useSessionEventSubscription(ws: ReturnType<typeof useWebSocket>) {
  useEffect(() => {
    // Gate-then-parse-then-apply lives in ingestSessionEvent — the gate must
    // see every stamped event, parsed or not, or ignored event types would
    // manufacture seq gaps. The hook just subscribes and delegates.
    const unsubEvent = ws.subscribe("session.event", (payload) => {
      ingestSessionEvent(ws, payload);
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
      // The broadcast prompt may carry a system-injected <brain> recall envelope
      // the optimistic turn (built from the user's raw input) doesn't have. Peel
      // it before matching, otherwise recall defeats the dedup and a duplicate
      // turn is created. On a match, adopt the augmented prompt so the recalled-
      // memory card renders live.
      const core = extractBrainBlock(payload.prompt)?.rest ?? payload.prompt;
      if (
        lastTurn &&
        !lastTurn.complete &&
        lastTurn.events.length === 0 &&
        lastTurn.prompt === core
      ) {
        useChatStore.getState().adoptTurnPrompt(sid, payload.prompt, {
          turnIndex: payload.turnIndex,
          origin: payload.origin,
        });
        return;
      }
      const attachments = payload.attachments?.map(fromWireAttachment);
      useChatStore.getState().submitQuery(sid, payload.prompt, attachments, {
        turnIndex: payload.turnIndex,
        origin: payload.origin,
      });
    });

    return () => {
      unsubEvent();
      unsubChannelMessage();
      unsubTurnStarted();
    };
  }, [ws]);
}
