import { useNavigate } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, GitBranch, Plus, Star } from "lucide-react";
import { type ReactNode, memo, useCallback, useState } from "react";
import { toast } from "sonner";
import { useShallow } from "zustand/shallow";
import { useWebSocket } from "~/hooks/useWebSocket";
import { setProjectFavorite } from "~/lib/project-actions";
import { getTagColor } from "~/lib/tag-colors";
import type { Project } from "~/lib/types";
import { cn, getErrorMessage } from "~/lib/utils";
import { type ProjectGitStatus, useAppStore } from "~/stores/app-store";
import { type ChatState, type SessionData, useChatStore } from "~/stores/chat-store";
import { useUIStore } from "~/stores/ui-store";
import { GitIndicators } from "./GitIndicators";
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
        worktreeBranch={meta.worktreeBranch}
        hasDirtyWorktree={meta.hasDirtyWorktree}
        worktreeMerged={meta.worktreeMerged}
        commitsAhead={meta.commitsAhead}
        commitsBehind={meta.commitsBehind}
        branchMissing={meta.branchMissing}
        hasUncommitted={meta.hasUncommitted}
        mergeStatus={meta.mergeStatus}
        gitOperation={meta.gitOperation}
        prUrl={meta.prUrl}
        onClick={handleClick}
      />
    </SessionHoverCard>
  );
});

function SessionGroups({
  sessionIds,
  activeSessionId,
  onSessionClick,
  newChatButton,
}: {
  sessionIds: string[];
  activeSessionId: string | undefined;
  onSessionClick: (id: string) => void;
  newChatButton: ReactNode;
}) {
  // Subscribed here (not in ProjectTreeItem) so the parent doesn't re-render on turn events.
  // Sorting is cheap; SidebarSessionRow is memo'd so rows only re-render on their own data changes.
  const sessions = useChatStore((s) => s.sessions);

  const active: string[] = [];
  const completed: string[] = [];

  for (const id of sessionIds) {
    const meta = sessions[id]?.meta;
    if (!meta) continue;
    if (meta.completedAt) {
      completed.push(id);
    } else {
      active.push(id);
    }
  }

  const { promoted, rest } = partitionActiveSessions(active, sessions);
  const sortedCompleted = sortCompletedByDate(completed, sessions);

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
      {promoted.length > 0 && rest.length > 0 && <div className="h-2" aria-hidden="true" />}
      {rest.map((id) => (
        <SidebarSessionRow
          key={id}
          id={id}
          activeSessionId={activeSessionId}
          onSessionClick={onSessionClick}
        />
      ))}
      {sortedCompleted.length > 0 && (
        <CompletedSection
          ids={sortedCompleted}
          activeSessionId={activeSessionId}
          onSessionClick={onSessionClick}
        />
      )}
    </>
  );
}

function CompletedSection({
  ids,
  activeSessionId,
  onSessionClick,
}: {
  ids: string[];
  activeSessionId: string | undefined;
  onSessionClick: (id: string) => void;
}) {
  const [expanded, setExpanded] = useState(false);

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
        <span className="text-xs font-semibold tracking-widest text-muted-foreground/70 uppercase group-hover:text-muted-foreground">
          Completed
        </span>
        <span className="text-xs text-muted-foreground/60 ml-auto">{ids.length}</span>
      </button>
      {expanded &&
        ids.map((id) => (
          <SidebarSessionRow
            key={id}
            id={id}
            activeSessionId={activeSessionId}
            onSessionClick={onSessionClick}
          />
        ))}
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

// --- Project git status row ---

function ProjectGitStatusRow({ gitStatus }: { gitStatus: ProjectGitStatus }) {
  return (
    <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
      <GitBranch className="h-3 w-3 shrink-0 text-muted-foreground" />
      <span className="font-mono truncate text-foreground/80">{gitStatus.branch}</span>
      <GitIndicators
        uncommittedCount={gitStatus.uncommittedCount}
        aheadCount={gitStatus.aheadRemote}
        behindCount={gitStatus.behindRemote}
        className="ml-auto"
      />
    </div>
  );
}

function ProjectTagDots({ projectId }: { projectId: string }) {
  const tags = useAppStore((s) => s.tags);
  const projectTags = useAppStore((s) => s.projectTags);
  const tagIds = projectTags.filter((pt) => pt.project_id === projectId).map((pt) => pt.tag_id);
  if (tagIds.length === 0) return null;

  return (
    <span className="flex items-center gap-0.5 shrink-0">
      {tagIds.map((id) => {
        const tag = tags.find((t) => t.id === id);
        if (!tag) return null;
        const color = getTagColor(tag.color);
        return (
          <span
            key={id}
            className="inline-block size-2 rounded-full"
            style={{ backgroundColor: color.bg }}
            title={tag.name}
          />
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
                <button
                  type="button"
                  onClick={handleToggleFavorite}
                  className={cn(
                    "shrink-0 transition-colors cursor-pointer",
                    isFavorite
                      ? "text-warning"
                      : "text-muted-foreground/30 opacity-0 group-hover:opacity-100",
                  )}
                  title={isFavorite ? "Remove from favorites" : "Add to favorites"}
                >
                  <Star className="size-3.5" fill={isFavorite ? "currentColor" : "none"} />
                </button>
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
                <ProjectTagDots projectId={project.id} />
                <ActiveSessionIndicators counts={sessionCounts} />
              </div>
              {isExpanded && gitStatus?.branch && <ProjectGitStatusRow gitStatus={gitStatus} />}
            </div>
          </div>
        </div>
      </ProjectHoverCard>

      {/* Sessions + new chat */}
      {isExpanded && (
        <div className="ml-4 mr-2 mt-1 space-y-0.5">
          <SessionGroups
            sessionIds={sessionIds}
            activeSessionId={activeSessionId}
            onSessionClick={handleSessionClick}
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
