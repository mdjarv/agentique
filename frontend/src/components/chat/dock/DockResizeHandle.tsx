import { useCallback, useEffect, useRef } from "react";
import { useUIStore } from "~/stores/ui-store";

/**
 * Drag edge for the dock's width. Reads the live width from the store on each
 * drag start rather than closing over a prop, so a width changed elsewhere
 * mid-session cannot make the next drag jump.
 */
export function DockResizeHandle() {
  const setDockWidth = useUIStore((s) => s.setDockWidth);
  const dragRef = useRef<{ startX: number; startWidth: number } | null>(null);
  // Holds the teardown for an in-flight drag so it runs on unmount too: if the
  // dock closes mid-drag the listeners and the body cursor would otherwise stick.
  const cleanupRef = useRef<(() => void) | null>(null);

  const handleDragStart = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      dragRef.current = { startX: e.clientX, startWidth: useUIStore.getState().dockWidth };

      const onMove = (ev: MouseEvent) => {
        if (!dragRef.current) return;
        setDockWidth(dragRef.current.startWidth + (dragRef.current.startX - ev.clientX));
      };
      const onUp = () => {
        dragRef.current = null;
        cleanupRef.current = null;
        document.removeEventListener("mousemove", onMove);
        document.removeEventListener("mouseup", onUp);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      cleanupRef.current = onUp;
      document.addEventListener("mousemove", onMove);
      document.addEventListener("mouseup", onUp);
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
    },
    [setDockWidth],
  );

  useEffect(() => () => cleanupRef.current?.(), []);

  return (
    <div
      className="absolute top-0 bottom-0 left-0 z-10 w-1 cursor-col-resize hover:bg-primary/20 active:bg-primary/30"
      onMouseDown={handleDragStart}
    />
  );
}
