import { useNavigate } from "@tanstack/react-router";
import {
  ChevronDown,
  ChevronRight,
  FolderOpen,
  GitBranch,
  Plus,
  Square,
  Trash2,
} from "lucide-react";
import { useShallow } from "zustand/shallow";
import { useWebSocket } from "~/hooks/useWebSocket";
import { deleteProject } from "~/lib/api";
import { stopSession } from "~/lib/session-actions";
import type { Project } from "~/lib/types";
import { cn } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";
import { SessionStatusDot } from "./SessionStatusDot";

interface ProjectTreeItemProps {
  project: Project;
  isActive: boolean;
  onNewSession: () => void;
}

export function ProjectTreeItem({ project, isActive, onNewSession }: ProjectTreeItemProps) {
  const navigate = useNavigate();
  const removeProject = useAppStore((s) => s.removeProject);
  const ws = useWebSocket();

  const sessionIds = useChatStore(useShallow((s) => Object.keys(s.sessions)));
  const sessions = useChatStore((s) => s.sessions);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const setActiveSessionId = useChatStore((s) => s.setActiveSessionId);

  const handleProjectClick = () => {
    navigate({ to: "/project/$projectId", params: { projectId: project.id } });
  };

  const handleDelete = async (e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      await deleteProject(project.id);
      removeProject(project.id);
      if (isActive) {
        navigate({ to: "/" });
      }
    } catch (err) {
      console.error("Failed to delete project:", err);
    }
  };

  const handleNewSession = (e: React.MouseEvent) => {
    e.stopPropagation();
    onNewSession();
  };

  const handleStopSession = async (e: React.MouseEvent, sessionId: string) => {
    e.stopPropagation();
    await stopSession(ws, sessionId);
  };

  const handleSessionClick = (sessionId: string) => {
    if (!isActive) {
      navigate({ to: "/project/$projectId", params: { projectId: project.id } });
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
          {isActive ? (
            <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          ) : (
            <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          )}
          <FolderOpen className="h-4 w-4 shrink-0" />
          <span className="text-sm font-medium truncate flex-1">{project.name}</span>
          <button
            type="button"
            aria-label="New session"
            onClick={handleNewSession}
            className="opacity-0 group-hover:opacity-100 p-0.5 rounded hover:bg-primary/20 transition-opacity"
          >
            <Plus className="h-3.5 w-3.5" />
          </button>
          <button
            type="button"
            aria-label="Delete project"
            onClick={handleDelete}
            className="opacity-0 group-hover:opacity-100 p-0.5 rounded hover:bg-destructive hover:text-destructive-foreground transition-opacity"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </div>
        <p className="text-xs text-muted-foreground truncate mt-0.5 pl-5">{project.path}</p>
      </div>

      {/* Sessions (only for active project) */}
      {isActive && sessionIds.length > 0 && (
        <div className="ml-4 mt-0.5 space-y-0.5">
          {sessionIds.map((id) => {
            const session = sessions[id]?.meta;
            if (!session) return null;
            const isActiveSession = id === activeSessionId;
            return (
              <div
                key={id}
                className={cn(
                  "flex items-center gap-2 rounded-md px-2 py-1 text-sm group/session hover:bg-accent/50 transition-colors",
                  isActiveSession && "bg-accent/70",
                )}
              >
                <button
                  type="button"
                  className="flex items-center gap-2 flex-1 min-w-0 cursor-pointer bg-transparent border-0 p-0 text-left text-inherit"
                  onClick={() => handleSessionClick(id)}
                >
                  <SessionStatusDot
                    state={session.state}
                    hasUnseenCompletion={sessions[id]?.hasUnseenCompletion}
                  />
                  <span className="truncate">{session.name}</span>
                  {session.worktreeBranch && (
                    <GitBranch className="h-3 w-3 text-muted-foreground shrink-0" />
                  )}
                </button>
                {session.state !== "stopped" && session.state !== "done" && (
                  <button
                    type="button"
                    aria-label="Stop session"
                    onClick={(e) => handleStopSession(e, id)}
                    className="opacity-0 group-hover/session:opacity-100 p-0.5 rounded hover:bg-destructive hover:text-destructive-foreground transition-opacity"
                  >
                    <Square className="h-3 w-3" />
                  </button>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
