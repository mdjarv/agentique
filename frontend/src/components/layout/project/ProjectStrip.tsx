import { memo, useCallback, useMemo } from "react";
import { useShallow } from "zustand/shallow";
import { ScrollArea, ScrollBar } from "~/components/ui/scroll-area";
import { useProjectActivity } from "~/hooks/useProjectActivity";
import { useTheme } from "~/hooks/useTheme";
import { getProjectColor } from "~/lib/project-colors";
import type { Project } from "~/lib/types";
import { cn } from "~/lib/utils";
import { type ProjectGitStatus, useAppStore } from "~/stores/app-store";

interface ProjectStripProps {
  selectedProjectId: string | null;
  onSelectProject: (projectId: string | null) => void;
}

const EMPTY_GIT: ProjectGitStatus | undefined = undefined;

/** Horizontal scrollable project chip strip for mobile sidebar. */
export const ProjectStrip = memo(function ProjectStrip({
  selectedProjectId,
  onSelectProject,
}: ProjectStripProps) {
  const projects = useAppStore((s) => s.projects);
  const projectIds = useAppStore(useShallow((s) => s.projects.map((p) => p.id)));

  const sorted = useMemo(() => {
    return [...projects].sort((a, b) => {
      if (a.favorite !== b.favorite) return b.favorite - a.favorite;
      const aOrder = a.sort_order || Number.MAX_SAFE_INTEGER;
      const bOrder = b.sort_order || Number.MAX_SAFE_INTEGER;
      if (aOrder !== bOrder) return aOrder - bOrder;
      return a.name.localeCompare(b.name);
    });
  }, [projects]);

  const favCount = useMemo(() => sorted.filter((p) => p.favorite).length, [sorted]);
  const hasBothGroups = favCount > 0 && favCount < sorted.length;

  return (
    <div className="border-b border-border/50">
      <ScrollArea className="w-full">
        <div className="flex items-center gap-2 px-3 py-2">
          {/* "All" chip */}
          <button
            type="button"
            onClick={() => onSelectProject(null)}
            className={cn(
              "shrink-0 rounded-full px-3 py-1 text-xs font-medium transition-colors",
              selectedProjectId === null
                ? "bg-primary/15 text-primary ring-1 ring-primary/50"
                : "bg-muted/50 text-muted-foreground",
            )}
          >
            All
          </button>

          {sorted.map((project, i) => (
            <StripChipGroup key={project.id}>
              {hasBothGroups && i === favCount && <div className="w-px h-4 bg-border shrink-0" />}
              <StripChip
                project={project}
                projectIds={projectIds}
                isSelected={selectedProjectId === project.id}
                onSelect={onSelectProject}
              />
            </StripChipGroup>
          ))}
        </div>
        <ScrollBar orientation="horizontal" />
      </ScrollArea>
    </div>
  );
});

/** Fragment wrapper to allow separator + chip as siblings in the map. */
function StripChipGroup({ children }: { children: React.ReactNode }) {
  return <>{children}</>;
}

const StripChip = memo(function StripChip({
  project,
  projectIds,
  isSelected,
  onSelect,
}: {
  project: Project;
  projectIds: string[];
  isSelected: boolean;
  onSelect: (id: string | null) => void;
}) {
  const gitStatus = useAppStore((s) => s.projectGitStatus[project.id] ?? EMPTY_GIT);
  const activity = useProjectActivity(project.id);
  const { resolvedTheme } = useTheme();
  const color = useMemo(
    () => getProjectColor(project.color, project.id, projectIds, resolvedTheme),
    [project.color, project.id, projectIds, resolvedTheme],
  );

  const handleClick = useCallback(() => onSelect(project.id), [onSelect, project.id]);

  const ahead = gitStatus && gitStatus.aheadRemote > 0;
  const behind = gitStatus && gitStatus.behindRemote > 0;

  // Activity dot color for the chip's leading indicator
  const activityDotClass = useMemo(() => {
    if (activity.attentionCount > 0) return "bg-orange animate-pulse";
    if (activity.runningCount > 0) return "bg-teal animate-pulse";
    if (activity.failedCount > 0) return "bg-destructive";
    if (activity.unseenCount > 0) return "bg-success";
    return null;
  }, [activity]);

  return (
    <button
      type="button"
      onClick={handleClick}
      className={cn(
        "shrink-0 rounded-full px-2.5 py-1 text-xs font-medium flex items-center gap-1.5 transition-colors",
        isSelected && "ring-1",
      )}
      style={{
        backgroundColor: `${color.bg}15`,
        color: color.fg,
        outlineColor: isSelected ? color.fg : undefined,
      }}
    >
      <span
        className={cn("size-[7px] rounded-full shrink-0", activityDotClass)}
        style={activityDotClass ? undefined : { backgroundColor: color.bg }}
      />
      {project.slug}
      {ahead && <span className="text-[9px] font-bold opacity-80">↑{gitStatus.aheadRemote}</span>}
      {behind && <span className="text-[9px] font-bold opacity-80">↓{gitStatus.behindRemote}</span>}
    </button>
  );
});
