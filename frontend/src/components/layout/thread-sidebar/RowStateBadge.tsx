import { memo } from "react";
import { cn } from "~/lib/utils";
import type { ThreadBadge } from "./types";

/**
 * Dot styling per badge. Hollow states (draft/off) draw an inset ring instead
 * of a fill; the amber pulse is rendered as a separate overlay so it stays the
 * only pulsing signal in the sidebar (planning breathes via opacity only).
 */
const DOT_CLASS: Record<Exclude<ThreadBadge, null>, string> = {
  working: "bg-teal",
  planning: "bg-teal animate-pulse motion-reduce:animate-none",
  attention: "bg-orange",
  unread: "bg-success",
  failed: "bg-destructive",
  merging: "bg-primary",
  draft: "bg-transparent shadow-[inset_0_0_0_1.5px_var(--info)]",
  off: "bg-transparent shadow-[inset_0_0_0_1.5px_var(--muted-foreground-faint)]",
};

interface RowStateBadgeProps {
  badge: ThreadBadge;
  /** Selected rows sit on the raised surface, so the surface ring must match. */
  selected?: boolean;
}

/** 11px corner state dot on the project icon, 2px-ringed with the row surface. */
export const RowStateBadge = memo(function RowStateBadge({ badge, selected }: RowStateBadgeProps) {
  if (!badge) return null;

  return (
    <span aria-hidden="true" className="absolute -right-[3px] -bottom-[3px] size-[11px]">
      {badge === "attention" && (
        <span className="absolute inset-0 rounded-full ring-2 ring-orange/40 animate-pulse motion-reduce:hidden" />
      )}
      <span
        className={cn(
          "absolute inset-0 rounded-full border-2",
          selected ? "border-sidebar-accent" : "border-sidebar",
          DOT_CLASS[badge],
        )}
      />
    </span>
  );
});
