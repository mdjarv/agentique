/**
 * Variant C4: "Collapsible Flat"
 * C3's thin project dividers and minimal chrome, but projects are
 * collapsible. Chevron on the divider row. Default: expanded if
 * project has active sessions. Collapsed projects show a compact
 * session count + attention badge inline.
 */
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  ChevronRight,
  GitBranch,
  Plus,
  Settings,
} from "lucide-react";
import { memo, useCallback, useMemo, useState } from "react";
import { useShallow } from "zustand/shallow";
import { useProjectGitActions } from "~/hooks/git/useProjectGitActions";
import { useProjectIcon } from "~/hooks/useProjectIcon";
import { useTheme } from "~/hooks/useTheme";
import { getProjectColor, type ProjectColor } from "~/lib/project-colors";
import { getWorstSessionState } from "~/lib/session/priority";
import type { Project } from "~/lib/types";
import { cn, relativeTime } from "~/lib/utils";
import { type ProjectGitStatus, useAppStore } from "~/stores/app-store";
import { type SessionData, useChatStore } from "~/stores/chat-store";
import { PulseStatus } from "../session/PulseStatus";
import { type BadgeState, SessionBadge } from "../session/SessionBadge";
import { SessionStatusBadge } from "../session/SessionStatusBadge";

// --- Session row ---

const SessionRow = memo(function SessionRow({
  sessionId,
  data,
  isActive,
  onClick,
}: {
  sessionId: string;
  data: SessionData;
  isActive: boolean;
  onClick: (id: string) => void;
}) {
  const { meta } = data;
  const todoDone = data.todos?.filter((t) => t.status === "completed").length ?? 0;
  const todoTotal = data.todos?.length ?? 0;
  const time = meta.completedAt
    ? relativeTime(meta.completedAt)
    : meta.lastQueryAt
      ? relativeTime(meta.lastQueryAt)
      : meta.updatedAt
        ? relativeTime(meta.updatedAt)
        : "";

  const isTerminal = meta.state === "done" || meta.state === "stopped" || meta.state === "failed";
  const faded = isTerminal && meta.worktreeMerged;
  const todosInProgress = todoTotal > 0 && todoDone < todoTotal;
  const pct = todoTotal > 0 ? (todoDone / todoTotal) * 100 : 0;

  return (
    <button
      type="button"
      onClick={() => onClick(sessionId)}
      className={cn(
        "flex items-center gap-1.5 w-full pl-7 pr-2 py-1.5 text-left transition-colors cursor-pointer text-xs rounded-sm",
        "hover:bg-sidebar-accent/50",
        isActive && "bg-sidebar-accent rounded-md",
      )}
      style={
        todosInProgress
          ? {
              background: `linear-gradient(to right, rgba(16,185,129,0.15) ${pct}%, transparent ${pct}%)`,
            }
          : undefined
      }
    >
      <SessionStatusBadge
        state={meta.state}
        connected={meta.connected}
        hasUnseenCompletion={data.hasUnseenCompletion}
        hasPendingApproval={!!(data.pendingApproval || data.pendingQuestion)}
        isPlanning={data.planMode}
        gitOperation={meta.gitOperation}
      />
      <div className="truncate flex-1 min-w-0">
        <span
          className={cn(
            "block truncate",
            !meta.name && "italic text-muted-foreground",
            faded && "text-muted-foreground line-through decoration-muted-foreground/50",
            data.hasUnseenCompletion && "font-semibold text-foreground-bright",
          )}
        >
          {meta.name || "Untitled"}
        </span>
        {meta.state === "running" && <PulseStatus sessionId={sessionId} />}
      </div>
      <span className="flex items-center gap-1.5 shrink-0 ml-auto">
        {todosInProgress && (
          <span className="text-[10px] text-muted-foreground tabular-nums">
            {todoDone}/{todoTotal}
          </span>
        )}
        {meta.commitsAhead > 0 && !meta.worktreeMerged && (
          <span className="text-[9px] text-success flex items-center">
            <ArrowUp className="size-2" />
            {meta.commitsAhead}
          </span>
        )}
        <span className="text-[9px] tabular-nums text-muted-foreground-faint">{time}</span>
      </span>
    </button>
  );
});

// --- Collapsible project divider ---

