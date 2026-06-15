/**
 * Folder-based sidebar — orchestrator component.
 *
 * Wires the folder-sidebar hooks (order, mutations, dnd, expand/collapse) to the
 * section components (FocusModeList / FolderSection / UngroupedSection) and owns
 * only the cross-cutting concerns: focus mode, pinning, and session navigation.
 */
import { DndContext, DragOverlay } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { useNavigate } from "@tanstack/react-router";
import { ChevronsDownUp, ChevronsUpDown, Eye, EyeOff, FolderPlus } from "lucide-react";
import { useCallback, useMemo } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { setProjectPinned } from "~/lib/project-actions";
import { cn, getErrorMessage, sessionShortId } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useAuthStore } from "~/stores/auth-store";
import { useChatStore } from "~/stores/chat-store";
import { useUIStore } from "~/stores/ui-store";
import { DragOverlayProject } from "./folder-sidebar/DraggableProject";
import { FocusModeList } from "./folder-sidebar/FocusModeList";
import { FolderContent } from "./folder-sidebar/FolderHeader";
import { FolderSection } from "./folder-sidebar/FolderSection";
import { InlineRename } from "./folder-sidebar/InlineRename";
import { SidebarRow } from "./folder-sidebar/SidebarRow";
import { UngroupedSection } from "./folder-sidebar/UngroupedSection";
import { useExpandCollapse } from "./folder-sidebar/use-expand-collapse";
import { useFolderGroups } from "./folder-sidebar/use-folder-groups";
import { useFolderMutations } from "./folder-sidebar/use-folder-mutations";
import { useFolderOrder } from "./folder-sidebar/use-folder-order";
import { useSidebarDnd } from "./folder-sidebar/use-sidebar-dnd";

