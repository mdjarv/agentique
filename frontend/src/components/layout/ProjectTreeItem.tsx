import { useNavigate } from "@tanstack/react-router";
import {
  ArrowDown,
  ArrowUp,
  ArrowUpToLine,
  ChevronDown,
  ChevronRight,
  FolderOpen,
  GitBranch,
  Loader2,
  Plus,
  RefreshCw,
} from "lucide-react";
import { type ReactNode, useCallback, useState } from "react";
import { toast } from "sonner";
import { useShallow } from "zustand/shallow";
import { useWebSocket } from "~/hooks/useWebSocket";
import { fetchProject, pushProject } from "~/lib/project-actions";
import type { Project } from "~/lib/types";
import { cn } from "~/lib/utils";
import { type ProjectGitStatus, useAppStore } from "~/stores/app-store";
import { type ChatState, useChatStore } from "~/stores/chat-store";
import { SessionHoverCard } from "./SessionHoverCard";
import { SessionRow } from "./SessionRow";

const activePriority: Record<string, number> = {
  running: 0,
  merging: 1,
  idle: 2,
};

function sortByPriorityThenDate(
  ids: string[],
  sessions: ChatState["sessions"],
  priority: Record<string, number>,
): string[] {
  return [...ids].sort((a, b) => {
    const sa = sessions[a]?.meta;
    const sb = sessions[b]?.meta;
    if (!sa || !sb) return 0;
    const pa = priority[sa.state] ?? 99;
    const pb = priority[sb.state] ?? 99;
    if (pa !== pb) return pa - pb;
    return new Date(sb.createdAt).getTime() - new Date(sa.createdAt).getTime();
  });
}

function sortCompletedByDate(ids: string[], sessions: ChatState["sessions"]): string[] {
  return [...ids].sort((a, b) => {
    const ma = sessions[a]?.meta;
    const mb = sessions[b]?.meta;
    const ta = new Date(ma?.updatedAt ?? ma?.createdAt ?? 0).getTime();
    const tb = new Date(mb?.updatedAt ?? mb?.createdAt ?? 0).getTime();
    return tb - ta;
  });
}

function renderSessionRow(
  id: string,
  sessions: ChatState["sessions"],
  activeSessionId: string | undefined,
  onSessionClick: (id: string) => void,
) {
  const session = sessions[id]?.meta;
  if (!session) return null;
  return (
    <SessionHoverCard key={id} sessionId={id}>
      <SessionRow
        name={session.name}
        state={session.state}
        connected={session.connected}
        hasUnseenCompletion={sessions[id]?.hasUnseenCompletion}
        hasPendingApproval={!!sessions[id]?.pendingApproval || !!sessions[id]?.pendingQuestion}
        isPlanning={!!sessions[id]?.planMode}
        isActive={id === activeSessionId}
        worktreeBranch={session.worktreeBranch}
        hasDirtyWorktree={session.hasDirtyWorktree}
        worktreeMerged={session.worktreeMerged}
        commitsAhead={session.commitsAhead}
        commitsBehind={session.commitsBehind}
        branchMissing={session.branchMissing}
        hasUncommitted={session.hasUncommitted}
        mergeStatus={session.mergeStatus}
        gitOperation={session.gitOperation}
        prUrl={session.prUrl}
        onClick={() => onSessionClick(id)}
      />
    </SessionHoverCard>
  );
}

function SessionGroups({
  sessionIds,
  sessions,
  activeSessionId,
  onSessionClick,
  newChatButton,
}: {
  sessionIds: string[];
  sessions: ChatState["sessions"];
  activeSessionId: string | undefined;
  onSessionClick: (id: string) => void;
  newChatButton: ReactNode;
}) {
  const active: string[] = [];
  const completed: string[] = [];

  for (const id of sessionIds) {
    const meta = sessions[id]?.meta;
    if (!meta) continue;
    if (meta.worktreeMerged) {
      completed.push(id);
    } else {
      active.push(id);
    }
  }

  const sortedActive = sortByPriorityThenDate(active, sessions, activePriority);
  const sortedCompleted = sortCompletedByDate(completed, sessions);

  return (
    <>
      {newChatButton}
      {sortedActive.map((id) => renderSessionRow(id, sessions, activeSessionId, onSessionClick))}
      {sortedCompleted.length > 0 && (
        <CompletedSection
          ids={sortedCompleted}
          sessions={sessions}
          activeSessionId={activeSessionId}
          onSessionClick={onSessionClick}
        />
      )}
    </>
  );
}

