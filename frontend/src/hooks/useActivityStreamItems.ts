import { useMemo } from "react";
import type { Project } from "~/lib/types";
import { relativeTime } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useChannelStore } from "~/stores/channel-store";
import { type SessionData, useChatStore } from "~/stores/chat-store";

export type AttentionKind = "approval" | "question" | "plan" | "failed" | "unseen" | "channel_msg";

export interface AttentionItem {
  id: string;
  isChannel: boolean;
  projectSlug: string;
  kind: AttentionKind;
  time: string;
  name: string;
  sortKey: number;
}

export type StreamItem =
  | { kind: "session"; sessionId: string; projectSlug: string; lastActivity: number }
  | { kind: "channel"; channelId: string; projectSlug: string; lastActivity: number };

export interface ActivityStreamItems {
  attention: AttentionItem[];
  active: StreamItem[];
  recent: StreamItem[];
  activeUnread: number;
  recentUnread: number;
}

/**
 * Partition the user's sessions and channels into the three sidebar sections:
 *  - attention: needs user input (approvals, questions, plans)
 *  - active: live work
 *  - recent: completed/archived
 *
 * Pure derivation from store state — no side effects, deterministic.
 */
export function useActivityStreamItems(
  searchQuery: string,
  filterProjectId: string | null,
): ActivityStreamItems {
  const sessions = useChatStore((s) => s.sessions);
  const projects = useAppStore((s) => s.projects);
  const channelMap = useChannelStore((s) => s.channels);

  return useMemo(() => {
    const projectMap = new Map(projects.map((p: Project) => [p.id, p]));
    const channels = Object.values(channelMap);
    const channelSessionIds = new Set<string>();
    for (const ch of channels) {
      for (const m of ch.members) channelSessionIds.add(m.sessionId);
    }

    const q = searchQuery.toLowerCase();
    const attention: AttentionItem[] = [];
    const activeItems: StreamItem[] = [];
    const recentItems: StreamItem[] = [];
    let activeUnread = 0;
    let recentUnread = 0;

    function matchesSearch(sessionName: string, project: Project): boolean {
      if (!q) return true;
      return (
        (sessionName || "").toLowerCase().includes(q) ||
        project.name.toLowerCase().includes(q) ||
        project.slug.toLowerCase().includes(q)
      );
    }

    // --- Sessions ---
    for (const [id, data] of Object.entries(sessions) as [string, SessionData][]) {
      if (channelSessionIds.has(id)) continue;

      const project = projectMap.get(data.meta.projectId);
      if (!project) continue;
      if (filterProjectId && project.id !== filterProjectId) continue;
      if (!matchesSearch(data.meta.name, project)) continue;

      const updatedAt = data.meta.updatedAt ? new Date(data.meta.updatedAt).getTime() : Date.now();
      const timeStr = relativeTime(data.meta.updatedAt ?? data.meta.createdAt);

      const lastActivity = data.meta.lastQueryAt
        ? new Date(data.meta.lastQueryAt).getTime()
        : updatedAt;

      // Archived by user — always goes to completed, regardless of state
      if (data.meta.completedAt) {
        if (data.hasUnseenCompletion) recentUnread++;
        recentItems.push({
          kind: "session",
          sessionId: id,
          projectSlug: project.slug,
          lastActivity,
        });
        continue;
      }

      // Inbox: agent is blocked waiting on user input
      let attentionKind: AttentionKind | null = null;
      if (data.pendingApproval) {
        attentionKind = data.planMode ? "plan" : "approval";
      } else if (data.pendingQuestion) {
        attentionKind = "question";
      }

      if (attentionKind) {
        attention.push({
          id,
          isChannel: false,
          projectSlug: project.slug,
          kind: attentionKind,
          time: timeStr,
          name: data.meta.name,
          sortKey: updatedAt,
        });
        continue;
      }

      // Everything else — active
      if (data.hasUnseenCompletion) activeUnread++;
      activeItems.push({
        kind: "session",
        sessionId: id,
        projectSlug: project.slug,
        lastActivity,
      });
    }

    // --- Channels ---
    for (const ch of channels) {
      const project = projectMap.get(ch.projectId);
      if (!project) continue;
      if (filterProjectId && project.id !== filterProjectId) continue;

      if (q) {
        const nameMatch = ch.name.toLowerCase().includes(q);
        const projectMatch =
          project.name.toLowerCase().includes(q) || project.slug.toLowerCase().includes(q);
        if (!nameMatch && !projectMatch) continue;
      }

      // Check if channel has attention-worthy members
      let worstKind: AttentionKind | null = null;
      let worstTime = 0;

      for (const m of ch.members) {
        const data = sessions[m.sessionId];
        if (!data) continue;
        const t = data.meta.updatedAt ? new Date(data.meta.updatedAt).getTime() : 0;

        if (data.pendingApproval || data.pendingQuestion) {
          worstKind = "approval";
          worstTime = Math.max(worstTime, t);
        }
      }

      if (worstKind) {
        attention.push({
          id: ch.id,
          isChannel: true,
          projectSlug: project.slug,
          kind: worstKind,
          time: relativeTime(ch.createdAt),
          name: `#${ch.name}`,
          sortKey: worstTime,
        });
        continue;
      }

      // Non-attention channel — active or completed
      let lastActivity = new Date(ch.createdAt).getTime();
      let allArchived = ch.members.length > 0;

      for (const m of ch.members) {
        const data = sessions[m.sessionId];
        if (!data) continue;
        if (!data.meta.completedAt) allArchived = false;
        const t = data.meta.lastQueryAt
          ? new Date(data.meta.lastQueryAt).getTime()
          : data.meta.updatedAt
            ? new Date(data.meta.updatedAt).getTime()
            : 0;
        lastActivity = Math.max(lastActivity, t);
      }

      const item: StreamItem = {
        kind: "channel",
        channelId: ch.id,
        projectSlug: project.slug,
        lastActivity,
      };

      if (allArchived && ch.members.length > 0) {
        recentItems.push(item);
      } else {
        activeItems.push(item);
      }
    }

    attention.sort((a, b) => b.sortKey - a.sortKey);
    activeItems.sort((a, b) => b.lastActivity - a.lastActivity);
    recentItems.sort((a, b) => b.lastActivity - a.lastActivity);

    return { attention, active: activeItems, recent: recentItems, activeUnread, recentUnread };
  }, [sessions, projects, channelMap, searchQuery, filterProjectId]);
}
