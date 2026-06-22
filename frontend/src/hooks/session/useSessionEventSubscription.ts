import { useEffect } from "react";
import type { useWebSocket } from "~/hooks/useWebSocket";
import { fromWireAttachment } from "~/lib/attachment-utils";
import type { ChannelMessage } from "~/lib/channel-actions";
import { parseServerEvent } from "~/lib/events";
import { extractBrainBlock } from "~/lib/prompt-parsing";
import { loadSessionHistory } from "~/lib/session/history";
import { useChannelStore } from "~/stores/channel-store";
import { useChatStore } from "~/stores/chat-store";
import { applyEvent } from "~/stores/event-orchestrator";
import { decideSeq, useEventSeqStore } from "~/stores/event-seq";
import { useStreamingStore } from "~/stores/streaming-store";

/** Subscribes to session.event and session.turn-started WS events. */
export function useSessionEventSubscription(ws: ReturnType<typeof useWebSocket>) {
  useEffect(() => {
    const unsubEvent = ws.subscribe("session.event", (payload) => {
      const raw = payload.event as Record<string, unknown>;
      const event = parseServerEvent(raw);
      if (!event) return;
      const sid = payload.sessionId;

      // --- Wire-sequence gate (runs FIRST, before any store mutation) ---
      // seq 0 = unsequenced (e.g. a channel message to an offline session) —
      // skip ordering/dedup checks and apply directly. Otherwise drop
      // duplicates/out-of-order, and resync on a gap or pipeline rebuild.
      if (payload.seq > 0) {
        const prev = useEventSeqStore.getState().states[sid];
        const { action, next } = decideSeq(prev, payload.epoch, payload.seq);
        if (action === "drop") return;
        useEventSeqStore.getState().record(sid, next);
        if (action === "resync") {
          // Backfill missed events. Coalesced by loadSessionHistory's
          // historyLoading in-flight guard; the force-load reseeds the seq
          // state authoritatively from the response's high-water mark.
          loadSessionHistory(ws, sid, true);
        }
      }

      // The orchestrator owns all cross-store sequencing (chat-store +
      // streaming-store + toolBlockIndex). The hook just parses and delegates.
      applyEvent(sid, event, raw);
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
        useChatStore.getState().adoptTurnPrompt(sid, payload.prompt);
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
