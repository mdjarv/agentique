import { useMemo } from "react";
import { useChannelStore } from "~/stores/channel-store";
import type { SessionItem } from "./types";

interface ChannelGroups {
  /** Sessions that are not workers of any channel lead. */
  topLevel: SessionItem[];
  /** Map from lead session ID → its worker sessions. */
  workerMap: Map<string, SessionItem[]>;
  /** Map from lead session ID → channel ID it leads. */
  channelForLead: Map<string, string>;
}

export function useChannelGroups(sessions: SessionItem[]): ChannelGroups {
  const channels = useChannelStore((s) => s.channels);

  return useMemo(() => {
    const workerSessionIds = new Set<string>();
    const wMap = new Map<string, SessionItem[]>();
    const chForLead = new Map<string, string>();

    for (const { id, data } of sessions) {
      const channelIds = data.meta.channelIds;
      const channelRoles = data.meta.channelRoles;
      if (!channelIds?.length || !channelRoles) continue;

      for (const chId of channelIds) {
        if (channelRoles[chId] !== "lead") continue;
        chForLead.set(id, chId);
        const channel = channels[chId];
        if (!channel) continue;
        const workers: SessionItem[] = [];
        for (const member of channel.members) {
          if (member.role !== "worker") continue;
          const workerSession = sessions.find((s) => s.id === member.sessionId);
          if (workerSession) {
            workers.push(workerSession);
            workerSessionIds.add(member.sessionId);
          }
        }
        if (workers.length > 0) wMap.set(id, workers);
      }
    }

    return {
      topLevel: sessions.filter((s) => !workerSessionIds.has(s.id)),
      workerMap: wMap,
      channelForLead: chForLead,
    };
  }, [sessions, channels]);
}
