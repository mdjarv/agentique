import { useNavigate } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, FolderOpen, Hash, Plus, Star } from "lucide-react";
import { type ReactNode, memo, useCallback, useState } from "react";
import { toast } from "sonner";
import { useShallow } from "zustand/shallow";
import { ANIMATE_DEFAULT, useAutoAnimate } from "~/hooks/useAutoAnimate";
import { useWebSocket } from "~/hooks/useWebSocket";
import { setProjectFavorite } from "~/lib/project-actions";
import { getTagColor } from "~/lib/tag-colors";
import type { Project } from "~/lib/types";
import { cn, getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { type ChatState, type SessionData, useChatStore } from "~/stores/chat-store";
import { useTeamStore } from "~/stores/team-store";
import { useUIStore } from "~/stores/ui-store";
import { ProjectHoverCard } from "./ProjectHoverCard";
import { SessionHoverCard } from "./SessionHoverCard";
import { SessionRow } from "./SessionRow";

function sessionNeedsInput(data: SessionData): boolean {
  return !!(data.pendingApproval || data.pendingQuestion);
}

function partitionActiveSessions(
  ids: string[],
  sessions: ChatState["sessions"],
): { promoted: string[]; rest: string[] } {
  const promoted: string[] = [];
  const rest: string[] = [];

  for (const id of ids) {
    const data = sessions[id];
    if (!data) continue;
    if (sessionNeedsInput(data)) promoted.push(id);
    else rest.push(id);
  }

  const byCreatedDesc = (a: string, b: string) => {
    const ta = new Date(sessions[a]?.meta.createdAt ?? 0).getTime();
    const tb = new Date(sessions[b]?.meta.createdAt ?? 0).getTime();
    return tb - ta;
  };

  promoted.sort(byCreatedDesc);
  rest.sort(byCreatedDesc);
  return { promoted, rest };
}

function sortCompletedByDate(ids: string[], sessions: ChatState["sessions"]): string[] {
  return [...ids].sort((a, b) => {
    const ta = new Date(sessions[a]?.meta?.completedAt ?? 0).getTime();
    const tb = new Date(sessions[b]?.meta?.completedAt ?? 0).getTime();
    return tb - ta;
  });
}

/** Subscribes narrowly per-session so only the affected row re-renders. */
const SidebarSessionRow = memo(function SidebarSessionRow({
  id,
  activeSessionId,
  onSessionClick,
}: {
  id: string;
  activeSessionId: string | undefined;
  onSessionClick: (id: string) => void;
}) {
  const meta = useChatStore((s) => s.sessions[id]?.meta);
  const hasUnseenCompletion = useChatStore((s) => s.sessions[id]?.hasUnseenCompletion ?? false);
  const hasPendingInput = useChatStore(
    (s) => !!(s.sessions[id]?.pendingApproval || s.sessions[id]?.pendingQuestion),
  );
  const isPlanning = useChatStore((s) => !!s.sessions[id]?.planMode);
  const hasDraft = useUIStore((s) => !!s.drafts[id]);
  const handleClick = useCallback(() => onSessionClick(id), [onSessionClick, id]);

  if (!meta) return null;

  return (
    <SessionHoverCard sessionId={id}>
      <SessionRow
        name={meta.name}
        state={meta.state}
        connected={meta.connected}
        hasUnseenCompletion={hasUnseenCompletion}
        hasPendingApproval={hasPendingInput}
        isPlanning={isPlanning}
        isActive={id === activeSessionId}
        hasDraft={hasDraft}
        worktreeMerged={meta.worktreeMerged}
        commitsAhead={meta.commitsAhead}
        gitOperation={meta.gitOperation}
        onClick={handleClick}
      />
    </SessionHoverCard>
  );
});

function SessionGroups({
  sessionIds,
  activeSessionId,
  onSessionClick,
  projectSlug,
  newChatButton,
}: {
  sessionIds: string[];
  activeSessionId: string | undefined;
  onSessionClick: (id: string) => void;
  projectSlug: string;
  newChatButton: ReactNode;
}) {
  // Subscribed here (not in ProjectTreeItem) so the parent doesn't re-render on turn events.
  // Sorting is cheap; SidebarSessionRow is memo'd so rows only re-render on their own data changes.
  const sessions = useChatStore((s) => s.sessions);

  const active: string[] = [];
  const completed: string[] = [];
  const activeChannelGroups = new Map<string, string[]>();
  const completedChannelGroups = new Map<string, string[]>();

  for (const id of sessionIds) {
    const meta = sessions[id]?.meta;
    if (!meta) continue;

    if (meta.teamId) {
      const targetMap = meta.completedAt ? completedChannelGroups : activeChannelGroups;
      const group = targetMap.get(meta.teamId);
      if (group) group.push(id);
      else targetMap.set(meta.teamId, [id]);
      continue;
    }

    if (meta.completedAt) {
      completed.push(id);
    } else {
      active.push(id);
    }
  }

  // Merge channel groups: if all members are completed, show in completed section
  const channelGroups = new Map<string, { ids: string[]; allCompleted: boolean }>();
  for (const [teamId, ids] of activeChannelGroups) {
    const completedIds = completedChannelGroups.get(teamId);
    const allIds = completedIds ? [...ids, ...completedIds] : ids;
    channelGroups.set(teamId, { ids: allIds, allCompleted: false });
    completedChannelGroups.delete(teamId);
  }
  for (const [teamId, ids] of completedChannelGroups) {
    channelGroups.set(teamId, { ids, allCompleted: true });
  }

  const { promoted, rest } = partitionActiveSessions(active, sessions);
  const sortedCompleted = sortCompletedByDate(completed, sessions);

  const activeChannelEntries = [...channelGroups.entries()].filter(([, g]) => !g.allCompleted);
  const completedChannelEntries = [...channelGroups.entries()].filter(([, g]) => g.allCompleted);

  return (
    <>
      {newChatButton}
      {promoted.map((id) => (
        <SidebarSessionRow
          key={id}
          id={id}
          activeSessionId={activeSessionId}
          onSessionClick={onSessionClick}
        />
      ))}
      {promoted.length > 0 && rest.length > 0 && <div className="h-3" aria-hidden="true" />}
      {rest.map((id) => (
        <SidebarSessionRow
          key={id}
          id={id}
          activeSessionId={activeSessionId}
          onSessionClick={onSessionClick}
        />
      ))}
      {activeChannelEntries.map(([teamId, { ids }]) => (
        <ChannelGroup
          key={teamId}
          teamId={teamId}
          sessionIds={ids}
          activeSessionId={activeSessionId}
          onSessionClick={onSessionClick}
          projectSlug={projectSlug}
        />
      ))}
      {(sortedCompleted.length > 0 || completedChannelEntries.length > 0) && (
        <CompletedSection
          ids={sortedCompleted}
          channelEntries={completedChannelEntries}
          activeSessionId={activeSessionId}
          onSessionClick={onSessionClick}
          projectSlug={projectSlug}
        />
      )}
    </>
  );
}

function useChannelSessionCounts(sessionIds: string[]): ActiveSessionCounts {
  return useChatStore(
    useShallow((s) => {
      let running = 0;
      let pendingApproval = 0;
      let idle = 0;

      for (const id of sessionIds) {
        const data = s.sessions[id];
        if (!data) continue;
        if (data.meta.worktreeMerged) continue;
        if (data.pendingApproval || data.pendingQuestion) pendingApproval++;
        else if (data.meta.state === "running") running++;
        else if (data.meta.state === "idle") idle++;
      }

      return { running, pendingApproval, idle };
    }),
  );
}

function ChannelGroup({
  teamId,
  sessionIds,
  activeSessionId,
  onSessionClick,
  projectSlug,
}: {
  teamId: string;
  sessionIds: string[];
  activeSessionId: string | undefined;
  onSessionClick: (id: string) => void;
  projectSlug: string;
}) {
  const navigate = useNavigate();
  const [expanded, setExpanded] = useState(true);
  const teamName = useTeamStore((s) => s.teams[teamId]?.name ?? "Channel");
  const counts = useChannelSessionCounts(sessionIds);

  const handleHeaderClick = useCallback(() => {
    setExpanded((v) => !v);
  }, []);

  const handleNameClick = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      useAppStore.getState().setSidebarOpen(false);
      navigate({
        to: "/project/$projectSlug/channel/$channelId",
        params: { projectSlug, channelId: teamId },
      });
    },
    [navigate, projectSlug, teamId],
  );

  return (
    <div className="mt-1">
      <button
        type="button"
        onClick={handleHeaderClick}
        className="group flex w-full items-center gap-1 rounded-md px-2 py-1 text-left cursor-pointer hover:bg-sidebar-accent/50 transition-colors"
      >
        {expanded ? (
          <ChevronDown className="size-3 shrink-0 text-muted-foreground" />
        ) : (
          <ChevronRight className="size-3 shrink-0 text-muted-foreground" />
        )}
        <Hash className="size-3 shrink-0 text-muted-foreground/70" />
        <span
          onClick={handleNameClick}
          onKeyDown={(e) => {
            if (e.key === "Enter") handleNameClick(e as unknown as React.MouseEvent);
          }}
          className="text-sm font-medium text-muted-foreground truncate hover:text-sidebar-foreground hover:underline"
        >
          {teamName}
        </span>
        <span className="text-xs text-muted-foreground/60 ml-auto shrink-0">
          {sessionIds.length}
        </span>
        {!expanded && <ActiveSessionIndicators counts={counts} />}
      </button>
      {expanded && (
        <div className="ml-4">
          {sessionIds.map((id) => (
            <SidebarSessionRow
              key={id}
              id={id}
              activeSessionId={activeSessionId}
              onSessionClick={onSessionClick}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function CompletedSection({
  ids,
  channelEntries,
  activeSessionId,
  onSessionClick,
  projectSlug,
}: {
  ids: string[];
  channelEntries: [string, { ids: string[]; allCompleted: boolean }][];
  activeSessionId: string | undefined;
  onSessionClick: (id: string) => void;
  projectSlug: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const totalCount = ids.length + channelEntries.reduce((sum, [, g]) => sum + g.ids.length, 0);

  return (
    <>
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className="group mt-2 mb-1.5 flex w-full items-center gap-1 px-2 pt-1.5 text-left cursor-pointer border-t border-sidebar-border/30"
      >
        {expanded ? (
          <ChevronDown className="size-3 shrink-0 text-muted-foreground transition-transform" />
        ) : (
          <ChevronRight className="size-3 shrink-0 text-muted-foreground transition-transform" />
        )}
        <span className="text-xs font-medium tracking-wide text-muted-foreground/70 uppercase group-hover:text-muted-foreground">
          Completed
        </span>
        <span className="text-xs text-muted-foreground/60 ml-auto">{totalCount}</span>
      </button>
      {expanded && (
        <>
          {channelEntries.map(([teamId, { ids: channelIds }]) => (
            <ChannelGroup
              key={teamId}
              teamId={teamId}
              sessionIds={channelIds}
              activeSessionId={activeSessionId}
              onSessionClick={onSessionClick}
              projectSlug={projectSlug}
            />
          ))}
          {ids.map((id) => (
            <SidebarSessionRow
              key={id}
              id={id}
              activeSessionId={activeSessionId}
              onSessionClick={onSessionClick}
            />
          ))}
        </>
      )}
    </>
  );
}

interface ProjectTreeItemProps {
  project: Project;
  isActive: boolean;
  isExpanded: boolean;
  onToggleExpand: () => void;
  activeSessionId: string | undefined;
  isNewChatActive: boolean;
}

// --- Active session indicators ---

interface ActiveSessionCounts {
  running: number;
  pendingApproval: number;
  idle: number;
}

function useActiveSessionCounts(projectId: string): ActiveSessionCounts {
  return useChatStore(
    useShallow((s) => {
      let running = 0;
      let pendingApproval = 0;
      let idle = 0;

      for (const data of Object.values(s.sessions)) {
        if (data.meta.projectId !== projectId) continue;
        if (data.meta.worktreeMerged) continue;
        if (data.pendingApproval || data.pendingQuestion) pendingApproval++;
        else if (data.meta.state === "running") running++;
        else if (data.meta.state === "idle") idle++;
      }

      return { running, pendingApproval, idle };
    }),
  );
}

function ActiveSessionIndicators({ counts }: { counts: ActiveSessionCounts }) {
  if (counts.running === 0 && counts.pendingApproval === 0 && counts.idle === 0) return null;

  return (
    <span className="ml-auto flex items-center gap-1.5 shrink-0">
      {counts.pendingApproval > 0 && (
        <span
          className="flex items-center gap-0.5 text-xs text-orange"
          title={`${counts.pendingApproval} awaiting approval`}
        >
          <span className="inline-block h-1.5 w-1.5 rounded-full bg-orange animate-pulse" />
          {counts.pendingApproval}
        </span>
      )}
      {counts.running > 0 && (
        <span
          className="flex items-center gap-0.5 text-xs text-teal"
          title={`${counts.running} running`}
        >
          <span className="inline-block h-1.5 w-1.5 rounded-full bg-teal animate-pulse" />
          {counts.running}
        </span>
      )}
      {counts.idle > 0 && counts.running === 0 && counts.pendingApproval === 0 && (
        <span
          className="flex items-center gap-0.5 text-xs text-success/70"
          title={`${counts.idle} idle`}
        >
          <span className="inline-block h-1.5 w-1.5 rounded-full bg-success" />
          {counts.idle}
        </span>
      )}
    </span>
  );
}

function ProjectTagBadges({ projectId }: { projectId: string }) {
  const tags = useAppStore((s) => s.tags);
  const projectTags = useAppStore((s) => s.projectTags);
  const tagIds = projectTags.filter((pt) => pt.project_id === projectId).map((pt) => pt.tag_id);
  if (tagIds.length === 0) return null;

  return (
    <span className="flex items-center gap-1 shrink-0">
      {tagIds.map((id) => {
        const tag = tags.find((t) => t.id === id);
        if (!tag) return null;
        const color = getTagColor(tag.color);
        return (
          <span
            key={id}
            className="rounded-full px-1.5 py-px text-[10px] font-medium leading-tight"
            style={{ backgroundColor: `${color.bg}20`, color: color.bg }}
          >
            {tag.name}
          </span>
        );
      })}
    </span>
  );
}

export function ProjectTreeItem({
  project,
  isActive,
  isExpanded,
  onToggleExpand,
  activeSessionId,
  isNewChatActive,
}: ProjectTreeItemProps) {
  const navigate = useNavigate();
  const ws = useWebSocket();
  const gitStatus = useAppStore((s) => s.projectGitStatus[project.id]);
  const sessionCounts = useActiveSessionCounts(project.id);
  const isFavorite = project.favorite === 1;

  const [sessionsRef] = useAutoAnimate<HTMLDivElement>(ANIMATE_DEFAULT);
  const sessionIds = useChatStore(
    useShallow((s) =>
      Object.keys(s.sessions).filter((id) => s.sessions[id]?.meta.projectId === project.id),
    ),
  );

  const closeSidebar = () => useAppStore.getState().setSidebarOpen(false);

  const handleProjectClick = () => {
    onToggleExpand();
  };

  const handleToggleFavorite = useCallback(
    async (e: React.MouseEvent) => {
      e.stopPropagation();
      try {
        const updated = await setProjectFavorite(ws, project.id, !isFavorite);
        useAppStore.getState().updateProject(updated);
      } catch (err) {
        toast.error(getErrorMessage(err, "Failed to toggle favorite"));
      }
    },
    [ws, project.id, isFavorite],
  );

  const handleSessionClick = useCallback(
    (sessionId: string) => {
      useAppStore.getState().setSidebarOpen(false);
      navigate({
        to: "/project/$projectSlug/session/$sessionShortId",
        params: { projectSlug: project.slug, sessionShortId: sessionId.split("-")[0] ?? "" },
      });
    },
    [navigate, project.slug],
  );

  return (
    <div
      className={cn(
        "border-l-2 border-transparent",
        isExpanded && "pb-2",
        isActive && isExpanded && "border-l-sidebar-primary",
      )}
    >
      {/* Project header — row 1: name + path, row 2: git status */}
      <ProjectHoverCard projectId={project.id} projectPath={project.path} gitStatus={gitStatus}>
        {/* biome-ignore lint/a11y/useSemanticElements: div with role=button avoids nested button HTML issues */}
        <div
          role="button"
          tabIndex={0}
          onClick={handleProjectClick}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              handleProjectClick();
            }
          }}
          className={cn(
            "w-full text-left px-3 py-1.5 max-md:py-2.5 group bg-sidebar-accent/50 hover:bg-sidebar-accent transition-colors cursor-pointer",
            isActive && "bg-sidebar-accent",
          )}
        >
          <div className="flex gap-1.5">
            <div className="flex items-center shrink-0">
              {isExpanded ? (
                <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
              ) : (
                <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />
              )}
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5">
                {isExpanded && (
                  <button
                    type="button"
                    onClick={handleToggleFavorite}
                    className={cn(
                      "shrink-0 transition-colors cursor-pointer",
                      isFavorite
                        ? "text-muted-foreground/60"
                        : "text-muted-foreground/30 opacity-0 group-hover:opacity-100",
                    )}
                    title={isFavorite ? "Remove from favorites" : "Add to favorites"}
                  >
                    <Star className="size-3.5" fill={isFavorite ? "currentColor" : "none"} />
                  </button>
                )}
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    navigate({
                      to: "/project/$projectSlug/settings",
                      params: { projectSlug: project.slug },
                    });
                  }}
                  className="text-base font-medium shrink-0 text-foreground-bright hover:underline"
                >
                  {project.name}
                </button>
                {isExpanded && (
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      closeSidebar();
                      navigate({
                        to: "/project/$projectSlug/files",
                        params: { projectSlug: project.slug },
                      });
                    }}
                    className="shrink-0 text-muted-foreground/30 opacity-0 group-hover:opacity-100 transition-colors hover:text-foreground cursor-pointer"
                    title="Browse files"
                  >
                    <FolderOpen className="size-3.5" />
                  </button>
                )}
                {isExpanded ? (
                  <>
                    <ProjectTagBadges projectId={project.id} />
                    <ActiveSessionIndicators counts={sessionCounts} />
                  </>
                ) : (
                  sessionCounts.pendingApproval > 0 && (
                    <span
                      className="ml-auto inline-block h-2 w-2 rounded-full bg-orange animate-pulse shrink-0"
                      title={`${sessionCounts.pendingApproval} awaiting approval`}
                    />
                  )
                )}
              </div>
            </div>
          </div>
        </div>
      </ProjectHoverCard>

      {/* Sessions + new chat */}
      {isExpanded && (
        <div ref={sessionsRef} className="ml-6 mr-2 mt-1 space-y-1">
          <SessionGroups
            sessionIds={sessionIds}
            activeSessionId={activeSessionId}
            onSessionClick={handleSessionClick}
            projectSlug={project.slug}
            newChatButton={
              <button
                type="button"
                onClick={() => {
                  closeSidebar();
                  navigate({
                    to: "/project/$projectSlug/session/new",
                    params: { projectSlug: project.slug },
                  });
                }}
                className={cn(
                  "flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 max-md:py-2.5 text-sm text-muted-foreground hover:text-sidebar-foreground hover:bg-sidebar-accent/50 transition-colors cursor-pointer",
                  isNewChatActive && "bg-sidebar-accent/70 text-sidebar-foreground",
                )}
              >
                <Plus className="h-3.5 w-3.5" />
                <span>New chat</span>
              </button>
            }
          />
        </div>
      )}
    </div>
  );
}
