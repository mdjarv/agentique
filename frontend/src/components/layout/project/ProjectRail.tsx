import {
  closestCenter,
  DndContext,
  type DragEndEvent,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  arrayMove,
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Plus } from "lucide-react";
import { Fragment, memo, useCallback, useMemo, useState } from "react";
import { useShallow } from "zustand/shallow";
import { Tooltip, TooltipContent, TooltipTrigger } from "~/components/ui/tooltip";
import { useProjectActivity } from "~/hooks/useProjectActivity";
import { useProjectIcon } from "~/hooks/useProjectIcon";
import { useTheme } from "~/hooks/useTheme";
import { useWebSocket } from "~/hooks/useWebSocket";
import { reorderProjects, setProjectFavorite } from "~/lib/project-actions";
import { getProjectColor } from "~/lib/project-colors";
import type { Project } from "~/lib/types";
import { cn } from "~/lib/utils";
import { type ProjectGitStatus, useAppStore } from "~/stores/app-store";
import { type BadgeState, SessionBadge } from "../session/SessionBadge";
import { NewProjectDialog } from "./NewProjectDialog";

interface ProjectRailProps {
  selectedProjectId: string | null;
  onSelectProject: (projectId: string | null) => void;
}

const EMPTY_GIT: ProjectGitStatus | undefined = undefined;

function useProjectGit(projectId: string): ProjectGitStatus | undefined {
  return useAppStore((s) => s.projectGitStatus[projectId] ?? EMPTY_GIT);
}

function sortWithinGroup(projects: Project[]): Project[] {
  return [...projects].sort((a, b) => {
    const aOrder = a.sort_order || Number.MAX_SAFE_INTEGER;
    const bOrder = b.sort_order || Number.MAX_SAFE_INTEGER;
    if (aOrder !== bOrder) return aOrder - bOrder;
    return a.name.localeCompare(b.name);
  });
}

/** Vertical project rail for desktop sidebar. */
export const ProjectRail = memo(function ProjectRail({
  selectedProjectId,
  onSelectProject,
}: ProjectRailProps) {
  const projects = useAppStore((s) => s.projects);
  const projectIds = useAppStore(useShallow((s) => s.projects.map((p) => p.id)));
  const ws = useWebSocket();

  const { allSorted, allIds, favBoundary } = useMemo(() => {
    const favs: Project[] = [];
    const rest: Project[] = [];
    for (const p of projects) {
      if (p.favorite) favs.push(p);
      else rest.push(p);
    }
    const sorted = [...sortWithinGroup(favs), ...sortWithinGroup(rest)];
    return {
      allSorted: sorted,
      allIds: sorted.map((p) => p.id),
      favBoundary: favs.length,
    };
  }, [projects]);

  const hasSeparator = favBoundary > 0 && favBoundary < allSorted.length;

  const [activeId, setActiveId] = useState<string | null>(null);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor),
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      setActiveId(null);
      const { active, over } = event;
      if (!over) return;

      const activeStr = active.id as string;
      const overStr = over.id as string;
      if (activeStr === overStr) return;

      const oldIndex = allIds.indexOf(activeStr);
      const newIndex = allIds.indexOf(overStr);
      if (oldIndex < 0 || newIndex < 0) return;

      const newOrder = arrayMove(allIds, oldIndex, newIndex);

      const wasFav = oldIndex < favBoundary;
      const nowAboveBoundary = newIndex < favBoundary;
      const crossedBoundary = hasSeparator && wasFav !== nowAboveBoundary;

      const updated = projects.map((p) => {
        const order = newOrder.indexOf(p.id) + 1;
        const fav = crossedBoundary && p.id === activeStr ? (nowAboveBoundary ? 1 : 0) : p.favorite;
        return { ...p, sort_order: order, favorite: fav };
      });
      useAppStore.getState().setProjects(updated);

      if (crossedBoundary) {
        setProjectFavorite(ws, activeStr, nowAboveBoundary).catch(console.error);
      }
      reorderProjects(ws, newOrder).catch(console.error);
    },
    [allIds, favBoundary, hasSeparator, projects, ws],
  );

  const activeProject = activeId ? projects.find((p) => p.id === activeId) : null;

  return (
    <div className="flex flex-col items-center gap-1 py-2 px-1.5 border-r border-border/50 bg-sidebar/50">
      {/* "All" button */}
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            onClick={() => onSelectProject(null)}
            className={cn(
              "size-9 rounded-[10px] flex items-center justify-center text-[10px] font-mono font-semibold transition-all",
              selectedProjectId === null
                ? "bg-primary/15 text-primary ring-2 ring-primary/50"
                : "bg-muted/50 text-muted-foreground hover:bg-muted",
            )}
          >
            All
          </button>
        </TooltipTrigger>
        <TooltipContent side="right">All projects</TooltipContent>
      </Tooltip>

      <div className="w-6 h-px bg-border my-1" />

      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragStart={(e) => setActiveId(e.active.id as string)}
        onDragCancel={() => setActiveId(null)}
        onDragEnd={handleDragEnd}
      >
        <SortableContext items={allIds} strategy={verticalListSortingStrategy}>
          {allSorted.map((project, i) => (
            <Fragment key={project.id}>
              {i === favBoundary && hasSeparator && <div className="w-6 h-px bg-border my-1" />}
              <SortableRailDot
                project={project}
                projectIds={projectIds}
                isSelected={selectedProjectId === project.id}
                onSelect={onSelectProject}
              />
            </Fragment>
          ))}
        </SortableContext>

        <DragOverlay>
          {activeProject ? (
            <RailDot
              project={activeProject}
              projectIds={projectIds}
              isSelected={false}
              onSelect={() => {}}
              isOverlay
            />
          ) : null}
        </DragOverlay>
      </DndContext>

      <div className="w-6 h-px bg-border my-1" />
      <Tooltip>
        <NewProjectDialog
          trigger={
            <TooltipTrigger asChild>
              <button
                type="button"
                className="size-9 rounded-[10px] flex items-center justify-center text-muted-foreground hover:text-foreground hover:bg-muted/50 border border-dashed border-muted-foreground/25 bg-muted/30 transition-all"
              >
                <Plus className="size-4" />
              </button>
            </TooltipTrigger>
          }
        />
        <TooltipContent side="right">New project</TooltipContent>
      </Tooltip>
    </div>
  );
});

