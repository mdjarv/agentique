import { create } from "zustand";
import type { ChannelInfo, ChannelMember, TimelineEvent } from "~/lib/channel-actions";

interface ChannelState {
  channels: Record<string, ChannelInfo>;
  timelines: Record<string, TimelineEvent[]>;

  setChannels: (channels: ChannelInfo[]) => void;
  mergeChannels: (channels: ChannelInfo[]) => void;
  addChannel: (channel: ChannelInfo) => void;
  removeChannel: (channelId: string) => void;
  updateChannelName: (channelId: string, name: string) => void;

  addMember: (channelId: string, member: ChannelMember) => void;
  removeMember: (channelId: string, sessionId: string) => void;
  updateMemberState: (sessionId: string, state: string, connected?: boolean) => void;

  setTimeline: (channelId: string, events: TimelineEvent[]) => void;
  appendTimelineEvent: (channelId: string, event: TimelineEvent) => void;

  getChannelForSession: (sessionId: string) => ChannelInfo | undefined;
  getChannelsForSession: (sessionId: string) => ChannelInfo[];
}

export const useChannelStore = create<ChannelState>((set, get) => ({
  channels: {},
  timelines: {},

  setChannels: (channels) =>
    set({
      channels: Object.fromEntries(channels.map((c) => [c.id, c])),
    }),

  mergeChannels: (channels) =>
    set((s) => {
      const merged = { ...s.channels };
      for (const c of channels) {
        const existing = merged[c.id];
        // Don't overwrite a channel that has more members (stale RPC vs fresh broadcast).
        if (existing && existing.members.length >= c.members.length) continue;
        merged[c.id] = c;
      }
      return { channels: merged };
    }),

  addChannel: (channel) => set((s) => ({ channels: { ...s.channels, [channel.id]: channel } })),

  removeChannel: (channelId) =>
    set((s) => {
      const { [channelId]: _, ...rest } = s.channels;
      const { [channelId]: __, ...restTimelines } = s.timelines;
      return { channels: rest, timelines: restTimelines };
    }),

  updateChannelName: (channelId, name) =>
    set((s) => {
      const channel = s.channels[channelId];
      if (!channel) return s;
      return { channels: { ...s.channels, [channelId]: { ...channel, name } } };
    }),

  addMember: (channelId, member) =>
    set((s) => {
      const channel = s.channels[channelId];
      if (!channel) return s;
      if (channel.members.some((m) => m.sessionId === member.sessionId)) return s;
      return {
        channels: {
          ...s.channels,
          [channelId]: { ...channel, members: [...channel.members, member] },
        },
      };
    }),

  removeMember: (channelId, sessionId) =>
    set((s) => {
      const channel = s.channels[channelId];
      if (!channel) return s;
      return {
        channels: {
          ...s.channels,
          [channelId]: {
            ...channel,
            members: channel.members.filter((m) => m.sessionId !== sessionId),
          },
        },
      };
    }),

  updateMemberState: (sessionId, state, connected) =>
    set((s) => {
      let changed = false;
      const channels = { ...s.channels };
      for (const [cid, channel] of Object.entries(channels)) {
        const idx = channel.members.findIndex((m) => m.sessionId === sessionId);
        if (idx === -1) continue;
        const prev = channel.members[idx];
        if (!prev) continue;
        if (prev.state === state && (connected === undefined || prev.connected === connected))
          continue;
        const patched: ChannelMember = { ...prev, state };
        if (connected !== undefined) patched.connected = connected;
        const updated = [...channel.members];
        updated[idx] = patched;
        channels[cid] = { ...channel, members: updated };
        changed = true;
      }
      return changed ? { channels } : s;
    }),

  setTimeline: (channelId, events) =>
    set((s) => ({
      timelines: { ...s.timelines, [channelId]: events },
    })),

  appendTimelineEvent: (channelId, event) =>
    set((s) => ({
      timelines: {
        ...s.timelines,
        [channelId]: [...(s.timelines[channelId] ?? []), event],
      },
    })),

  getChannelForSession: (sessionId) => {
    const channels = get().channels;
    return Object.values(channels).find((c) => c.members.some((m) => m.sessionId === sessionId));
  },

  getChannelsForSession: (sessionId) => {
    const channels = get().channels;
    return Object.values(channels).filter((c) => c.members.some((m) => m.sessionId === sessionId));
  },
}));
