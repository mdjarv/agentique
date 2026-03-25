import { useNavigate } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, FolderOpen, Plus, Trash2, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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
import {
  deleteSession,
  deleteSessionsBulk,
  interruptSession,
  stopSession,
} from "~/lib/session-actions";
import type { Project } from "~/lib/types";
import { cn } from "~/lib/utils";
import { type ChatState, useChatStore } from "~/stores/chat-store";
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

/** Compute the display-ordered list of session IDs (active then completed). */
function computeOrderedIds(sessionIds: string[], sessions: ChatState["sessions"]): string[] {
  const active: string[] = [];
  const completed: string[] = [];
  for (const id of sessionIds) {
    if (sessions[id]?.meta.worktreeMerged) completed.push(id);
    else active.push(id);
  }
  return [
    ...sortByPriorityThenDate(active, sessions, activePriority),
    ...sortCompletedByDate(completed, sessions),
  ];
}

interface SelectionProps {
  selectedIds: Set<string>;
  inMultiSelect: boolean;
  onSelect: (e: React.MouseEvent, id: string) => void;
}

function renderSessionRow(
  id: string,
  sessions: ChatState["sessions"],
  activeSessionId: string | undefined,
  onSessionClick: (id: string) => void,
  onStop: (e: React.MouseEvent, id: string, state: string) => void,
  onDelete: (e: React.MouseEvent, id: string) => void,
  selection: SelectionProps,
) {
  const session = sessions[id]?.meta;
  if (!session) return null;
  return (
    <SessionRow
      key={id}
      name={session.name}
      state={session.state}
      connected={session.connected}
      hasUnseenCompletion={sessions[id]?.hasUnseenCompletion}
      hasPendingApproval={!!sessions[id]?.pendingApproval || !!sessions[id]?.pendingQuestion}
      isPlanning={!!sessions[id]?.planMode}
      isActive={id === activeSessionId}
      isSelected={selection.selectedIds.has(id)}
      showCheckbox={selection.inMultiSelect}
      worktreeBranch={session.worktreeBranch}
      hasDirtyWorktree={session.hasDirtyWorktree}
      worktreeMerged={session.worktreeMerged}
      commitsAhead={session.commitsAhead}
      commitsBehind={session.commitsBehind}
      branchMissing={session.branchMissing}
      hasUncommitted={session.hasUncommitted}
      prUrl={session.prUrl}
      totalCost={session.totalCost}
      onClick={() => onSessionClick(id)}
      onStop={(e) => onStop(e, id, session.state)}
      onDelete={(e) => onDelete(e, id)}
      onSelect={(e) => selection.onSelect(e, id)}
    />
  );
}

