import { useNavigate } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, FolderOpen, Plus } from "lucide-react";
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
import { type SessionState, useChatStore } from "~/stores/chat-store";
import { SessionRow } from "./SessionRow";

const statePriority: Record<SessionState, number> = {
  running: 0,
  starting: 1,
  idle: 2,
  draft: 3,
  disconnected: 4,
  failed: 5,
  stopped: 6,
  done: 7,
};

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

  const sessionIds = useChatStore(useShallow((s) => Object.keys(s.sessions)));
  const sessions = useChatStore((s) => s.sessions);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const setActiveSessionId = useChatStore((s) => s.setActiveSessionId);

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
      await deleteSession(ws, sessionToDelete);
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
          <button
            type="button"
            onClick={() => {
              useChatStore.getState().createDraft(project.id);
              if (!isActive) {
                navigate({ to: "/project/$projectId", params: { projectId: project.id } });
              }
            }}
            className="flex items-center gap-1.5 w-full rounded-md px-2 py-1 text-sm text-muted-foreground hover:text-foreground hover:bg-accent/50 transition-colors cursor-pointer"
          >
            <Plus className="h-3.5 w-3.5" />
            <span>New chat</span>
          </button>
          {isActive &&
            [...sessionIds]
              .filter((id) => sessions[id]?.meta.state !== "draft")
              .sort((a, b) => {
                const sa = sessions[a]?.meta;
                const sb = sessions[b]?.meta;
                if (!sa || !sb) return 0;
                const pa = statePriority[sa.state] ?? 99;
                const pb = statePriority[sb.state] ?? 99;
                if (pa !== pb) return pa - pb;
                return new Date(sb.createdAt).getTime() - new Date(sa.createdAt).getTime();
              })
              .map((id) => {
                const session = sessions[id]?.meta;
                if (!session) return null;
                return (
                  <SessionRow
                    key={id}
                    name={session.name}
                    state={session.state}
                    hasUnseenCompletion={sessions[id]?.hasUnseenCompletion}
                    hasPendingApproval={
                      !!sessions[id]?.pendingApproval || !!sessions[id]?.pendingQuestion
                    }
                    isPlanning={!!sessions[id]?.planMode}
                    isActive={id === activeSessionId}
                    worktreeBranch={session.worktreeBranch}
                    hasDirtyWorktree={session.hasDirtyWorktree}
                    onClick={() => handleSessionClick(id)}
                    onStop={(e) => handleStopSession(e, id, session.state)}
                    onDelete={(e) => handleDeleteSession(e, id)}
                  />
                );
              })}
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
