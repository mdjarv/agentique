/**
 * Folder-based sidebar — orchestrator component.
 *
 * Composes SidebarRow + content components (FolderContent, ProjectContent,
 * SessionContent) with layout concerns: indent levels, drag-and-drop,
 * context menus, drop zones.
 */
import {
  DndContext,
  type DragEndEvent,
  DragOverlay,
  type DragStartEvent,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { useNavigate } from "@tanstack/react-router";
import { ChevronsDownUp, ChevronsUpDown, Eye, EyeOff, FolderPlus } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { updateProject } from "~/lib/api";
import { cn } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";
import { useUIStore } from "~/stores/ui-store";
import { DraggableProject, DragOverlayProject } from "./folder-sidebar/DraggableProject";
import { DropZone } from "./folder-sidebar/DropZone";
import { FolderContent } from "./folder-sidebar/FolderHeader";
import { InlineRename } from "./folder-sidebar/InlineRename";
import { SidebarRow } from "./folder-sidebar/SidebarRow";
import { SortableFolderHeader } from "./folder-sidebar/SortableFolderHeader";
import { indentClass, LEVEL, UNGROUPED } from "./folder-sidebar/types";
import { useFolderGroups } from "./folder-sidebar/use-folder-groups";

// ─── Folder order persistence ───────────────────────────────────

const FOLDER_ORDER_KEY = "agentique-folder-order";

function loadFolderOrder(): string[] {
  try {
    const stored = localStorage.getItem(FOLDER_ORDER_KEY);
    return stored ? JSON.parse(stored) : [];
  } catch {
    return [];
  }
}

function saveFolderOrder(order: string[]): void {
  localStorage.setItem(FOLDER_ORDER_KEY, JSON.stringify(order));
}

/** Reconcile stored order with current folder names — drop stale, append new. */
function reconcileFolderOrder(stored: string[], current: string[]): string[] {
  const currentSet = new Set(current);
  const reconciled = stored.filter((n) => currentSet.has(n));
  const reconciledSet = new Set(reconciled);
  for (const name of current) {
    if (!reconciledSet.has(name)) reconciled.push(name);
  }
  return reconciled;
}

// ─── Main ────────────────────────────────────────────────────────

export function FolderSidebar() {
  const navigate = useNavigate();
  const { folders, ungrouped } = useFolderGroups();

  const expandedFolders = useUIStore((s) => s.expandedFolders);
  const expandedProjects = useUIStore((s) => s.expandedProjects);
  const pinnedProjectIds = useUIStore((s) => s.pinnedProjectIds);
  const focusMode = useUIStore((s) => s.sidebarFocusMode);
  const setFolderExpanded = useUIStore((s) => s.setFolderExpanded);
  const setManyFoldersExpanded = useUIStore((s) => s.setManyFoldersExpanded);
  const setProjectExpanded = useUIStore((s) => s.setProjectExpanded);
  const setManyProjectsExpanded = useUIStore((s) => s.setManyProjectsExpanded);
  const renameFolderExpanded = useUIStore((s) => s.renameFolderExpanded);
  const toggleProjectPinned = useUIStore((s) => s.toggleProjectPinned);
  const setSidebarFocusMode = useUIStore((s) => s.setSidebarFocusMode);

  const pinnedSet = useMemo(() => new Set(pinnedProjectIds), [pinnedProjectIds]);

  const [renamingFolder, setRenamingFolder] = useState<string | null>(null);
  const [creatingFolder, setCreatingFolder] = useState(false);
  const [localEmptyFolders, setLocalEmptyFolders] = useState<string[]>([]);

  const projects = useAppStore((s) => s.projects);

  const handleSessionClick = useCallback(
    (sessionId: string) => {
      const data = useChatStore.getState().sessions[sessionId];
      if (!data) return;
      const project = projects.find((p) => p.id === data.meta.projectId);
      if (!project) return;
      useAppStore.getState().setSidebarOpen(false);
      navigate({
        to: "/project/$projectSlug/session/$sessionShortId",
        params: { projectSlug: project.slug, sessionShortId: sessionId.split("-")[0] ?? "" },
      });
    },
    [navigate, projects],
  );

  const isProjectExpanded = useCallback(
    (projectId: string, hasActive: boolean) => {
      if (projectId in expandedProjects) return expandedProjects[projectId] ?? false;
      return hasActive;
    },
    [expandedProjects],
  );

  // ── Folder operations ──

  const setProjectFolder = useCallback(async (projectId: string, folder: string) => {
    try {
      const updated = await updateProject(projectId, { folder });
      useAppStore.getState().updateProject(updated);
    } catch (e) {
      console.error("Failed to update project folder:", e);
    }
  }, []);

  const renameFolder = useCallback(
    async (oldName: string, newName: string) => {
      const inFolder = projects.filter((p) => p.folder === oldName);
      await Promise.all(inFolder.map((p) => setProjectFolder(p.id, newName)));
      setRenamingFolder(null);
      renameFolderExpanded(oldName, newName);
      setLocalEmptyFolders((prev) => prev.map((f) => (f === oldName ? newName : f)));
      setFolderOrder((prev) => {
        const next = prev.map((f) => (f === oldName ? newName : f));
        saveFolderOrder(next);
        return next;
      });
    },
    [projects, setProjectFolder, renameFolderExpanded],
  );

  const deleteFolder = useCallback(
    async (folderName: string) => {
      const inFolder = projects.filter((p) => p.folder === folderName);
      await Promise.all(inFolder.map((p) => setProjectFolder(p.id, "")));
      setLocalEmptyFolders((prev) => prev.filter((f) => f !== folderName));
    },
    [projects, setProjectFolder],
  );

  const createFolder = useCallback(
    (name: string) => {
      setCreatingFolder(false);
      if (!name) return;
      setLocalEmptyFolders((prev) => [...prev, name]);
      setFolderExpanded(name, true);
    },
    [setFolderExpanded],
  );

  const allFolders = useMemo(() => {
    const realNames = new Set(folders.map((f) => f.name));
    const empties = localEmptyFolders
      .filter((n) => !realNames.has(n))
      .map((name) => ({ name, projects: [] }));
    return [...folders, ...empties];
  }, [folders, localEmptyFolders]);

  // ── Folder ordering (persisted in localStorage) ──

  const [folderOrder, setFolderOrder] = useState<string[]>(loadFolderOrder);

  const orderedFolders = useMemo(() => {
    const currentNames = allFolders.map((f) => f.name);
    const reconciled = reconcileFolderOrder(folderOrder, currentNames);
    const orderMap = new Map(reconciled.map((name, idx) => [name, idx]));
    return [...allFolders].sort(
      (a, b) => (orderMap.get(a.name) ?? Infinity) - (orderMap.get(b.name) ?? Infinity),
    );
  }, [allFolders, folderOrder]);

  // ── Drag and drop ──

  const [draggedId, setDraggedId] = useState<string | null>(null);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { delay: 200, tolerance: 5 } }),
    useSensor(KeyboardSensor),
  );

  const allSortableIds = useMemo(
    () => orderedFolders.map((f) => `folder-sort:${f.name}`),
    [orderedFolders],
  );

  const projectFolderMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const f of orderedFolders) {
      for (const p of f.projects) map.set(p.project.id, f.name);
    }
    for (const p of ungrouped) map.set(p.project.id, UNGROUPED);
    return map;
  }, [orderedFolders, ungrouped]);

  const handleDragStart = useCallback((event: DragStartEvent) => {
    setDraggedId(event.active.id as string);
  }, []);

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      setDraggedId(null);
      const { active, over } = event;
      if (!over) return;

      const activeId = active.id as string;
      const overId = over.id as string;
      const isFolderDrag = activeId.startsWith("folder-sort:");

      // Folder reordering: folder dropped on another folder
      if (isFolderDrag && overId.startsWith("folder-sort:")) {
        const activeName = activeId.slice("folder-sort:".length);
        const overName = overId.slice("folder-sort:".length);
        if (activeName === overName) return;
        setFolderOrder((prev) => {
          const currentNames = orderedFolders.map((f) => f.name);
          const base = reconcileFolderOrder(prev, currentNames);
          const oldIdx = base.indexOf(activeName);
          const newIdx = base.indexOf(overName);
          if (oldIdx < 0 || newIdx < 0) return prev;
          base.splice(oldIdx, 1);
          base.splice(newIdx, 0, activeName);
          saveFolderOrder(base);
          return base;
        });
        return;
      }

      // Project dropped on folder-sort header → move to that folder
      if (!isFolderDrag && overId.startsWith("folder-sort:")) {
        const targetFolder = overId.slice("folder-sort:".length);
        if (projectFolderMap.get(activeId) !== targetFolder) {
          setProjectFolder(activeId, targetFolder);
        }
        return;
      }

      // Project dropped on empty-folder DropZone
      if (overId.startsWith("folder:")) {
        const targetFolder = overId.slice("folder:".length);
        if (projectFolderMap.get(activeId) !== targetFolder) {
          setProjectFolder(activeId, targetFolder);
        }
        return;
      }

      if (overId === "drop:ungrouped") {
        if (projectFolderMap.get(activeId) !== UNGROUPED) {
          setProjectFolder(activeId, "");
        }
        return;
      }

      // folder dropped on non-folder target — ignore
      if (isFolderDrag) return;
    },
    [projectFolderMap, setProjectFolder, orderedFolders],
  );

  const draggedEntry = useMemo(() => {
    if (!draggedId || draggedId.startsWith("folder-sort:")) return null;
    for (const f of orderedFolders) {
      const e = f.projects.find((p) => p.project.id === draggedId);
      if (e) return e;
    }
    return ungrouped.find((p) => p.project.id === draggedId) ?? null;
  }, [draggedId, orderedFolders, ungrouped]);

  const draggedFolderName = useMemo(() => {
    if (!draggedId?.startsWith("folder-sort:")) return null;
    return draggedId.slice("folder-sort:".length);
  }, [draggedId]);

  const isDragActive = !!draggedId;
  const isDragProject = isDragActive && !draggedId?.startsWith("folder-sort:");
  const dragSourceFolder =
    isDragProject && draggedId ? (projectFolderMap.get(draggedId) ?? null) : null;

  // ── Helpers for toggle callbacks ──

  const toggleFolder = useCallback(
    (name: string) => setFolderExpanded(name, !(expandedFolders[name] ?? true)),
    [expandedFolders, setFolderExpanded],
  );
  const toggleProject = useCallback(
    (id: string) => setProjectExpanded(id, !expandedProjects[id]),
    [expandedProjects, setProjectExpanded],
  );
  const expandProject = useCallback(
    (id: string) => setProjectExpanded(id, true),
    [setProjectExpanded],
  );

  // Sticky expand: once a project has an active session, record expand=true so
  // the user's inline session view doesn't silently collapse when activity ends.
  // Pin (for focus mode) is a separate, user-curated axis — see toggleProjectPinned.
  useEffect(() => {
    const toExpand: string[] = [];
    const visit = (id: string, hasActive: boolean) => {
      if (hasActive && !(id in expandedProjects)) toExpand.push(id);
    };
    for (const f of orderedFolders) {
      for (const e of f.projects) visit(e.project.id, e.active.length > 0);
    }
    for (const e of ungrouped) visit(e.project.id, e.active.length > 0);
    if (toExpand.length > 0) setManyProjectsExpanded(toExpand, true);
  }, [orderedFolders, ungrouped, expandedProjects, setManyProjectsExpanded]);

  const allProjectIds = useMemo(() => {
    const ids: string[] = [];
    for (const f of orderedFolders) for (const e of f.projects) ids.push(e.project.id);
    for (const e of ungrouped) ids.push(e.project.id);
    return ids;
  }, [orderedFolders, ungrouped]);

  const allFolderNames = useMemo(() => orderedFolders.map((f) => f.name), [orderedFolders]);

  const collapseAll = useCallback(() => {
    setManyFoldersExpanded(allFolderNames, false);
    setManyProjectsExpanded(allProjectIds, false);
  }, [allFolderNames, allProjectIds, setManyFoldersExpanded, setManyProjectsExpanded]);

  const expandAll = useCallback(() => {
    setManyFoldersExpanded(allFolderNames, true);
    setManyProjectsExpanded(allProjectIds, true);
  }, [allFolderNames, allProjectIds, setManyFoldersExpanded, setManyProjectsExpanded]);

  return (
    <div className="flex-1 flex flex-col min-w-0 min-h-0">
      <DndContext
        sensors={sensors}
        onDragStart={handleDragStart}
        onDragCancel={() => setDraggedId(null)}
        onDragEnd={handleDragEnd}
      >
        <div className="flex-1 overflow-y-auto min-h-0 py-2 px-2">
          <SortableContext items={allSortableIds} strategy={verticalListSortingStrategy}>
            {focusMode ? (
              (() => {
                const visible = [...orderedFolders.flatMap((f) => f.projects), ...ungrouped].filter(
                  (e) => pinnedSet.has(e.project.id),
                );
                if (visible.length === 0) {
                  return (
                    <div className="px-3 py-6 text-[11px] text-muted-foreground-faint text-center leading-relaxed">
                      No pinned projects.
                      <br />
                      Right-click a project to pin it.
                    </div>
                  );
                }
                return visible.map((entry) => (
                  <DraggableProject
                    key={entry.project.id}
                    entry={entry}
                    expanded={isProjectExpanded(entry.project.id, entry.active.length > 0)}
                    compact
                    isPinned
                    onToggle={() => toggleProject(entry.project.id)}
                    onExpand={() => expandProject(entry.project.id)}
                    onTogglePin={() => toggleProjectPinned(entry.project.id)}
                    onSessionClick={handleSessionClick}
                    level={LEVEL.project}
                  />
                ));
              })()
            ) : (
              <>
                {orderedFolders.map((folder, folderIdx) => {
                  const folderExpanded = expandedFolders[folder.name] ?? true;
                  const totalActive = folder.projects.reduce((s, p) => s + p.active.length, 0);
                  const hasAttention = folder.projects.some((p) => p.worstState);

                  return (
                    <div
                      key={folder.name}
                      className={cn(
                        "mb-2",
                        folderIdx > 0 && "mt-1 pt-1 border-t border-sidebar-border/30",
                      )}
                    >
                      {renamingFolder === folder.name ? (
                        <InlineRename
                          initialValue={folder.name}
                          onConfirm={(n) => renameFolder(folder.name, n)}
                          onCancel={() => setRenamingFolder(null)}
                        />
                      ) : (
                        <SortableFolderHeader
                          name={folder.name}
                          expanded={folderExpanded}
                          onToggle={() => toggleFolder(folder.name)}
                          projectCount={folder.projects.length}
                          activeCount={totalActive}
                          hasAttention={hasAttention}
                          onRename={() => setRenamingFolder(folder.name)}
                          onDelete={() => deleteFolder(folder.name)}
                          isDragActive={isDragActive}
                        />
                      )}

                      {folderExpanded && (
                        <div>
                          {folder.projects.length === 0 && !isDragProject && (
                            <div
                              className={`${indentClass(LEVEL.project + 1)} py-2 text-[10px] text-muted-foreground-faint italic`}
                            >
                              Drag projects here
                            </div>
                          )}
                          {folder.projects.map((entry) => (
                            <DraggableProject
                              key={entry.project.id}
                              entry={entry}
                              expanded={isProjectExpanded(
                                entry.project.id,
                                entry.active.length > 0,
                              )}
                              isPinned={pinnedSet.has(entry.project.id)}
                              onToggle={() => toggleProject(entry.project.id)}
                              onExpand={() => expandProject(entry.project.id)}
                              onTogglePin={() => toggleProjectPinned(entry.project.id)}
                              onSessionClick={handleSessionClick}
                              onMoveToUngrouped={() => setProjectFolder(entry.project.id, "")}
                              level={LEVEL.project}
                            />
                          ))}
                          {isDragProject && dragSourceFolder !== folder.name && (
                            <DropZone
                              id={`folder:${folder.name}`}
                              label={`Drop into ${folder.name}`}
                            />
                          )}
                        </div>
                      )}
                    </div>
                  );
                })}

                {(ungrouped.length > 0 || isDragProject) && (
                  <div className="mt-3">
                    {orderedFolders.length > 0 && (
                      <div className="flex items-center py-1 mb-0.5">
                        <span className="text-[10px] font-semibold text-muted-foreground-faint uppercase tracking-wider">
                          Ungrouped
                        </span>
                      </div>
                    )}
                    {ungrouped.map((entry) => (
                      <DraggableProject
                        key={entry.project.id}
                        entry={entry}
                        expanded={isProjectExpanded(entry.project.id, entry.active.length > 0)}
                        isPinned={pinnedSet.has(entry.project.id)}
                        onToggle={() => toggleProject(entry.project.id)}
                        onExpand={() => expandProject(entry.project.id)}
                        onTogglePin={() => toggleProjectPinned(entry.project.id)}
                        onSessionClick={handleSessionClick}
                        level={LEVEL.folder}
                      />
                    ))}
                    {isDragProject && dragSourceFolder !== UNGROUPED && (
                      <DropZone id="drop:ungrouped" label="Drop to ungrouped" />
                    )}
                  </div>
                )}
              </>
            )}
          </SortableContext>
        </div>

        <DragOverlay>
          {draggedEntry && <DragOverlayProject entry={draggedEntry} />}
          {draggedFolderName && (
            <SidebarRow
              as="div"
              indent={0}
              className="shadow-lg shadow-black/20 rounded-md bg-sidebar"
            >
              <FolderContent
                name={draggedFolderName}
                expanded={false}
                projectCount={0}
                activeCount={0}
                hasAttention={false}
              />
            </SidebarRow>
          )}
        </DragOverlay>
      </DndContext>

      {/* Footer */}
      <div className="px-2 py-1.5 border-t border-border/50">
        {creatingFolder ? (
          <InlineRename
            initialValue=""
            onConfirm={createFolder}
            onCancel={() => setCreatingFolder(false)}
          />
        ) : (
          <div className="flex items-center justify-between">
            <button
              type="button"
              onClick={() => setCreatingFolder(true)}
              className="flex items-center gap-1.5 px-1 py-1 text-[10px] text-muted-foreground-faint hover:text-muted-foreground transition-colors cursor-pointer rounded hover:bg-sidebar-accent/30"
            >
              <FolderPlus className="size-3" />
              New folder
            </button>
            <div className="flex items-center gap-0.5">
              <button
                type="button"
                onClick={collapseAll}
                title="Collapse all"
                className="flex items-center px-1 py-1 text-muted-foreground-faint hover:text-muted-foreground hover:bg-sidebar-accent/30 transition-colors cursor-pointer rounded"
              >
                <ChevronsDownUp className="size-3" />
              </button>
              <button
                type="button"
                onClick={expandAll}
                title="Expand all"
                className="flex items-center px-1 py-1 text-muted-foreground-faint hover:text-muted-foreground hover:bg-sidebar-accent/30 transition-colors cursor-pointer rounded"
              >
                <ChevronsUpDown className="size-3" />
              </button>
              <button
                type="button"
                onClick={() => setSidebarFocusMode(!focusMode)}
                title={focusMode ? "Show all projects" : "Show only expanded"}
                className={cn(
                  "flex items-center gap-1 px-1.5 py-1 text-[10px] transition-colors cursor-pointer rounded",
                  focusMode
                    ? "text-primary bg-primary/10 hover:bg-primary/20"
                    : "text-muted-foreground-faint hover:text-muted-foreground hover:bg-sidebar-accent/30",
                )}
              >
                {focusMode ? <Eye className="size-3" /> : <EyeOff className="size-3" />}
                Focus
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
