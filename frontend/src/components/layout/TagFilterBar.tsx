import { Settings2, X } from "lucide-react";
import { useState } from "react";
import { ANIMATE_DEFAULT, useAutoAnimate } from "~/hooks/useAutoAnimate";
import { getTagColor } from "~/lib/tag-colors";
import { cn } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useUIStore } from "~/stores/ui-store";
import { TagManagerDropdown } from "./TagManagerDropdown";

export function TagFilterBar() {
  const tags = useAppStore((s) => s.tags);
  const activeFilters = useUIStore((s) => s.activeTagFilters);
  const toggleFilter = useUIStore((s) => s.toggleTagFilter);
  const clearFilters = useUIStore((s) => s.clearTagFilters);
  const [managerOpen, setManagerOpen] = useState(false);

  const [animateRef] = useAutoAnimate<HTMLDivElement>(ANIMATE_DEFAULT);

  if (tags.length === 0 && !managerOpen) return null;

  const hasActiveFilters = activeFilters.length > 0;

  return (
    <div ref={animateRef} className="px-3 py-2 border-b flex flex-wrap items-center gap-1.5">
      {tags.map((tag) => {
        const color = getTagColor(tag.color);
        const isActive = activeFilters.includes(tag.id);
        return (
          <button
            key={tag.id}
            type="button"
            onClick={() => toggleFilter(tag.id)}
            className={cn(
              "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium transition-all cursor-pointer",
              isActive ? "ring-1 ring-offset-1 ring-offset-sidebar" : "opacity-50 hover:opacity-80",
            )}
            style={{
              backgroundColor: `${color.bg}20`,
              color: color.bg,
              ...(isActive ? { ringColor: color.bg } : {}),
            }}
          >
            {tag.name}
          </button>
        );
      })}
      {hasActiveFilters && (
        <button
          type="button"
          onClick={clearFilters}
          className="inline-flex items-center gap-0.5 text-xs text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
          title="Clear filters"
        >
          <X className="size-3" />
        </button>
      )}
      <TagManagerDropdown open={managerOpen} onOpenChange={setManagerOpen}>
        <button
          type="button"
          className="inline-flex items-center p-0.5 text-muted-foreground hover:text-foreground transition-colors cursor-pointer ml-auto"
          title="Manage tags"
        >
          <Settings2 className="size-3.5" />
        </button>
      </TagManagerDropdown>
    </div>
  );
}