const ProjectDivider = memo(function ProjectDivider({
  project,
  color,
  gitStatus,
  worstState,
  expanded,
  onToggle,
  onExpand,
}: {
  project: Project;
  color: ProjectColor;
  gitStatus: ProjectGitStatus | undefined;
  worstState: BadgeState | null;
  expanded: boolean;
  onToggle: () => void;
  onExpand: () => void;
}) {
  const navigate = useNavigate();
  const Icon = useProjectIcon(project.icon);
  const { pushing, pulling, handlePush, handlePull } = useProjectGitActions(project.id);
  const initials = project.slug
    .split("-")
    .map((w) => w[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);

  const ahead = gitStatus && gitStatus.aheadRemote > 0;
  const behind = gitStatus && gitStatus.behindRemote > 0;

  const handleNewSession = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      onExpand();
      useAppStore.getState().setSidebarOpen(false);
      navigate({ to: "/project/$projectSlug/session/new", params: { projectSlug: project.slug } });
    },
    [navigate, project.slug, onExpand],
  );

  return (
    <div className="mt-6 first:mt-0 rounded-md" style={{ backgroundColor: `${color.bg}15` }}>
      {/* Row 1: toggle + name + settings ... new session */}
      <div className="group/divider flex items-center gap-1 hover:bg-sidebar-accent/30 transition-colors rounded-md">
        <button
          type="button"
          onClick={onToggle}
          className="flex items-center gap-1.5 min-w-0 px-2 py-2 cursor-pointer"
        >
          {expanded ? (
            <ChevronDown className="size-2.5 text-muted-foreground shrink-0" />
          ) : (
            <ChevronRight className="size-2.5 text-muted-foreground shrink-0" />
          )}
          <span
            className="size-4 rounded flex items-center justify-center text-[8px] font-bold shrink-0"
            style={{ backgroundColor: `${color.bg}20`, color: color.fg }}
          >
            {Icon ? <Icon className="size-2.5" /> : initials}
          </span>
          <span className="text-sm font-semibold truncate" style={{ color: color.fg }}>
            {project.name}
          </span>

          {/* Collapsed: worst-state badge */}
          {!expanded && worstState && <SessionBadge state={worstState} size="md" pulse />}
        </button>

        <span className="flex items-center gap-0.5 ml-auto mr-1.5 opacity-0 group-hover/divider:opacity-100 transition-opacity">
          <Link
            to="/project/$projectSlug/settings"
            params={{ projectSlug: project.slug }}
            onClick={(e) => e.stopPropagation()}
            className="size-5 flex items-center justify-center rounded text-muted-foreground-faint hover:text-foreground transition-colors shrink-0"
          >
            <Settings className="size-3" />
          </Link>

          <button
            type="button"
            onClick={handleNewSession}
            className="size-5 rounded-md flex items-center justify-center cursor-pointer transition-all hover:scale-110 active:scale-95 shrink-0"
            style={{
              color: color.fg,
              backgroundColor: `${color.bg}25`,
            }}
            onMouseEnter={(e) => {
              e.currentTarget.style.backgroundColor = `${color.bg}40`;
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.backgroundColor = `${color.bg}25`;
            }}
          >
            <Plus className="size-3" />
          </button>
        </span>
      </div>

      {/* Row 2: git status sub-line (only when expanded, same bg tint) */}
      {expanded && gitStatus?.branch && (
        <div className="flex items-center gap-1.5 pl-7 pr-2 pb-2">
          {/* Left: branch + counters + dirty */}
          <span className="inline-flex items-center gap-1 text-[11px] text-muted-foreground truncate">
            <GitBranch className="size-3 shrink-0" />
            <span className="truncate">{gitStatus.branch}</span>
            {ahead && <span className="text-success shrink-0">↑{gitStatus.aheadRemote}</span>}
            {behind && <span className="text-orange shrink-0">↓{gitStatus.behindRemote}</span>}
          </span>
          {gitStatus.uncommittedCount > 0 && (
            <span className="text-[10px] text-warning font-medium">
              {gitStatus.uncommittedCount} dirty
            </span>
          )}

          {/* Right: action buttons */}
          {(ahead || behind) && (
            <span className="flex items-center gap-1 ml-auto shrink-0">
              {ahead && (
                <button
                  type="button"
                  onClick={() => handlePush()}
                  disabled={pushing}
                  className="inline-flex items-center gap-1 text-[10px] font-medium px-1.5 py-0.5 rounded border border-success/30 bg-success/10 text-success hover:bg-success/20 transition-colors disabled:opacity-50 cursor-pointer"
                >
                  <ArrowUp className="size-2.5" />
                  {pushing ? "Pushing..." : "Push"}
                </button>
              )}
              {behind && (
                <button
                  type="button"
                  onClick={() => handlePull()}
                  disabled={pulling}
                  className="inline-flex items-center gap-1 text-[10px] font-medium px-1.5 py-0.5 rounded border border-orange/30 bg-orange/10 text-orange hover:bg-orange/20 transition-colors disabled:opacity-50 cursor-pointer"
                >
                  <ArrowDown className="size-2.5" />
                  {pulling ? "Pulling..." : "Pull"}
                </button>
              )}
            </span>
          )}
        </div>
      )}
    </div>
  );
});

// --- Main ---

