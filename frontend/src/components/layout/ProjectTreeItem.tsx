import { useNavigate } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, FolderOpen, GitBranch, Plus } from "lucide-react";
import { type ReactNode, useState } from "react";
import { useShallow } from "zustand/shallow";
import type { Project } from "~/lib/types";
import { cn } from "~/lib/utils";
import { type ProjectGitStatus, useAppStore } from "~/stores/app-store";
import { type ChatState, type SessionData, useChatStore } from "~/stores/chat-store";
import { GitIndicators } from "./GitIndicators";
import { ProjectHoverCard } from "./ProjectHoverCard";
import { SessionHoverCard } from "./SessionHoverCard";
import { SessionRow } from "./SessionRow";

function sessionSortKey(data: SessionData): [number, number, number] {
  const needsInput = data.pendingApproval || data.pendingQuestion ? 0 : 1;
  const unseen = data.hasUnseenCompletion ? 0 : 1;
  const recency = -new Date(data.meta.lastQueryAt ?? data.meta.createdAt).getTime();
  return [needsInput, unseen, recency];
}

function sortActiveSessions(ids: string[], sessions: ChatState["sessions"]): string[] {
  return [...ids].sort((a, b) => {
    const da = sessions[a];
    const db = sessions[b];
    if (!da || !db) return 0;
    const [a0, a1, a2] = sessionSortKey(da);
    const [b0, b1, b2] = sessionSortKey(db);
    return a0 - b0 || a1 - b1 || a2 - b2;
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

  const sortedActive = sortActiveSessions(active, sessions);
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
  dragListeners?: React.HTMLAttributes<HTMLElement>;
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
          className="flex items-center gap-0.5 text-xs text-[#ff9e64]"
          title={`${counts.pendingApproval} awaiting approval`}
        >
          <span className="inline-block h-1.5 w-1.5 rounded-full bg-[#ff9e64] animate-pulse" />
          {counts.pendingApproval}
        </span>
      )}
      {counts.running > 0 && (
        <span
          className="flex items-center gap-0.5 text-xs text-[#73daca]"
          title={`${counts.running} running`}
        >
          <span className="inline-block h-1.5 w-1.5 rounded-full bg-[#73daca] animate-pulse" />
          {counts.running}
        </span>
      )}
      {counts.idle > 0 && counts.running === 0 && counts.pendingApproval === 0 && (
        <span
          className="flex items-center gap-0.5 text-xs text-[#9ece6a]/70"
          title={`${counts.idle} idle`}
        >
          <span className="inline-block h-1.5 w-1.5 rounded-full bg-[#9ece6a]" />
          {counts.idle}
        </span>
      )}
    </span>
  );
}

// --- Project git status row ---

function ProjectGitStatusRow({ gitStatus }: { gitStatus: ProjectGitStatus }) {
  return (
    <div className="flex items-center gap-1.5 pl-5.5 text-xs text-muted-foreground">
      <GitBranch className="h-3 w-3 shrink-0 text-muted-foreground" />
      <span className="font-mono truncate text-foreground/80">{gitStatus.branch}</span>
      <GitIndicators
        uncommittedCount={gitStatus.uncommittedCount}
        aheadCount={gitStatus.aheadRemote}
        behindCount={gitStatus.behindRemote}
      />
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
  dragListeners,
}: ProjectTreeItemProps) {
  const navigate = useNavigate();
  const gitStatus = useAppStore((s) => s.projectGitStatus[project.id]);
  const sessionCounts = useActiveSessionCounts(project.id);

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
    <div
      className={cn("border-l-2 border-transparent pb-2", isActive && "border-l-sidebar-primary")}
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
          {...dragListeners}
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
                  className="text-base font-medium shrink-0 text-foreground-bright hover:underline"
                >
                  {project.name}
                </button>
                <ActiveSessionIndicators counts={sessionCounts} />
              </div>
              {gitStatus?.branch && <ProjectGitStatusRow gitStatus={gitStatus} />}
            </div>
          </div>
        </div>
      </ProjectHoverCard>

      {/* Sessions + new chat */}
      {isExpanded && (
        <div className="ml-4 mr-2 mt-1 space-y-0.5">
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
