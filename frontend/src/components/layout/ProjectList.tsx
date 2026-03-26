import {
  DndContext,
  type DragEndEvent,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useParams, useRouterState } from "@tanstack/react-router";
import { useCallback, useEffect, useState } from "react";
import { useWebSocket } from "~/hooks/useWebSocket";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";
import { ProjectTreeItem } from "./ProjectTreeItem";

function SortableProjectItem({
  id,
  children,
}: {
  id: string;
  children: (dragListeners: React.HTMLAttributes<HTMLElement>) => React.ReactNode;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id,
  });

  return (
    <div
      ref={setNodeRef}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.5 : 1,
      }}
      {...attributes}
    >
      {children(listeners ?? {})}
    </div>
  );
}

export function ProjectList() {
  const projects = useAppStore((s) => s.projects);
  const reorderProjects = useAppStore((s) => s.reorderProjects);
  const ws = useWebSocket();
  const params = useParams({ strict: false }) as {
    projectSlug?: string;
    sessionShortId?: string;
  };
  const activeProjectSlug = params.projectSlug;
  const activeSessionId = useChatStore((s) => {
    const shortId = params.sessionShortId;
    if (!shortId) return undefined;
    return Object.keys(s.sessions).find((id) => id.startsWith(shortId));
  });
  const isNewChatRoute = useRouterState({
    select: (s) => s.location.pathname.endsWith("/session/new"),
  });

  const [expandedIds, setExpandedIds] = useState<Set<string>>(
    () => new Set(projects.map((p) => p.id)),
  );

  useEffect(() => {
    setExpandedIds((prev) => {
      const next = new Set(prev);
      let changed = false;
      for (const p of projects) {
        if (!next.has(p.id)) {
          next.add(p.id);
          changed = true;
        }
      }
      return changed ? next : prev;
    });
  }, [projects]);

  const toggleExpand = useCallback((id: string) => {
    setExpandedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 8 },
    }),
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      if (!over || active.id === over.id) return;

      const oldIndex = projects.findIndex((p) => p.id === active.id);
      const newIndex = projects.findIndex((p) => p.id === over.id);
      if (oldIndex === -1 || newIndex === -1) return;

      const reordered = arrayMove(projects, oldIndex, newIndex);
      const orderedIds = reordered.map((p) => p.id);
      reorderProjects(orderedIds);
      ws.request("project.reorder", { projectIds: orderedIds }).catch(console.error);
    },
    [projects, reorderProjects, ws],
  );

  if (projects.length === 0) {
    return <div className="p-4 text-sm text-sidebar-foreground/70">No projects yet</div>;
  }

  return (
    <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
      <SortableContext items={projects.map((p) => p.id)} strategy={verticalListSortingStrategy}>
        <div className="py-1">
          {projects.map((project) => (
            <SortableProjectItem key={project.id} id={project.id}>
              {(dragListeners) => (
                <ProjectTreeItem
                  project={project}
                  isActive={project.slug === activeProjectSlug}
                  isExpanded={expandedIds.has(project.id)}
                  onToggleExpand={() => toggleExpand(project.id)}
                  activeSessionId={activeSessionId}
                  isNewChatActive={project.slug === activeProjectSlug && isNewChatRoute}
                  dragListeners={dragListeners}
                />
              )}
            </SortableProjectItem>
          ))}
        </div>
      </SortableContext>
    </DndContext>
  );
}
