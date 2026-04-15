import { useDraggable } from "@dnd-kit/core";
import { Link } from "@tanstack/react-router";
import { FolderMinus, Plus, Settings } from "lucide-react";
import { memo } from "react";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "~/components/ui/context-menu";
import { type ProjectGitStatus, useAppStore } from "~/stores/app-store";
import { ChannelSessions } from "./ChannelSessions";
import { ProjectGitLine } from "./ProjectGitLine";
import { ProjectContent } from "./ProjectRow";
import { SidebarRow } from "./SidebarRow";
import type { ProjectEntry } from "./types";

const EMPTY_GIT: ProjectGitStatus | undefined = undefined;

function useProjectGit(projectId: string): ProjectGitStatus | undefined {
  return useAppStore((s) => s.projectGitStatus[projectId] ?? EMPTY_GIT);
}

export const DraggableProject = memo(function DraggableProject({
  entry,
  expanded,
  onToggle,
  onExpand,
  onSessionClick,
  onMoveToUngrouped,
  level,
  compact,
}: {
  entry: ProjectEntry;
  expanded: boolean;
  onToggle: () => void;
  onExpand: () => void;
  onSessionClick: (id: string) => void;
  onMoveToUngrouped?: () => void;
  level: number;
  compact?: boolean;
}) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: entry.project.id,
    data: { type: "project", folder: entry.project.folder },
  });

  const gitStatus = useProjectGit(entry.project.id);
  const sessionLevel = level + 2;
  const workerLevel = level + 3;

  const tintStyle = {
    backgroundColor: `${entry.color.fg}15`,
    opacity: isDragging ? 0.3 : undefined,
  };

  const content = (
    <div ref={setNodeRef} style={tintStyle} {...attributes} {...listeners} className="-mx-2 px-2">
      <SidebarRow as="div" indent={level} plain className="group/proj relative">
        <ProjectContent
          slug={entry.project.slug}
          name={entry.project.name}
          icon={entry.project.icon}
          color={entry.color}
          expanded={expanded}
          onToggle={onToggle}
          onExpand={onExpand}
          worstState={entry.worstState}
        />
      </SidebarRow>

      {expanded && !compact && (
        <ProjectGitLine projectId={entry.project.id} gitStatus={gitStatus} />
      )}

      {expanded && (
        <ChannelSessions
          sessions={entry.active}
          completed={compact ? [] : entry.completed}
          onSessionClick={onSessionClick}
          projectSlug={entry.project.slug}
          sessionLevel={sessionLevel}
          workerLevel={workerLevel}
        />
      )}
    </div>
  );

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{content}</ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem asChild>
          <Link to="/project/$projectSlug/settings" params={{ projectSlug: entry.project.slug }}>
            <Settings className="size-3.5" />
            <span>Project settings</span>
          </Link>
        </ContextMenuItem>
        <ContextMenuItem asChild>
          <Link to="/project/$projectSlug/session/new" params={{ projectSlug: entry.project.slug }}>
            <Plus className="size-3.5" />
            <span>New session</span>
          </Link>
        </ContextMenuItem>
        {onMoveToUngrouped && (
          <>
            <ContextMenuSeparator />
            <ContextMenuItem onClick={onMoveToUngrouped}>
              <FolderMinus className="size-3.5" />
              <span>Move to ungrouped</span>
            </ContextMenuItem>
          </>
        )}
      </ContextMenuContent>
    </ContextMenu>
  );
});

/** Non-interactive drag preview. */
export function DragOverlayProject({ entry }: { entry: ProjectEntry }) {
  return (
    <SidebarRow
      as="div"
      indent={0}
      plain
      className="shadow-lg shadow-black/20 rounded-md bg-sidebar"
    >
      <ProjectContent
        slug={entry.project.slug}
        name={entry.project.name}
        icon={entry.project.icon}
        color={entry.color}
        expanded={false}
        onToggle={() => {}}
        onExpand={() => {}}
        worstState={entry.worstState}
      />
    </SidebarRow>
  );
}
