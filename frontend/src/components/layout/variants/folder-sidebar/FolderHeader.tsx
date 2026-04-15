import { ChevronDown, ChevronRight, FolderOpen, GripVertical } from "lucide-react";
import { memo } from "react";
import { IconSlot } from "./IconSlot";

/** Folder row content — icon (swaps to grip on hover), name, counts, chevron. */
export const FolderContent = memo(function FolderContent({
  name,
  expanded,
  projectCount,
  activeCount,
  hasAttention,
  gripProps,
}: {
  name: string;
  expanded: boolean;
  projectCount: number;
  activeCount: number;
  hasAttention: boolean;
  gripProps?: Record<string, unknown>;
}) {
  return (
    <>
      <IconSlot>
        <FolderOpen className="size-3.5 text-muted-foreground group-hover:hidden" />
        <span
          className="hidden group-hover:flex items-center justify-center cursor-grab"
          {...gripProps}
        >
          <GripVertical className="size-3 text-muted-foreground/40 hover:!text-muted-foreground" />
        </span>
      </IconSlot>
      <span className="text-[11px] font-bold text-muted-foreground uppercase tracking-wider flex-1 truncate ml-1">
        {name}
      </span>
      <span className="flex items-center gap-1">
        {!expanded && hasAttention && (
          <span className="size-1.5 rounded-full bg-orange animate-pulse" />
        )}
        <span className="text-[10px] text-muted-foreground-faint tabular-nums">
          {projectCount}
          {activeCount > 0 && <span className="text-teal ml-0.5">·{activeCount}</span>}
        </span>
        {expanded ? (
          <ChevronDown className="size-2.5 text-muted-foreground" />
        ) : (
          <ChevronRight className="size-2.5 text-muted-foreground" />
        )}
      </span>
    </>
  );
});
