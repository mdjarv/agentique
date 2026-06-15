/** One folder block — header (or inline rename) plus its project rows and drop zone. */
import { cn } from "~/lib/utils";
import { DraggableProject } from "./DraggableProject";
import { DropZone } from "./DropZone";
import { InlineRename } from "./InlineRename";
import { SortableFolderHeader } from "./SortableFolderHeader";
import type { FolderGroup } from "./types";
import { indentClass, LEVEL } from "./types";

interface FolderSectionProps {
  folder: FolderGroup;
  folderIdx: number;
  expanded: boolean;
  isRenaming: boolean;
  onStartRename: () => void;
  onConfirmRename: (newName: string) => void;
  onCancelRename: () => void;
  onToggle: () => void;
  onDelete: () => void;
  isDragActive: boolean;
  isDragProject: boolean;
  dragSourceFolder: string | null;
  isProjectExpanded: (id: string, hasActive: boolean) => boolean;
  onToggleProject: (id: string) => void;
  onExpandProject: (id: string) => void;
  onTogglePin: (id: string, current: boolean) => void;
  onSessionClick: (id: string) => void;
  onMoveToUngrouped: (id: string) => void;
}

export function FolderSection({
  folder,
  folderIdx,
  expanded,
  isRenaming,
  onStartRename,
  onConfirmRename,
  onCancelRename,
  onToggle,
  onDelete,
  isDragActive,
  isDragProject,
  dragSourceFolder,
  isProjectExpanded,
  onToggleProject,
  onExpandProject,
  onTogglePin,
  onSessionClick,
  onMoveToUngrouped,
}: FolderSectionProps) {
  const totalActive = folder.projects.reduce((s, p) => s + p.active.length, 0);
  const hasAttention = folder.projects.some((p) => p.worstState);

  return (
    <div className={cn("mb-2", folderIdx > 0 && "mt-1 pt-1 border-t border-sidebar-border/30")}>
      {isRenaming ? (
        <InlineRename
          initialValue={folder.name}
          onConfirm={onConfirmRename}
          onCancel={onCancelRename}
        />
      ) : (
        <SortableFolderHeader
          name={folder.name}
          expanded={expanded}
          onToggle={onToggle}
          projectCount={folder.projects.length}
          activeCount={totalActive}
          hasAttention={hasAttention}
          onRename={onStartRename}
          onDelete={onDelete}
          isDragActive={isDragActive}
        />
      )}

      {expanded && (
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
              expanded={isProjectExpanded(entry.project.id, entry.active.length > 0)}
              isPinned={entry.project.pinned === 1}
              onToggle={() => onToggleProject(entry.project.id)}
              onExpand={() => onExpandProject(entry.project.id)}
              onTogglePin={() => onTogglePin(entry.project.id, entry.project.pinned === 1)}
              onSessionClick={onSessionClick}
              onMoveToUngrouped={() => onMoveToUngrouped(entry.project.id)}
              level={LEVEL.project}
            />
          ))}
          {isDragProject && dragSourceFolder !== folder.name && (
            <DropZone id={`folder:${folder.name}`} label={`Drop into ${folder.name}`} />
          )}
        </div>
      )}
    </div>
  );
}
