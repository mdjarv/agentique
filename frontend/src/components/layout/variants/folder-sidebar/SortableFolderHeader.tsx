import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Pencil, Trash2 } from "lucide-react";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "~/components/ui/context-menu";
import { cn } from "~/lib/utils";
import { FolderContent } from "./FolderHeader";
import { SidebarRow } from "./SidebarRow";
import { LEVEL } from "./types";

export function SortableFolderHeader({
  name,
  expanded,
  onToggle,
  projectCount,
  activeCount,
  hasAttention,
  onRename,
  onDelete,
  isDragActive,
}: {
  name: string;
  expanded: boolean;
  onToggle: () => void;
  projectCount: number;
  activeCount: number;
  hasAttention: boolean;
  onRename: () => void;
  onDelete: () => void;
  isDragActive: boolean;
}) {
  const { setNodeRef, attributes, listeners, transform, transition, isDragging, isOver } =
    useSortable({
      id: `folder-sort:${name}`,
      data: { type: "folder-sort", folder: name },
    });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.3 : undefined,
  };

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <div ref={setNodeRef} style={style} {...attributes}>
          <SidebarRow
            as="div"
            indent={LEVEL.folder}
            onClick={onToggle}
            className={cn(
              "group",
              isDragActive && isOver && "bg-primary/10 ring-1 ring-primary/30",
            )}
          >
            <FolderContent
              name={name}
              expanded={expanded}
              projectCount={projectCount}
              activeCount={activeCount}
              hasAttention={hasAttention}
              gripProps={listeners}
            />
          </SidebarRow>
        </div>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem onClick={onRename}>
          <Pencil className="size-3.5" />
          <span>Rename folder</span>
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem onClick={onDelete} className="text-destructive focus:text-destructive">
          <Trash2 className="size-3.5" />
          <span>Delete folder</span>
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}