export function FolderSidebar() {
  const navigate = useNavigate();
  const ws = useWebSocket();
  const { folders, ungrouped } = useFolderGroups();
  const projects = useAppStore((s) => s.projects);

  const mutations = useFolderMutations(projects);

  const allFolders = useMemo(() => {
    const realNames = new Set(folders.map((f) => f.name));
    const empties = mutations.localEmptyFolders
      .filter((n) => !realNames.has(n))
      .map((name) => ({ name, projects: [] }));
    return [...folders, ...empties];
  }, [folders, mutations.localEmptyFolders]);

  const { orderedFolders, moveFolder, renameInFolderOrder } = useFolderOrder(allFolders);

  const dnd = useSidebarDnd({
    orderedFolders,
    ungrouped,
    setProjectFolder: mutations.setProjectFolder,
    moveFolder,
  });

  const expand = useExpandCollapse({ orderedFolders, ungrouped });

  // ── Focus mode (server-backed when signed in, local otherwise) ──

  const renameFolderExpanded = useUIStore((s) => s.renameFolderExpanded);
  const localFocusMode = useUIStore((s) => s.sidebarFocusMode);
  const setLocalFocusMode = useUIStore((s) => s.setSidebarFocusMode);
  const authUser = useAuthStore((s) => s.user);
  const setAuthFocusMode = useAuthStore((s) => s.setSidebarFocusMode);

  const focusMode = authUser?.sidebarFocusMode ?? localFocusMode;
  const toggleFocusMode = useCallback(() => {
    const next = !focusMode;
    if (authUser) {
      setAuthFocusMode(next).catch((err) =>
        toast.error(getErrorMessage(err, "Failed to update focus mode")),
      );
    } else {
      setLocalFocusMode(next);
    }
  }, [focusMode, authUser, setAuthFocusMode, setLocalFocusMode]);

  const togglePinned = useCallback(
    (projectId: string, current: boolean) => {
      setProjectPinned(ws, projectId, !current).catch((err) =>
        toast.error(getErrorMessage(err, "Failed to update pin")),
      );
    },
    [ws],
  );

  const handleSessionClick = useCallback(
    (sessionId: string) => {
      const data = useChatStore.getState().sessions[sessionId];
      if (!data) return;
      const project = projects.find((p) => p.id === data.meta.projectId);
      if (!project) return;
      useAppStore.getState().setSidebarOpen(false);
      navigate({
        to: "/project/$projectSlug/session/$sessionShortId",
        params: { projectSlug: project.slug, sessionShortId: sessionShortId(sessionId) },
      });
    },
    [navigate, projects],
  );

  // Renaming a folder spans three owners: project records (mutations), persisted
  // order, and expanded state — sequenced here to keep the hooks independent.
  const handleRenameFolder = useCallback(
    async (oldName: string, newName: string) => {
      await mutations.renameFolderProjects(oldName, newName);
      renameFolderExpanded(oldName, newName);
      renameInFolderOrder(oldName, newName);
    },
    [mutations.renameFolderProjects, renameFolderExpanded, renameInFolderOrder],
  );

  return (
    <div className="flex-1 flex flex-col min-w-0 min-h-0">
      <DndContext
        sensors={dnd.sensors}
        onDragStart={dnd.handleDragStart}
        onDragCancel={dnd.handleDragCancel}
        onDragEnd={dnd.handleDragEnd}
      >
        <div className="flex-1 overflow-y-auto min-h-0 py-2 px-2">
          <SortableContext items={dnd.allSortableIds} strategy={verticalListSortingStrategy}>
            {focusMode ? (
              <FocusModeList
                orderedFolders={orderedFolders}
                ungrouped={ungrouped}
                isProjectExpanded={expand.isProjectExpanded}
                onToggleProject={expand.toggleProject}
                onExpandProject={expand.expandProject}
                onTogglePin={togglePinned}
                onSessionClick={handleSessionClick}
              />
            ) : (
              <>
                {orderedFolders.map((folder, folderIdx) => (
                  <FolderSection
                    key={folder.name}
                    folder={folder}
                    folderIdx={folderIdx}
                    expanded={expand.isFolderExpanded(folder.name)}
                    isRenaming={mutations.renamingFolder === folder.name}
                    onStartRename={() => mutations.setRenamingFolder(folder.name)}
                    onConfirmRename={(n) => handleRenameFolder(folder.name, n)}
                    onCancelRename={() => mutations.setRenamingFolder(null)}
                    onToggle={() => expand.toggleFolder(folder.name)}
                    onDelete={() => mutations.deleteFolder(folder.name)}
                    isDragActive={dnd.isDragActive}
                    isDragProject={dnd.isDragProject}
                    dragSourceFolder={dnd.dragSourceFolder}
                    isProjectExpanded={expand.isProjectExpanded}
                    onToggleProject={expand.toggleProject}
                    onExpandProject={expand.expandProject}
                    onTogglePin={togglePinned}
                    onSessionClick={handleSessionClick}
                    onMoveToUngrouped={(id) => mutations.setProjectFolder(id, "")}
                  />
                ))}

                <UngroupedSection
                  ungrouped={ungrouped}
                  hasFolders={orderedFolders.length > 0}
                  isDragProject={dnd.isDragProject}
                  dragSourceFolder={dnd.dragSourceFolder}
                  isProjectExpanded={expand.isProjectExpanded}
                  onToggleProject={expand.toggleProject}
                  onExpandProject={expand.expandProject}
                  onTogglePin={togglePinned}
                  onSessionClick={handleSessionClick}
                />
              </>
            )}
          </SortableContext>
        </div>

        <DragOverlay>
          {dnd.draggedEntry && <DragOverlayProject entry={dnd.draggedEntry} />}
          {dnd.draggedFolderName && (
            <SidebarRow
              as="div"
              indent={0}
              className="shadow-lg shadow-black/20 rounded-md bg-sidebar"
            >
              <FolderContent
                name={dnd.draggedFolderName}
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
        {mutations.creatingFolder ? (
          <InlineRename
            initialValue=""
            onConfirm={mutations.createFolder}
            onCancel={() => mutations.setCreatingFolder(false)}
          />
        ) : (
          <div className="flex items-center justify-between">
            <button
              type="button"
              onClick={() => mutations.setCreatingFolder(true)}
              className="flex items-center gap-1.5 px-1 py-1 text-[10px] text-muted-foreground-faint hover:text-muted-foreground transition-colors cursor-pointer rounded hover:bg-sidebar-accent/30"
            >
              <FolderPlus className="size-3" />
              New folder
            </button>
            <div className="flex items-center gap-0.5">
              <button
                type="button"
                onClick={expand.toggleCollapseAll}
                title={expand.anythingExpanded ? "Collapse all" : "Expand all"}
                className="flex items-center px-1 py-1 text-muted-foreground-faint hover:text-muted-foreground hover:bg-sidebar-accent/30 transition-colors cursor-pointer rounded"
              >
                {expand.anythingExpanded ? (
                  <ChevronsDownUp className="size-3" />
                ) : (
                  <ChevronsUpDown className="size-3" />
                )}
              </button>
              <button
                type="button"
                onClick={toggleFocusMode}
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
