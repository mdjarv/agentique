import {
  DndContext,
  type DragEndEvent,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { useNavigate, useParams, useRouterState } from "@tanstack/react-router";

import { AlertTriangle, MoreHorizontal, Trash2 } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { ProjectPill } from "~/components/ui/project-pill";
import { type AttentionItem, useActivityStreamItems } from "~/hooks/useActivityStreamItems";
import { useTheme } from "~/hooks/useTheme";
import { useWebSocket } from "~/hooks/useWebSocket";
import { joinChannel, leaveChannel } from "~/lib/channel-actions";
import { getProjectColor } from "~/lib/project-colors";
import { deleteSessionsBulk } from "~/lib/session/actions";
import type { Project } from "~/lib/types";
import { getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { type SessionData, useChatStore } from "~/stores/chat-store";
import { ChannelStreamItem } from "./ChannelStreamItem";
import { SectionLabel } from "./SectionLabel";
import type { BadgeState } from "./session/SessionBadge";
import { SessionBadge } from "./session/SessionBadge";
import { StreamSessionRow } from "./session/StreamSessionRow";

// --- Completed section actions ---

function CompletedSectionActions({
  safeCount,
  riskyCount,
  onDeleteSafe,
  onDeleteAll,
}: {
  safeCount: number;
  riskyCount: number;
  onDeleteSafe: () => void;
  onDeleteAll: () => void;
}) {
  const total = safeCount + riskyCount;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          className="p-0.5 rounded text-muted-foreground hover:text-foreground hover:bg-sidebar-accent transition-colors"
          onClick={(e) => e.stopPropagation()}
        >
          <MoreHorizontal className="size-3.5" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" side="bottom">
        <DropdownMenuItem
          onClick={onDeleteSafe}
          disabled={safeCount === 0}
          className="text-xs gap-2"
        >
          <Trash2 className="size-3.5" />
          Delete safe ({safeCount})
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={onDeleteAll}
          disabled={total === 0}
          className="text-xs gap-2 text-destructive focus:text-destructive"
        >
          <Trash2 className="size-3.5" />
          Delete all completed ({total})
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// --- Drag overlay ---

function DraggedSessionPreview({ sessionId }: { sessionId: string }) {
  const name = useChatStore((s) => s.sessions[sessionId]?.meta.name ?? "Untitled");
  return (
    <div className="rounded-md bg-sidebar-accent border border-sidebar-border px-3 py-1.5 text-sm shadow-lg">
      {name}
    </div>
  );
}

// --- Main component ---

interface ActivityStreamProps {
  searchQuery: string;
  filterProjectId?: string | null;
}

export function ActivityStream({ searchQuery, filterProjectId = null }: ActivityStreamProps) {
  const navigate = useNavigate();
  const ws = useWebSocket();
  const { attention, active, recent, activeUnread, recentUnread } = useActivityStreamItems(
    searchQuery,
    filterProjectId,
  );

  const params = useParams({ strict: false }) as {
    projectSlug?: string;
    sessionShortId?: string;
  };
  const activeSessionId = useChatStore((s) => {
    const shortId = params.sessionShortId;
    if (!shortId) return undefined;
    return Object.keys(s.sessions).find((id) => id.startsWith(shortId));
  });

  const projects = useAppStore((s) => s.projects);
  const sessions = useChatStore((s) => s.sessions);

  // Resolve filter project color for visual tinting
  const filterProject = useAppStore((s) =>
    filterProjectId ? s.projects.find((p) => p.id === filterProjectId) : undefined,
  );
  const projectIds = useAppStore(useShallow((s) => s.projects.map((p) => p.id)));
  const { resolvedTheme } = useTheme();
  const filterColor = useMemo(
    () =>
      filterProject
        ? getProjectColor(filterProject.color, filterProject.id, projectIds, resolvedTheme)
        : null,
    [filterProject, projectIds, resolvedTheme],
  );
  const isFiltered = !!filterProjectId;

  const projectBySession = useMemo(() => {
    const map: Record<string, string> = {};
    for (const [id, data] of Object.entries(sessions) as [string, SessionData][]) {
      const project = projects.find((p: Project) => p.id === data.meta.projectId);
      if (project) map[id] = project.slug;
    }
    return map;
  }, [projects, sessions]);

  const [completedExpanded, setCompletedExpanded] = useState(false);
  const [bulkDeleteMode, setBulkDeleteMode] = useState<"safe" | "all" | null>(null);
  const [draggingId, setDraggingId] = useState<string | null>(null);

  const { safeIds, riskyIds, riskySessions } = useMemo(() => {
    const safe: string[] = [];
    const risky: string[] = [];
    const riskyMeta: Array<{ id: string; name: string; commitsAhead: number }> = [];
    for (const item of recent) {
      if (item.kind !== "session") continue;
      const meta = sessions[item.sessionId]?.meta;
      if (!meta) continue;
      if (meta.worktreePath && !meta.worktreeMerged) {
        risky.push(item.sessionId);
        riskyMeta.push({ id: item.sessionId, name: meta.name, commitsAhead: meta.commitsAhead });
      } else {
        safe.push(item.sessionId);
      }
    }
    return { safeIds: safe, riskyIds: risky, riskySessions: riskyMeta };
  }, [recent, sessions]);

  const handleBulkDelete = useCallback(async () => {
    const ids = bulkDeleteMode === "safe" ? safeIds : [...safeIds, ...riskyIds];
    if (ids.length === 0) {
      setBulkDeleteMode(null);
      return;
    }
    try {
      const result = await deleteSessionsBulk(ws, ids);
      const failed = result.results.filter((r) => !r.success);
      if (failed.length > 0) {
        toast.error(`${failed.length} session(s) failed to delete`);
      } else {
        toast.success(`Deleted ${result.results.length} session(s)`);
      }
    } catch (err) {
      toast.error(getErrorMessage(err, "Bulk delete failed"));
    } finally {
      setBulkDeleteMode(null);
    }
  }, [bulkDeleteMode, safeIds, riskyIds, ws]);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor),
  );

  const handleSessionClick = useCallback(
    (sessionId: string) => {
      const slug = projectBySession[sessionId];
      if (!slug) return;
      useAppStore.getState().setSidebarOpen(false);
      navigate({
        to: "/project/$projectSlug/session/$sessionShortId",
        params: { projectSlug: slug, sessionShortId: sessionId.split("-")[0] ?? "" },
      });
    },
    [navigate, projectBySession],
  );

  const handleAttentionClick = useCallback(
    (item: AttentionItem) => {
      if (item.isChannel) {
        useAppStore.getState().setSidebarOpen(false);
        navigate({
          to: "/project/$projectSlug/channel/$channelId",
          params: { projectSlug: item.projectSlug, channelId: item.id },
        });
      } else {
        handleSessionClick(item.id);
      }
    },
    [navigate, handleSessionClick],
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      setDraggingId(null);
      const { active: dragActive, over } = event;
      if (!over) return;

      const sessionId = dragActive.data.current?.sessionId as string | undefined;
      const sourceChannelId = dragActive.data.current?.channelId as string | undefined;
      if (!sessionId) return;

      const overId = over.id as string;

      if (overId.startsWith("channel:")) {
        const targetChannelId = over.data.current?.channelId as string;
        if (sourceChannelId === targetChannelId) return;

        if (sourceChannelId) {
          leaveChannel(ws, sessionId, sourceChannelId)
            .then(() => joinChannel(ws, sessionId, targetChannelId, "worker"))
            .catch((err) => toast.error(getErrorMessage(err, "Failed to move to channel")));
        } else {
          joinChannel(ws, sessionId, targetChannelId, "worker").catch((err) =>
            toast.error(getErrorMessage(err, "Failed to join channel")),
          );
        }
      }
    },
    [ws],
  );

  const isNewChatRoute = useRouterState({
    select: (s) => s.location.pathname.endsWith("/session/new"),
  });

  const hasAnything = attention.length > 0 || active.length > 0 || recent.length > 0;

  if (!hasAnything && !isNewChatRoute) {
    return (
      <div className="flex-1 flex items-center justify-center p-4">
        <span className="text-sm text-muted-foreground-faint">
          {searchQuery ? "No matching sessions" : "No sessions yet"}
        </span>
      </div>
    );
  }

  return (
    <DndContext
      sensors={sensors}
      onDragStart={(e) => setDraggingId(e.active.id as string)}
      onDragCancel={() => setDraggingId(null)}
      onDragEnd={handleDragEnd}
    >
      <div
        className="flex-1 overflow-y-auto py-1 space-y-1"
        style={
          filterColor
            ? {
                borderLeft: `2px solid ${filterColor.bg}40`,
                background: `linear-gradient(to right, ${filterColor.bg}06, transparent 40%)`,
              }
            : undefined
        }
      >
        {/* Attention section */}
        {attention.length > 0 && (
          <div>
            <SectionLabel
              label="Needs input"
              count={attention.length}
              accentColor={filterColor?.fg}
            />
            <div className="px-1 space-y-0.5">
              {attention.map((item) =>
                item.isChannel ? (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => handleAttentionClick(item)}
                    className="flex w-full items-start gap-1.5 rounded-md px-2 py-1.5 max-md:py-2.5 text-sm text-left transition-colors cursor-pointer hover:bg-sidebar-accent/50"
                  >
                    <SessionBadge state={item.kind as BadgeState} className="mt-0.5" />
                    <div className="min-w-0 flex-1">
                      <span className="block truncate text-sidebar-foreground">{item.name}</span>
                      <span className="flex items-center gap-1 text-[10px] text-muted-foreground-faint">
                        {!isFiltered && (
                          <ProjectPill
                            slug={item.projectSlug}
                            background={false}
                            className="truncate"
                          />
                        )}
                        <span className="shrink-0 tabular-nums text-muted-foreground-faint ml-auto">
                          {item.time}
                        </span>
                      </span>
                    </div>
                  </button>
                ) : (
                  <StreamSessionRow
                    key={item.id}
                    sessionId={item.id}
                    projectSlug={item.projectSlug}
                    activeSessionId={activeSessionId}
                    isDragActive={!!draggingId}
                    onSessionClick={handleSessionClick}
                    hideProjectPill={isFiltered}
                  />
                ),
              )}
            </div>
          </div>
        )}

        {/* Active section */}
        {active.length > 0 && (
          <div>
            <SectionLabel
              label="Active"
              count={active.length}
              unreadCount={activeUnread}
              accentColor={filterColor?.fg}
            />
            <div className="px-1">
              {active.map((item) =>
                item.kind === "session" ? (
                  <StreamSessionRow
                    key={item.sessionId}
                    sessionId={item.sessionId}
                    projectSlug={item.projectSlug}
                    activeSessionId={activeSessionId}
                    isDragActive={!!draggingId}
                    onSessionClick={handleSessionClick}
                    hideProjectPill={isFiltered}
                  />
                ) : (
                  <ChannelStreamItem
                    key={item.channelId}
                    channelId={item.channelId}
                    projectSlug={item.projectSlug}
                    activeSessionId={activeSessionId}
                    isDragActive={!!draggingId}
                    onSessionClick={handleSessionClick}
                    hideProjectPill={isFiltered}
                  />
                ),
              )}
            </div>
          </div>
        )}

        {/* Completed section */}
        {recent.length > 0 && (
          <div>
            <SectionLabel
              label="Completed"
              count={recent.length}
              unreadCount={recentUnread}
              collapsible
              expanded={completedExpanded}
              onToggle={() => setCompletedExpanded((v) => !v)}
              accentColor={filterColor?.fg}
              actions={
                <CompletedSectionActions
                  safeCount={safeIds.length}
                  riskyCount={riskyIds.length}
                  onDeleteSafe={() => setBulkDeleteMode("safe")}
                  onDeleteAll={() => setBulkDeleteMode("all")}
                />
              }
            />
            {completedExpanded && (
              <div className="px-1">
                {recent.map((item) =>
                  item.kind === "session" ? (
                    <StreamSessionRow
                      key={item.sessionId}
                      sessionId={item.sessionId}
                      projectSlug={item.projectSlug}
                      activeSessionId={activeSessionId}
                      isDragActive={!!draggingId}
                      onSessionClick={handleSessionClick}
                      hideProjectPill={isFiltered}
                    />
                  ) : (
                    <ChannelStreamItem
                      key={item.channelId}
                      channelId={item.channelId}
                      projectSlug={item.projectSlug}
                      activeSessionId={activeSessionId}
                      isDragActive={!!draggingId}
                      onSessionClick={handleSessionClick}
                      hideProjectPill={isFiltered}
                    />
                  ),
                )}
              </div>
            )}
          </div>
        )}
      </div>

      <DragOverlay>
        {draggingId ? <DraggedSessionPreview sessionId={draggingId} /> : null}
      </DragOverlay>

      <AlertDialog
        open={bulkDeleteMode !== null}
        onOpenChange={(open) => !open && setBulkDeleteMode(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              Delete {bulkDeleteMode === "safe" ? "safe" : "all"} completed sessions
            </AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div>
                <p>
                  This will permanently delete{" "}
                  <strong>
                    {bulkDeleteMode === "safe" ? safeIds.length : safeIds.length + riskyIds.length}
                  </strong>{" "}
                  session(s). This cannot be undone.
                </p>
                {bulkDeleteMode === "all" && riskyIds.length > 0 && (
                  <div className="mt-3 p-2 rounded border border-warning/30 bg-warning/5">
                    <p className="text-sm font-medium text-warning flex items-center gap-1.5">
                      <AlertTriangle className="size-3.5 shrink-0" />
                      {riskyIds.length} session(s) have unmerged worktrees:
                    </p>
                    <ul className="mt-1 text-xs text-muted-foreground space-y-0.5 ml-5 list-disc">
                      {riskySessions.slice(0, 5).map((s) => (
                        <li key={s.id}>
                          {s.name || "Untitled"} ({s.commitsAhead} commit
                          {s.commitsAhead !== 1 ? "s" : ""} ahead)
                        </li>
                      ))}
                      {riskySessions.length > 5 && <li>...and {riskySessions.length - 5} more</li>}
                    </ul>
                  </div>
                )}
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleBulkDelete}>
              Delete {bulkDeleteMode === "safe" ? safeIds.length : safeIds.length + riskyIds.length}{" "}
              session(s)
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </DndContext>
  );
}