function CompletedSection({
  ids,
  sessions,
  activeSessionId,
  onSessionClick,
}: {
  ids: string[];
  sessions: ChatState["sessions"];
  activeSessionId: string | undefined;
  onSessionClick: (id: string) => void;
}) {
  const [expanded, setExpanded] = useState(false);

  return (
    <>
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className="group mt-2 mb-0.5 flex w-full items-center gap-1 px-2 text-left cursor-pointer"
      >
        {expanded ? (
          <ChevronDown className="size-3 shrink-0 text-muted-foreground transition-transform" />
        ) : (
          <ChevronRight className="size-3 shrink-0 text-muted-foreground transition-transform" />
        )}
        <span className="text-xs font-semibold tracking-widest text-muted-foreground/70 uppercase group-hover:text-muted-foreground">
          Completed
        </span>
        <span className="text-xs text-muted-foreground/60">{ids.length}</span>
      </button>
      {expanded && ids.map((id) => renderSessionRow(id, sessions, activeSessionId, onSessionClick))}
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

function truncatePath(path: string): string {
  return path.replace(/^\/home\/[^/]+/, "~").replace(/^\/Users\/[^/]+/, "~");
}

function shouldShowPath(name: string, path: string): boolean {
  const lastSegment = path.split("/").filter(Boolean).pop() ?? "";
  return lastSegment !== name;
}

// --- Project git status row ---

function ProjectGitStatusRow({
  gitStatus,
  projectId,
}: {
  gitStatus: ProjectGitStatus;
  projectId: string;
}) {
  const ws = useWebSocket();
  const [pushing, setPushing] = useState(false);
  const [fetching, setFetching] = useState(false);

  const handlePush = useCallback(
    async (e: React.MouseEvent) => {
      e.stopPropagation();
      setPushing(true);
      try {
        const status = await pushProject(ws, projectId);
        useAppStore.getState().setProjectGitStatus(status);
        toast.success("Pushed");
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Push failed");
      } finally {
        setPushing(false);
      }
    },
    [ws, projectId],
  );

  const handleFetch = useCallback(
    async (e: React.MouseEvent) => {
      e.stopPropagation();
      setFetching(true);
      try {
        const status = await fetchProject(ws, projectId);
        useAppStore.getState().setProjectGitStatus(status);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Fetch failed");
      } finally {
        setFetching(false);
      }
    },
    [ws, projectId],
  );

  const ahead = gitStatus.aheadRemote > 0;
  const behind = gitStatus.behindRemote > 0;
  const dirty = gitStatus.uncommittedCount > 0;
  const hasAnything = ahead || behind || dirty;

  return (
    <div className="flex items-center gap-1.5 pl-7 pr-2 pb-1 text-xs text-muted-foreground">
      <GitBranch className="h-3 w-3 shrink-0 text-muted-foreground" />
      <span className="font-mono truncate text-foreground/80">{gitStatus.branch}</span>

      {hasAnything && (
        <span className="flex items-center gap-1.5 ml-auto shrink-0">
          {dirty && (
            <span
              className="flex items-center gap-0.5 text-[#e0af68]/80"
              title={`${gitStatus.uncommittedCount} uncommitted`}
            >
              <span className="text-[0.5rem] leading-none">&#9679;</span>
              {gitStatus.uncommittedCount}
            </span>
          )}
          {ahead && (
            <span className="flex items-center gap-0.5" title={`${gitStatus.aheadRemote} ahead`}>
              <ArrowUp className="size-2.5" />
              {gitStatus.aheadRemote}
            </span>
          )}
          {behind && (
            <span
              className="flex items-center gap-0.5 text-[#7aa2f7]/80"
              title={`${gitStatus.behindRemote} behind`}
            >
              <ArrowDown className="size-2.5" />
              {gitStatus.behindRemote}
            </span>
          )}
        </span>
      )}

      {/* Action buttons — always visible when relevant */}
      {gitStatus.hasRemote && (
        <span className={cn("flex items-center gap-0.5 shrink-0", !hasAnything && "ml-auto")}>
          {ahead && (
            <button
              type="button"
              onClick={handlePush}
              disabled={pushing}
              className="p-0.5 rounded hover:bg-muted hover:text-foreground transition-colors"
              title="Push"
            >
              {pushing ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                <ArrowUpToLine className="h-3 w-3" />
              )}
            </button>
          )}
          <button
            type="button"
            onClick={handleFetch}
            disabled={fetching}
            className="p-0.5 rounded hover:bg-muted hover:text-foreground transition-colors"
            title="Fetch"
          >
            {fetching ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <RefreshCw className="h-3 w-3" />
            )}
          </button>
        </span>
      )}
    </div>
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
  const gitStatus = useAppStore((s) => s.projectGitStatus[project.id]);

  const sessionIds = useChatStore(
    useShallow((s) =>
      Object.keys(s.sessions).filter((id) => s.sessions[id]?.meta.projectId === project.id),
    ),
  );
  const sessions = useChatStore((s) => s.sessions);

  const closeSidebar = () => useAppStore.getState().setSidebarOpen(false);

  const handleProjectClick = () => {
    onToggleExpand();
    if (!isActive) {
      navigate({ to: "/project/$projectSlug", params: { projectSlug: project.slug } });
    }
  };

  const handleSessionClick = (sessionId: string) => {
    closeSidebar();
    navigate({
      to: "/project/$projectSlug/session/$sessionShortId",
      params: { projectSlug: project.slug, sessionShortId: sessionId.split("-")[0] ?? "" },
    });
  };

  return (
    <div>
      {/* Project header — row 1: name + path, row 2: git status */}
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
          "w-full text-left rounded-md px-2 pt-1.5 max-md:pt-2.5 group hover:bg-sidebar-accent transition-colors cursor-pointer",
          gitStatus?.branch ? "pb-0.5" : "pb-1.5 max-md:pb-2.5",
          isActive && "bg-sidebar-accent",
        )}
      >
        <div className="flex items-center gap-1.5">
          {isExpanded ? (
            <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          ) : (
            <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          )}
          <FolderOpen className="h-4 w-4 shrink-0" />
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              navigate({
                to: "/project/$projectSlug/settings",
                params: { projectSlug: project.slug },
              });
            }}
            className="text-sm font-medium shrink-0 text-foreground-bright hover:underline"
          >
            {project.name}
          </button>
          {shouldShowPath(project.name, project.path) && (
            <span className="text-xs text-muted-foreground min-w-0 overflow-hidden text-ellipsis whitespace-nowrap flex-1">
              {truncatePath(project.path)}
            </span>
          )}
        </div>
      </div>

      {/* Git status row */}
      {gitStatus?.branch && <ProjectGitStatusRow gitStatus={gitStatus} projectId={project.id} />}

      {/* Sessions + new chat */}
      {isExpanded && (
        <div className="ml-4 mt-0.5 space-y-0.5">
          <SessionGroups
            sessionIds={sessionIds}
            sessions={sessions}
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
                  "flex w-full items-center gap-1.5 rounded-md border border-dashed border-sidebar-foreground/15 px-2 py-1.5 max-md:py-2.5 text-sm text-sidebar-foreground/60 hover:text-sidebar-foreground hover:border-sidebar-foreground/30 hover:bg-sidebar-accent/40 transition-colors cursor-pointer",
                  isNewChatActive &&
                    "border-solid border-[#7aa2f7]/40 bg-[#7aa2f7]/10 text-sidebar-foreground",
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