export function VariantC4() {
  const navigate = useNavigate();
  const projects = useAppStore((s) => s.projects);
  const projectIds = useAppStore(useShallow((s) => s.projects.map((p) => p.id)));
  const projectGitStatus = useAppStore((s) => s.projectGitStatus);
  const sessions = useChatStore((s) => s.sessions);
  const { resolvedTheme } = useTheme();
  const [showCompleted, setShowCompleted] = useState(false);

  const params = useParams({ strict: false }) as { projectSlug?: string; sessionShortId?: string };
  const activeSessionId = useChatStore((s) => {
    const shortId = params.sessionShortId;
    if (!shortId) return undefined;
    return Object.keys(s.sessions).find((id) => id.startsWith(shortId));
  });

  const { groups, totalCompleted } = useMemo(() => {
    const result: Array<{
      project: Project;
      color: ProjectColor;
      gitStatus: ProjectGitStatus | undefined;
      active: Array<{ id: string; data: SessionData }>;
      completed: Array<{ id: string; data: SessionData }>;
      worstState: BadgeState | null;
    }> = [];
    let completedCount = 0;

    for (const project of projects) {
      const color = getProjectColor(project.color, project.id, projectIds, resolvedTheme);
      const gitStatus = projectGitStatus[project.id];
      const active: Array<{ id: string; data: SessionData }> = [];
      const completed: Array<{ id: string; data: SessionData }> = [];

      for (const [id, data] of Object.entries(sessions) as [string, SessionData][]) {
        if (data.meta.projectId !== project.id) continue;
        if (data.meta.completedAt) {
          completed.push({ id, data });
        } else {
          active.push({ id, data });
        }
      }

      active.sort((a, b) => {
        const aPri = getSessionPriority(a.data);
        const bPri = getSessionPriority(b.data);
        if (aPri !== bPri) return aPri - bPri;
        return (
          new Date(b.data.meta.updatedAt ?? b.data.meta.createdAt).getTime() -
          new Date(a.data.meta.updatedAt ?? a.data.meta.createdAt).getTime()
        );
      });

      const worstState = getWorstSessionState(active);
      completedCount += completed.length;
      result.push({ project, color, gitStatus, active, completed, worstState });
    }

    result.sort((a, b) => {
      const aHas = a.active.length > 0 ? 1 : 0;
      const bHas = b.active.length > 0 ? 1 : 0;
      if (aHas !== bHas) return bHas - aHas;
      if (a.project.favorite !== b.project.favorite) return b.project.favorite - a.project.favorite;
      return a.project.name.localeCompare(b.project.name);
    });

    return { groups: result, totalCompleted: completedCount };
  }, [projects, projectIds, projectGitStatus, sessions, resolvedTheme]);

  // Expanded state — default expanded if has active sessions
  const [expandedMap, setExpandedMap] = useState<Record<string, boolean>>({});
  const isExpanded = useCallback(
    (projectId: string, hasActive: boolean) => {
      if (projectId in expandedMap) return expandedMap[projectId] ?? false;
      return hasActive;
    },
    [expandedMap],
  );
  const toggleExpanded = useCallback((projectId: string, currentExpanded: boolean) => {
    setExpandedMap((prev) => ({ ...prev, [projectId]: !currentExpanded }));
  }, []);
  const expandProject = useCallback((projectId: string) => {
    setExpandedMap((prev) => ({ ...prev, [projectId]: true }));
  }, []);

  const handleSessionClick = useCallback(
    (sessionId: string) => {
      const data = sessions[sessionId];
      if (!data) return;
      const project = projects.find((p) => p.id === data.meta.projectId);
      if (!project) return;
      useAppStore.getState().setSidebarOpen(false);
      navigate({
        to: "/project/$projectSlug/session/$sessionShortId",
        params: { projectSlug: project.slug, sessionShortId: sessionId.split("-")[0] ?? "" },
      });
    },
    [navigate, projects, sessions],
  );

  return (
    <div className="flex-1 flex flex-col min-w-0 min-h-0">
      <div className="flex-1 overflow-y-auto min-h-0 py-2 px-1.5">
        {groups.map(({ project, color, gitStatus, active, completed, worstState }) => {
          const expanded = isExpanded(project.id, active.length > 0);
          return (
            <div key={project.id}>
              <ProjectDivider
                project={project}
                color={color}
                gitStatus={gitStatus}
                worstState={worstState}
                expanded={expanded}
                onToggle={() => toggleExpanded(project.id, expanded)}
                onExpand={() => expandProject(project.id)}
              />
              {expanded && (
                <>
                  {active.map(({ id, data }) => (
                    <SessionRow
                      key={id}
                      sessionId={id}
                      data={data}
                      isActive={id === activeSessionId}
                      onClick={handleSessionClick}
                    />
                  ))}
                  {showCompleted &&
                    completed.map(({ id, data }) => (
                      <SessionRow
                        key={id}
                        sessionId={id}
                        data={data}
                        isActive={id === activeSessionId}
                        onClick={handleSessionClick}
                      />
                    ))}
                </>
              )}
            </div>
          );
        })}
      </div>

      {totalCompleted > 0 && (
        <div className="px-3 py-1.5 border-t border-border/50">
          <button
            type="button"
            onClick={() => setShowCompleted((v) => !v)}
            className="text-[10px] text-muted-foreground-faint hover:text-muted-foreground transition-colors cursor-pointer"
          >
            {showCompleted
              ? `Hide ${totalCompleted} completed`
              : `Show ${totalCompleted} completed`}
          </button>
        </div>
      )}
    </div>
  );
}

function getSessionPriority(data: SessionData): number {
  if (data.pendingApproval || data.pendingQuestion) return 0;
  if (data.meta.state === "running") return 1;
  if (data.meta.state === "idle" && !data.meta.completedAt) return 2;
  if (data.meta.state === "failed") return 3;
  return 10;
}
