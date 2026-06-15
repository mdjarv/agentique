/**
 * Sidebar drag-and-drop — dnd-kit sensors plus the drag-end routing that maps a
 * drop target to either a folder reorder or a project↔folder move. Also derives
 * the drag-overlay state consumed by the orchestrator.
 */
import {
  type DragEndEvent,
  type DragStartEvent,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { useCallback, useMemo, useState } from "react";
import type { FolderGroup, ProjectEntry } from "./types";
import { UNGROUPED } from "./types";

const FOLDER_SORT_PREFIX = "folder-sort:";

interface UseSidebarDndArgs {
  orderedFolders: FolderGroup[];
  ungrouped: ProjectEntry[];
  /** Persist a project's folder assignment (`""` = ungrouped). */
  setProjectFolder: (projectId: string, folder: string) => void;
  /** Reorder folders (folder dropped on another folder). */
  moveFolder: (activeName: string, overName: string) => void;
}

export interface SidebarDnd {
  sensors: ReturnType<typeof useSensors>;
  /** Sortable ids for the folder `SortableContext`. */
  allSortableIds: string[];
  handleDragStart: (event: DragStartEvent) => void;
  handleDragCancel: () => void;
  handleDragEnd: (event: DragEndEvent) => void;
  /** Project entry under the cursor while dragging a project, else null. */
  draggedEntry: ProjectEntry | null;
  /** Folder name being dragged, else null. */
  draggedFolderName: string | null;
  isDragActive: boolean;
  isDragProject: boolean;
  /** Folder the dragged project came from (drives drop-zone suppression). */
  dragSourceFolder: string | null;
}

export function useSidebarDnd({
  orderedFolders,
  ungrouped,
  setProjectFolder,
  moveFolder,
}: UseSidebarDndArgs): SidebarDnd {
  const [draggedId, setDraggedId] = useState<string | null>(null);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { delay: 200, tolerance: 5 } }),
    useSensor(KeyboardSensor),
  );

  const allSortableIds = useMemo(
    () => orderedFolders.map((f) => `${FOLDER_SORT_PREFIX}${f.name}`),
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

  const handleDragCancel = useCallback(() => setDraggedId(null), []);

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      setDraggedId(null);
      const { active, over } = event;
      if (!over) return;

      const activeId = active.id as string;
      const overId = over.id as string;
      const isFolderDrag = activeId.startsWith(FOLDER_SORT_PREFIX);

      // Folder reordering: folder dropped on another folder
      if (isFolderDrag && overId.startsWith(FOLDER_SORT_PREFIX)) {
        const activeName = activeId.slice(FOLDER_SORT_PREFIX.length);
        const overName = overId.slice(FOLDER_SORT_PREFIX.length);
        if (activeName === overName) return;
        moveFolder(activeName, overName);
        return;
      }

      // Project dropped on folder-sort header → move to that folder
      if (!isFolderDrag && overId.startsWith(FOLDER_SORT_PREFIX)) {
        const targetFolder = overId.slice(FOLDER_SORT_PREFIX.length);
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
    [projectFolderMap, setProjectFolder, moveFolder],
  );

  const draggedEntry = useMemo(() => {
    if (!draggedId || draggedId.startsWith(FOLDER_SORT_PREFIX)) return null;
    for (const f of orderedFolders) {
      const e = f.projects.find((p) => p.project.id === draggedId);
      if (e) return e;
    }
    return ungrouped.find((p) => p.project.id === draggedId) ?? null;
  }, [draggedId, orderedFolders, ungrouped]);

  const draggedFolderName = useMemo(() => {
    if (!draggedId?.startsWith(FOLDER_SORT_PREFIX)) return null;
    return draggedId.slice(FOLDER_SORT_PREFIX.length);
  }, [draggedId]);

  const isDragActive = !!draggedId;
  const isDragProject = isDragActive && !draggedId?.startsWith(FOLDER_SORT_PREFIX);
  const dragSourceFolder =
    isDragProject && draggedId ? (projectFolderMap.get(draggedId) ?? null) : null;

  return {
    sensors,
    allSortableIds,
    handleDragStart,
    handleDragCancel,
    handleDragEnd,
    draggedEntry,
    draggedFolderName,
    isDragActive,
    isDragProject,
    dragSourceFolder,
  };
}
