import { useDroppable } from "@dnd-kit/core";
import { cn } from "~/lib/utils";

/** Visual drop target shown during drag. */
export function DropZone({
  id,
  label,
  className,
}: {
  id: string;
  label: string;
  className?: string;
}) {
  const { setNodeRef, isOver } = useDroppable({ id });

  return (
    <div
      ref={setNodeRef}
      className={cn(
        "mx-1 my-0.5 rounded-md border-2 border-dashed px-3 py-2 text-center text-[10px] font-medium transition-colors",
        isOver
          ? "border-primary/50 bg-primary/10 text-primary"
          : "border-muted-foreground/20 text-muted-foreground/40",
        className,
      )}
    >
      {label}
    </div>
  );
}
