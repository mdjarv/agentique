import { useNavigate } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, EyeOff, FolderOpen, Plus } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { useShallow } from "zustand/shallow";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog";
import { useWebSocket } from "~/hooks/useWebSocket";
import { deleteSession, interruptSession, stopSession } from "~/lib/session-actions";
import type { Project } from "~/lib/types";
import { cn } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { type ChatState, useChatStore } from "~/stores/chat-store";
import { SessionRow } from "./SessionRow";

const activePriority: Record<string, number> = {
  running: 0,
  starting: 1,
  idle: 2,
  draft: 3,
  disconnected: 4,
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

function renderSessionRow(
  id: string,
  sessions: ChatState["sessions"],
  activeSessionId: string | null,
  onSessionClick: (id: string) => void,
  onStop: (e: React.MouseEvent, id: string, state: string) => void,
  onDelete: (e: React.MouseEvent, id: string) => void,
) {
  const session = sessions[id]?.meta;
  if (!session) return null;
  return (
    <SessionRow
      key={id}
      name={session.name}
      state={session.state}
      hasUnseenCompletion={sessions[id]?.hasUnseenCompletion}
      hasPendingApproval={!!sessions[id]?.pendingApproval || !!sessions[id]?.pendingQuestion}
      isPlanning={!!sessions[id]?.planMode}
      isActive={id === activeSessionId}
      worktreeBranch={session.worktreeBranch}
      hasDirtyWorktree={session.hasDirtyWorktree}
      worktreeMerged={session.worktreeMerged}
      commitsAhead={session.commitsAhead}
      branchMissing={session.branchMissing}
      hasUncommitted={session.hasUncommitted}
      onClick={() => onSessionClick(id)}
      onStop={(e) => onStop(e, id, session.state)}
      onDelete={(e) => onDelete(e, id)}
    />
  );
}

function SessionGroups({
  sessionIds,
  sessions,
  activeSessionId,
  hideStoppedSessions,
  onSessionClick,
  onStop,
  onDelete,
}: {
  sessionIds: string[];
  sessions: ChatState["sessions"];
  activeSessionId: string | null;
  hideStoppedSessions: boolean;
  onSessionClick: (id: string) => void;
  onStop: (e: React.MouseEvent, id: string, state: string) => void;
  onDelete: (e: React.MouseEvent, id: string) => void;
}) {
  const active: string[] = [];
  const completed: string[] = [];

  for (const id of sessionIds) {
    const meta = sessions[id]?.meta;
    if (!meta) continue;
    // Completed = explicitly merged. Everything else stays active.
    if (meta.worktreeMerged) {
      completed.push(id);
    } else {
      if (hideStoppedSessions && meta.state === "stopped") continue;
      active.push(id);
    }
  }

  const sortedActive = sortByPriorityThenDate(active, sessions, activePriority);
  // Completed: most recently active first
  const sortedCompleted = [...completed].sort((a, b) => {
    const ma = sessions[a]?.meta;
    const mb = sessions[b]?.meta;
    const ta = new Date(ma?.updatedAt ?? ma?.createdAt ?? 0).getTime();
    const tb = new Date(mb?.updatedAt ?? mb?.createdAt ?? 0).getTime();
    return tb - ta;
  });

  return (
    <>
      {sortedActive.map((id) =>
        renderSessionRow(id, sessions, activeSessionId, onSessionClick, onStop, onDelete),
      )}
      {sortedCompleted.length > 0 && (
        <CompletedSection
          ids={sortedCompleted}
          sessions={sessions}
          activeSessionId={activeSessionId}
          hasActiveSessions={sortedActive.length > 0}
          onSessionClick={onSessionClick}
          onStop={onStop}
          onDelete={onDelete}
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
  onStop,
  onDelete,
}: {
  ids: string[];
  sessions: ChatState["sessions"];
  activeSessionId: string | null;
  hasActiveSessions: boolean;
  onSessionClick: (id: string) => void;
  onStop: (e: React.MouseEvent, id: string, state: string) => void;
  onDelete: (e: React.MouseEvent, id: string) => void;
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
          <ChevronDown className="size-3 shrink-0 text-muted-foreground/40 transition-transform" />
        ) : (
          <ChevronRight className="size-3 shrink-0 text-muted-foreground/40 transition-transform" />
        )}
        <span className="text-[10px] font-semibold tracking-widest text-muted-foreground/40 uppercase group-hover:text-muted-foreground/60">
          Completed
        </span>
        <span className="text-[10px] text-muted-foreground/30">{ids.length}</span>
      </button>
      {expanded &&
        ids.map((id) =>
          renderSessionRow(id, sessions, activeSessionId, onSessionClick, onStop, onDelete),
        )}
    </>
  );
}

interface ProjectTreeItemProps {
  project: Project;
  isActive: boolean;
  isExpanded: boolean;
  onToggleExpand: () => void;
}

function truncatePath(path: string): string {
  return path.replace(/^\/home\/[^/]+/, "~").replace(/^\/Users\/[^/]+/, "~");
}

export function ProjectTreeItem({
  project,
  isActive,
  isExpanded,
  onToggleExpand,
}: ProjectTreeItemProps) {
  const navigate = useNavigate();
  const ws = useWebSocket();
  const [sessionToDelete, setSessionToDelete] = useState<string | null>(null);
  const [busySessionId, setBusySessionId] = useState<string | null>(null);

  const sessionIds = useChatStore(
    useShallow((s) =>
      Object.keys(s.sessions).filter((id) => s.sessions[id]?.meta.projectId === project.id),
    ),
  );
  const sessions = useChatStore((s) => s.sessions);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const setActiveSessionId = useChatStore((s) => s.setActiveSessionId);
  const hideStoppedSessions = useAppStore((s) => s.hideStoppedSessions);
  const toggleHideStoppedSessions = useAppStore((s) => s.toggleHideStoppedSessions);

  const handleProjectClick = () => {
    onToggleExpand();
    if (!isActive) {
      navigate({ to: "/project/$projectId", params: { projectId: project.id } });
    }
  };

  const handleStopSession = async (e: React.MouseEvent, sessionId: string, state: string) => {
    e.stopPropagation();
    if (busySessionId) return;
    setBusySessionId(sessionId);
    try {
      if (state === "running") {
        await interruptSession(ws, sessionId);
      } else {
        await stopSession(ws, sessionId);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to stop session");
    } finally {
      setBusySessionId(null);
    }
  };

  const handleDeleteSession = (e: React.MouseEvent, sessionId: string) => {
    e.stopPropagation();
    setSessionToDelete(sessionId);
  };

  const confirmDeleteSession = async () => {
    if (!sessionToDelete) return;
    try {
      if (sessionToDelete.startsWith("draft-")) {
        useChatStore.getState().removeSession(sessionToDelete);
      } else {
        await deleteSession(ws, sessionToDelete);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to delete session");
    } finally {
      setSessionToDelete(null);
    }
  };

  const handleSessionClick = (sessionId: string) => {
    if (!isActive) {
      navigate({
        to: "/project/$projectId",
        params: { projectId: project.id },
        search: { session: sessionId },
      });
    }
    setActiveSessionId(sessionId);
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
          "w-full text-left rounded-md px-2 py-1.5 group hover:bg-accent transition-colors cursor-pointer",
          isActive && "bg-accent",
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
                to: "/project/$projectId/settings",
                params: { projectId: project.id },
              });
            }}
            className="text-sm font-medium shrink-0 hover:underline"
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
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={() => {
                useChatStore.getState().createDraft(project.id);
                if (!isActive) {
                  navigate({ to: "/project/$projectId", params: { projectId: project.id } });
                }
              }}
              className="flex items-center gap-1.5 flex-1 rounded-md px-2 py-1 text-sm text-muted-foreground hover:text-foreground hover:bg-accent/50 transition-colors cursor-pointer"
            >
              <Plus className="h-3.5 w-3.5" />
              <span>New chat</span>
            </button>
            <button
              type="button"
              onClick={toggleHideStoppedSessions}
              title={hideStoppedSessions ? "Show stopped sessions" : "Hide stopped sessions"}
              className={cn(
                "rounded-md p-1 transition-colors cursor-pointer",
                hideStoppedSessions
                  ? "text-foreground bg-accent/50"
                  : "text-muted-foreground hover:text-foreground hover:bg-accent/50",
              )}
            >
              <EyeOff className="h-3.5 w-3.5" />
            </button>
          </div>
          {isActive && (
            <SessionGroups
              sessionIds={sessionIds}
              sessions={sessions}
              activeSessionId={activeSessionId}
              hideStoppedSessions={hideStoppedSessions}
              onSessionClick={handleSessionClick}
              onStop={handleStopSession}
              onDelete={handleDeleteSession}
            />
          )}
        </div>
      )}

      <AlertDialog
        open={!!sessionToDelete}
        onOpenChange={(open) => {
          if (!open) setSessionToDelete(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete session</AlertDialogTitle>
            <AlertDialogDescription>
              This will remove the session and its data. This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDeleteSession}>Delete</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
