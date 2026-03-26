import { useNavigate } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, FolderOpen, Plus } from "lucide-react";
import { type ReactNode, useState } from "react";
import { useShallow } from "zustand/shallow";
import type { Project } from "~/lib/types";
import { cn } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
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
      {sortedActive.map((id) => renderSessionRow(id, sessions, activeSessionId, onSessionClick))}
      {newChatButton}
      {sortedCompleted.length > 0 && (
        <CompletedSection
          ids={sortedCompleted}
          sessions={sessions}
          activeSessionId={activeSessionId}
          hasActiveSessions={sortedActive.length > 0}
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
  hasActiveSessions,
  onSessionClick,
}: {
  ids: string[];
  sessions: ChatState["sessions"];
  activeSessionId: string | undefined;
  hasActiveSessions: boolean;
  onSessionClick: (id: string) => void;
}) {
  const [expanded, setExpanded] = useState(!hasActiveSessions);

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

export function ProjectTreeItem({
  project,
  isActive,
  isExpanded,
  onToggleExpand,
  activeSessionId,
  isNewChatActive,
}: ProjectTreeItemProps) {
  const navigate = useNavigate();

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
      {/* Project row */}
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
          "w-full text-left rounded-md px-2 py-1.5 max-md:py-2.5 group hover:bg-sidebar-accent transition-colors cursor-pointer",
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
          <span
            className="text-xs text-muted-foreground min-w-0 overflow-hidden text-ellipsis whitespace-nowrap flex-1"
            dir="rtl"
          >
            {truncatePath(project.path)}
          </span>
        </div>
      </div>

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
                  "flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 max-md:py-2.5 text-sm text-sidebar-foreground/60 hover:text-sidebar-foreground hover:bg-sidebar-accent/50 transition-colors cursor-pointer",
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
