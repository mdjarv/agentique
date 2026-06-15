/** Focus-mode sidebar body — flat list of pinned projects only. */
import { useMemo } from "react";
import { DraggableProject } from "./DraggableProject";
import type { FolderGroup, ProjectEntry } from "./types";
import { LEVEL } from "./types";

interface FocusModeListProps {
  orderedFolders: FolderGroup[];
  ungrouped: ProjectEntry[];
  isProjectExpanded: (id: string, hasActive: boolean) => boolean;
  onToggleProject: (id: string) => void;
  onExpandProject: (id: string) => void;
  onTogglePin: (id: string, current: boolean) => void;
  onSessionClick: (id: string) => void;
}

export function FocusModeList({
  orderedFolders,
  ungrouped,
  isProjectExpanded,
  onToggleProject,
  onExpandProject,
  onTogglePin,
  onSessionClick,
}: FocusModeListProps) {
  const visible = useMemo(
    () =>
      [...orderedFolders.flatMap((f) => f.projects), ...ungrouped].filter(
        (e) => e.project.pinned === 1,
      ),
    [orderedFolders, ungrouped],
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

  return (
    <>
      {visible.map((entry) => (
        <DraggableProject
          key={entry.project.id}
          entry={entry}
          expanded={isProjectExpanded(entry.project.id, entry.active.length > 0)}
          compact
          isPinned
          onToggle={() => onToggleProject(entry.project.id)}
          onExpand={() => onExpandProject(entry.project.id)}
          onTogglePin={() => onTogglePin(entry.project.id, true)}
          onSessionClick={onSessionClick}
          level={LEVEL.project}
        />
      ))}
    </>
  );
}