function SessionGroups({
  sessionIds,
  sessions,
  activeSessionId,
  onSessionClick,
  onStop,
  onDelete,
  selection,
}: {
  sessionIds: string[];
  sessions: ChatState["sessions"];
  activeSessionId: string | undefined;
  onSessionClick: (id: string) => void;
  onStop: (e: React.MouseEvent, id: string, state: string) => void;
  onDelete: (e: React.MouseEvent, id: string) => void;
  selection: SelectionProps;
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
      {sortedActive.map((id) =>
        renderSessionRow(
          id,
          sessions,
          activeSessionId,
          onSessionClick,
          onStop,
          onDelete,
          selection,
        ),
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
          selection={selection}
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
  selection,
}: {
  ids: string[];
  sessions: ChatState["sessions"];
  activeSessionId: string | undefined;
  hasActiveSessions: boolean;
  onSessionClick: (id: string) => void;
  onStop: (e: React.MouseEvent, id: string, state: string) => void;
  onDelete: (e: React.MouseEvent, id: string) => void;
  selection: SelectionProps;
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
      {expanded &&
        ids.map((id) =>
          renderSessionRow(
            id,
            sessions,
            activeSessionId,
            onSessionClick,
            onStop,
            onDelete,
            selection,
          ),
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
  const ws = useWebSocket();
  const [sessionToDelete, setSessionToDelete] = useState<string | null>(null);
  const [busySessionId, setBusySessionId] = useState<string | null>(null);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [showBulkDeleteDialog, setShowBulkDeleteDialog] = useState(false);
  const [bulkDeleting, setBulkDeleting] = useState(false);
  const lastClickedRef = useRef<string | null>(null);

  const sessionIds = useChatStore(
    useShallow((s) =>
      Object.keys(s.sessions).filter((id) => s.sessions[id]?.meta.projectId === project.id),
    ),
  );
  const sessions = useChatStore((s) => s.sessions);

  const orderedIds = useMemo(() => computeOrderedIds(sessionIds, sessions), [sessionIds, sessions]);

  const inMultiSelect = selectedIds.size > 0;

  // Prune stale IDs from selection when sessions change
  useEffect(() => {
    if (!inMultiSelect) return;
    const sessionIdSet = new Set(sessionIds);
    setSelectedIds((prev) => {
      const next = new Set<string>();
      for (const id of prev) {
        if (sessionIdSet.has(id)) next.add(id);
      }
      return next.size === prev.size ? prev : next;
    });
  }, [sessionIds, inMultiSelect]);

  const handleSessionSelect = useCallback(
    (e: React.MouseEvent, id: string) => {
      setSelectedIds((prev) => {
        const next = new Set(prev);

        if (e.shiftKey && lastClickedRef.current) {
          const startIdx = orderedIds.indexOf(lastClickedRef.current);
          const endIdx = orderedIds.indexOf(id);
          if (startIdx !== -1 && endIdx !== -1) {
            const lo = Math.min(startIdx, endIdx);
            const hi = Math.max(startIdx, endIdx);
            for (let i = lo; i <= hi; i++) {
              const oid = orderedIds[i];
              if (oid) next.add(oid);
            }
            lastClickedRef.current = id;
            return next;
          }
        }

        if (next.has(id)) next.delete(id);
        else next.add(id);
        lastClickedRef.current = id;
        return next;
      });
    },
    [orderedIds],
  );

  const clearSelection = useCallback(() => {
    setSelectedIds(new Set());
    lastClickedRef.current = null;
  }, []);

  const selectedWithWorktrees = useMemo(() => {
    const result: string[] = [];
    for (const id of selectedIds) {
      const meta = sessions[id]?.meta;
      if (meta?.worktreePath && !meta.worktreeMerged) result.push(id);
    }
    return result;
  }, [selectedIds, sessions]);

  const selection: SelectionProps = useMemo(
    () => ({
      selectedIds,
      inMultiSelect,
      onSelect: handleSessionSelect,
    }),
    [selectedIds, inMultiSelect, handleSessionSelect],
  );

  const handleProjectClick = () => {
    onToggleExpand();
    if (!isActive) {
      navigate({ to: "/project/$projectSlug", params: { projectSlug: project.slug } });
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

  const confirmBulkDelete = async () => {
    if (selectedIds.size === 0) return;
    setBulkDeleting(true);
    try {
      const result = await deleteSessionsBulk(ws, [...selectedIds]);
      const failed = result.results.filter((r) => !r.success);
      if (failed.length > 0) {
        toast.error(`Failed to delete ${failed.length} session(s): ${failed[0]?.error}`);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Bulk delete failed");
    } finally {
      setBulkDeleting(false);
      setShowBulkDeleteDialog(false);
      clearSelection();
    }
  };

  const handleSessionClick = (sessionId: string) => {
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
          "w-full text-left rounded-md px-2 py-1.5 group hover:bg-sidebar-accent transition-colors cursor-pointer",
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
          <button
            type="button"
            onClick={() => {
              navigate({
                to: "/project/$projectSlug/session/new",
                params: { projectSlug: project.slug },
              });
            }}
            className={cn(
              "flex items-center gap-1.5 rounded-md px-2 py-1 text-sm text-sidebar-foreground/60 hover:text-sidebar-foreground hover:bg-sidebar-accent/50 transition-colors cursor-pointer",
              isNewChatActive && "bg-sidebar-accent/70 text-sidebar-foreground",
            )}
          >
            <Plus className="h-3.5 w-3.5" />
            <span>New chat</span>
          </button>
          {inMultiSelect && (
            <div className="flex items-center gap-1 px-2 py-1 text-xs text-muted-foreground">
              <span className="font-medium">{selectedIds.size} selected</span>
              <button
                type="button"
                onClick={() => setShowBulkDeleteDialog(true)}
                className="ml-auto flex items-center gap-0.5 rounded px-1.5 py-0.5 text-destructive hover:bg-destructive hover:text-destructive-foreground transition-colors"
              >
                <Trash2 className="size-3" />
                Delete
              </button>
              <button
                type="button"
                onClick={clearSelection}
                aria-label="Clear selection"
                className="rounded p-0.5 hover:bg-sidebar-accent transition-colors"
              >
                <X className="size-3" />
              </button>
            </div>
          )}
          <SessionGroups
            sessionIds={sessionIds}
            sessions={sessions}
            activeSessionId={activeSessionId}
            onSessionClick={handleSessionClick}
            onStop={handleStopSession}
            onDelete={handleDeleteSession}
            selection={selection}
          />
        </div>
      )}

      {/* Single delete dialog */}
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

      {/* Bulk delete dialog */}
      <AlertDialog
        open={showBulkDeleteDialog}
        onOpenChange={(open) => {
          if (!open && !bulkDeleting) setShowBulkDeleteDialog(false);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete {selectedIds.size} sessions</AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div>
                <p>This will permanently delete the selected sessions and their data.</p>
                {selectedWithWorktrees.length > 0 && (
                  <p className="mt-2 font-medium text-[#e0af68]">
                    {selectedWithWorktrees.length} session(s) have worktrees that will be removed.
                  </p>
                )}
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={bulkDeleting}>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={confirmBulkDelete} disabled={bulkDeleting}>
              {bulkDeleting ? "Deleting..." : `Delete ${selectedIds.size} sessions`}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