/** Sortable wrapper around RailDot. */
const SortableRailDot = memo(function SortableRailDot(props: {
  project: Project;
  projectIds: string[];
  isSelected: boolean;
  onSelect: (id: string | null) => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: props.project.id,
  });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.3 : undefined,
  };

  return (
    <div ref={setNodeRef} style={style} {...attributes} {...listeners}>
      <RailDot {...props} />
    </div>
  );
});

/** Individual project dot in the rail. */
const RailDot = memo(function RailDot({
  project,
  projectIds,
  isSelected,
  onSelect,
  isOverlay,
}: {
  project: Project;
  projectIds: string[];
  isSelected: boolean;
  onSelect: (id: string | null) => void;
  isOverlay?: boolean;
}) {
  const gitStatus = useProjectGit(project.id);
  const activity = useProjectActivity(project.id);
  const { resolvedTheme } = useTheme();
  const color = useMemo(
    () => getProjectColor(project.color, project.id, projectIds, resolvedTheme),
    [project.color, project.id, projectIds, resolvedTheme],
  );

  const handleClick = useCallback(() => onSelect(project.id), [onSelect, project.id]);

  const Icon = useProjectIcon(project.icon);
  const initials = project.slug
    .split("-")
    .map((w) => w[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);

  const ahead = gitStatus && gitStatus.aheadRemote > 0;
  const behind = gitStatus && gitStatus.behindRemote > 0;
  const hasGitAttention = ahead || behind;

  // Activity badge: priority cascade — attention > running > failed > unseen > git
  const activityBadge = useMemo((): { state: BadgeState; label: string } | null => {
    if (activity.attentionCount > 0)
      return { state: "approval", label: `${activity.attentionCount} need attention` };
    if (activity.runningCount > 0)
      return { state: "running", label: `${activity.runningCount} running` };
    if (activity.failedCount > 0)
      return { state: "failed", label: `${activity.failedCount} failed` };
    if (activity.unseenCount > 0)
      return { state: "unseen", label: `${activity.unseenCount} completed` };
    return null;
  }, [activity]);

  // Tooltip with activity summary
  const tooltipParts: string[] = [project.name];
  if (gitStatus?.branch) tooltipParts.push(gitStatus.branch);
  if (activityBadge) tooltipParts.push(activityBadge.label);
  const tooltipLabel = tooltipParts.join(" · ");

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="relative flex items-center">
          {isSelected && (
            <span
              className="absolute -left-1.5 h-5 w-[3px] rounded-full"
              style={{ backgroundColor: color.fg }}
            />
          )}
          <button
            type="button"
            onClick={handleClick}
            className={cn(
              "size-9 rounded-[10px] flex items-center justify-center text-xs font-bold relative transition-all",
              "hover:rounded-[14px]",
              isSelected ? "opacity-100" : "opacity-70 hover:opacity-100",
              isOverlay && "shadow-lg shadow-black/30 scale-110",
            )}
            style={{
              backgroundColor: `${color.bg}25`,
              color: color.fg,
              boxShadow: isOverlay
                ? undefined
                : isSelected
                  ? `inset 0 1px 0 0 ${color.bg}18, 0 1px 3px 0 rgba(0,0,0,0.08)`
                  : `inset 0 1px 0 0 ${color.bg}12, 0 1px 2px 0 rgba(0,0,0,0.04)`,
            }}
          >
            {Icon ? <Icon className="size-4" /> : initials}
            {activityBadge ? (
              <SessionBadge
                state={activityBadge.state}
                size="md"
                title={activityBadge.label}
                accentColor={color.fg}
                className="absolute -top-1.5 -right-1.5 z-10"
              />
            ) : (
              hasGitAttention && (
                <span
                  className={cn(
                    "absolute -top-1 -right-1 min-w-[14px] h-[14px] rounded-full text-[8px] font-bold flex items-center justify-center px-0.5",
                    behind ? "bg-orange/20 text-orange" : "bg-success/20 text-success",
                  )}
                >
                  {ahead && `↑${gitStatus.aheadRemote}`}
                  {behind && `↓${gitStatus.behindRemote}`}
                </span>
              )
            )}
          </button>
        </div>
      </TooltipTrigger>
      <TooltipContent side="right">{tooltipLabel}</TooltipContent>
    </Tooltip>
  );
});
