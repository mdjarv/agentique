import { useNavigate } from "@tanstack/react-router";
import { Hash, Users } from "lucide-react";
import { memo, useCallback, useMemo } from "react";
import { useShallow } from "zustand/react/shallow";
import { PulseStatus } from "~/components/layout/session/PulseStatus";
import { SessionStatusBadge } from "~/components/layout/session/SessionStatusBadge";
import { ActivityFeed } from "~/components/layout/variants/folder-sidebar/ActivityFeed";
import type { ChannelInfo, ChannelMember } from "~/lib/channel-actions";
import { cn } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useChannelStore } from "~/stores/channel-store";
import { type SessionData, useChatStore } from "~/stores/chat-store";

/** True when every member in channel is idle/done/stopped/disconnected. */
function isChannelDimmed(members: ChannelMember[], sessions: Record<string, SessionData>): boolean {
  if (members.length === 0) return true;
  const restingStates = new Set(["idle", "done", "stopped", "failed"]);
  return members.every((m) => {
    const session = sessions[m.sessionId];
    if (!session) return true;
    return restingStates.has(session.meta.state);
  });
}

// ─── Member row ────────────────────────────────────────────────

const MemberRow = memo(function MemberRow({
  member,
  session,
}: {
  member: ChannelMember;
  session: SessionData | undefined;
}) {
  const state = session?.meta.state ?? "idle";
  const connected = session?.meta.connected ?? false;
  const hasPending = !!(session?.pendingApproval || session?.pendingQuestion);
  const isPlanning = session?.planMode ?? false;

  return (
    <div className="flex items-center gap-1.5 py-0.5 pl-6 pr-2 min-w-0">
      <SessionStatusBadge
        state={state}
        connected={connected}
        hasUnseenCompletion={false}
        hasPendingApproval={hasPending}
        isPlanning={isPlanning}
        size="sm"
      />
      <div className="flex-1 min-w-0">
        <span
          className={cn(
            "block truncate text-xs",
            member.role === "lead" ? "font-medium" : "text-muted-foreground",
          )}
        >
          {member.name}
        </span>
        {state === "running" && <PulseStatus sessionId={member.sessionId} />}
      </div>
      <span className="text-[10px] text-muted-foreground-faint shrink-0">{member.role}</span>
    </div>
  );
});

// ─── Channel card ──────────────────────────────────────────────

const ChannelCard = memo(function ChannelCard({
  channel,
  projectSlug,
  dimmed,
}: {
  channel: ChannelInfo;
  projectSlug: string;
  dimmed: boolean;
}) {
  const navigate = useNavigate();
  const sessions = useChatStore(useShallow((s) => s.sessions));

  const handleClick = useCallback(() => {
    useAppStore.getState().setSidebarOpen(false);
    navigate({
      to: "/project/$projectSlug/channel/$channelId",
      params: { projectSlug, channelId: channel.id },
    });
  }, [navigate, projectSlug, channel.id]);

  return (
    <div className={cn("rounded-md transition-opacity", dimmed && "opacity-50")}>
      <button
        type="button"
        onClick={handleClick}
        className="flex items-center gap-2 w-full px-2 py-1.5 rounded-md text-left hover:bg-sidebar-accent/40 transition-colors cursor-pointer"
      >
        <Hash className="size-3.5 text-primary/60 shrink-0" />
        <span className="text-sm truncate flex-1">{channel.name}</span>
        <span className="flex items-center gap-0.5 text-[10px] text-muted-foreground-faint shrink-0">
          <Users className="size-3" />
          {channel.members.length}
        </span>
      </button>
      <div className="pb-1">
        {channel.members.map((m) => (
          <MemberRow key={m.sessionId} member={m} session={sessions[m.sessionId]} />
        ))}
      </div>
    </div>
  );
});

// ─── Project group ─────────────────────────────────────────────

function ProjectChannelGroup({
  projectName,
  projectSlug,
  channels,
}: {
  projectName: string;
  projectSlug: string;
  channels: ChannelInfo[];
}) {
  const sessions = useChatStore(useShallow((s) => s.sessions));

  return (
    <div className="space-y-1">
      <h3 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground-faint px-2">
        {projectName}
      </h3>
      {channels.map((ch) => (
        <ChannelCard
          key={ch.id}
          channel={ch}
          projectSlug={projectSlug}
          dimmed={isChannelDimmed(ch.members, sessions)}
        />
      ))}
    </div>
  );
}

// ─── Main ──────────────────────────────────────────────────────

export function TeamsTab() {
  const channels = useChannelStore(useShallow((s) => s.channels));
  const projects = useAppStore(useShallow((s) => s.projects));

  const channelList = useMemo(() => Object.values(channels), [channels]);

  // Group channels by project.
  const grouped = useMemo(() => {
    const projectMap = new Map(projects.map((p) => [p.id, p]));
    const groups = new Map<string, { name: string; slug: string; channels: ChannelInfo[] }>();

    for (const ch of channelList) {
      const project = projectMap.get(ch.projectId);
      if (!project) continue;
      let group = groups.get(ch.projectId);
      if (!group) {
        group = { name: project.name, slug: project.slug, channels: [] };
        groups.set(ch.projectId, group);
      }
      group.channels.push(ch);
    }

    return Array.from(groups.values());
  }, [channelList, projects]);

  // Collect unique project IDs from channels for activity feeds.
  const projectIds = useMemo(
    () => [...new Set(channelList.map((ch) => ch.projectId))],
    [channelList],
  );

  if (channelList.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center text-muted-foreground text-sm">
        No active teams
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-y-auto min-h-0 py-2 px-2 space-y-3">
      {grouped.map((g) => (
        <ProjectChannelGroup
          key={g.slug}
          projectName={g.name}
          projectSlug={g.slug}
          channels={g.channels}
        />
      ))}
      {projectIds.map((pid) => (
        <ActivityFeed key={pid} projectId={pid} />
      ))}
    </div>
  );
}
