/**
 * Pinned-section drag-and-drop — dnd-kit vertical list reorder.
 *
 * `usePinDnd` supplies the sensors + drag-end handler for the orchestrator's
 * `DndContext`/`SortableContext`; `usePinSortable` is the per-row hook
 * (DraggableProject pattern) whose ref/style/listeners the row wrapper spreads.
 */
import {
  type DragEndEvent,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useCallback } from "react";

export interface PinDnd {
  sensors: ReturnType<typeof useSensors>;
  /** Pass to the pinned `SortableContext`. */
  strategy: typeof verticalListSortingStrategy;
  handleDragEnd: (event: DragEndEvent) => void;
}

export function usePinDnd(pinnedIds: string[], onReorder: (ids: string[]) => void): PinDnd {
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { delay: 200, tolerance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      if (!over || active.id === over.id) return;
      const from = pinnedIds.indexOf(active.id as string);
      const to = pinnedIds.indexOf(over.id as string);
      if (from < 0 || to < 0) return;
      onReorder(arrayMove(pinnedIds, from, to));
    },
    [pinnedIds, onReorder],
  );

  return { sensors, strategy: verticalListSortingStrategy, handleDragEnd };
}

type SortableHandle = ReturnType<typeof useSortable>;

export interface PinSortable {
  setNodeRef: SortableHandle["setNodeRef"];
  style: React.CSSProperties;
  attributes: SortableHandle["attributes"];
  listeners: SortableHandle["listeners"];
  isDragging: boolean;
}

/** Per-row sortable handle for a pinned ThreadRow's wrapper element. */
export function usePinSortable(sessionId: string): PinSortable {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: sessionId,
  });

  return {
    setNodeRef,
    style: {
      transform: CSS.Transform.toString(transform),
      transition,
      opacity: isDragging ? 0.4 : undefined,
    },
    attributes,
    listeners,
    isDragging,
  };
}
