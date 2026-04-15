import { useEffect } from "react";
import type { useWebSocket } from "~/hooks/useWebSocket";
import { useChannelStore } from "~/stores/channel-store";
import { useChatStore } from "~/stores/chat-store";

function removeChannelEverywhere(channelId: string) {
  useChannelStore.getState().removeChannel(channelId);
  const sessions = useChatStore.getState().sessions;
  for (const [sid, data] of Object.entries(sessions)) {
    if (data.meta.channelIds?.includes(channelId)) {
      useChatStore.getState().removeSessionChannel(sid, channelId);
    }
  }
}

export function useChannelSubscriptions(ws: ReturnType<typeof useWebSocket>) {
  useEffect(() => {
    const unsubCreated = ws.subscribe("channel.created", (payload) => {
      useChannelStore.getState().addChannel(payload);
    });

    const unsubDeleted = ws.subscribe("channel.deleted", (payload) => {
      removeChannelEverywhere(payload.channelId);
    });

    const unsubDissolved = ws.subscribe("channel.dissolved", (payload) => {
      removeChannelEverywhere(payload.channelId);
    });

    const unsubUpdated = ws.subscribe("channel.updated", (payload) => {
      useChannelStore.getState().addChannel(payload);
    });

    const unsubJoined = ws.subscribe("channel.member-joined", (payload) => {
      if (payload.channel) {
        useChannelStore.getState().addChannel(payload.channel);
      } else {
        useChannelStore.getState().addMember(payload.channelId, payload.member);
      }
      useChatStore
        .getState()
        .addSessionChannel(payload.member.sessionId, payload.channelId, payload.member.role);
    });

    const unsubLeft = ws.subscribe("channel.member-left", (payload) => {
      useChannelStore.getState().removeMember(payload.channelId, payload.sessionId);
      useChatStore.getState().removeSessionChannel(payload.sessionId, payload.channelId);
    });

    return () => {
      unsubCreated();
      unsubDeleted();
      unsubDissolved();
      unsubUpdated();
      unsubJoined();
      unsubLeft();
    };
  }, [ws]);
}
