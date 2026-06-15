/** Ungrouped projects block — shown below all folders, with an ungrouped drop zone. */
import { DraggableProject } from "./DraggableProject";
import { DropZone } from "./DropZone";
import type { ProjectEntry } from "./types";
import { LEVEL, UNGROUPED } from "./types";

interface UngroupedSectionProps {
  ungrouped: ProjectEntry[];
  /** Whether any folders are rendered above (drives the "Ungrouped" label). */
  hasFolders: boolean;
  isDragProject: boolean;
  dragSourceFolder: string | null;
  isProjectExpanded: (id: string, hasActive: boolean) => boolean;
  onToggleProject: (id: string) => void;
  onExpandProject: (id: string) => void;
  onTogglePin: (id: string, current: boolean) => void;
  onSessionClick: (id: string) => void;
}

export function UngroupedSection({
  ungrouped,
  hasFolders,
  isDragProject,
  dragSourceFolder,
  isProjectExpanded,
  onToggleProject,
  onExpandProject,
  onTogglePin,
  onSessionClick,
}: UngroupedSectionProps) {
  if (ungrouped.length === 0 && !isDragProject) return null;

  return (
    <div className="mt-3">
      {hasFolders && (
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
          isPinned={entry.project.pinned === 1}
          onToggle={() => onToggleProject(entry.project.id)}
          onExpand={() => onExpandProject(entry.project.id)}
          onTogglePin={() => onTogglePin(entry.project.id, entry.project.pinned === 1)}
          onSessionClick={onSessionClick}
          level={LEVEL.folder}
        />
      ))}
      {isDragProject && dragSourceFolder !== UNGROUPED && (
        <DropZone id="drop:ungrouped" label="Drop to ungrouped" />
      )}
    </div>
  );
}
