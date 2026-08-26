import { ChevronDown, ChevronRight } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "~/lib/utils";

interface DockSectionProps {
  icon: ReactNode;
  title: string;
  /**
   * The section's own badge, shown whether it is open or shut — a collapsed
   * header still has to report, or folding a section away hides the state that
   * made you want it.
   */
  mark?: ReactNode;
  open: boolean;
  onToggle: () => void;
  children: ReactNode;
  /** Let this section take the leftover height (the roster does; todos don't). */
  grow?: boolean;
}

/**
 * One stacked section inside a grouped dock view. Sections rather than nested
 * tabs because the things inside `Work` are true at the same time: the plan and
 * who is out working it belong on screen together, which is the entire reason
 * the group exists.
 */
export function DockSection({
  icon,
  title,
  mark,
  open,
  onToggle,
  children,
  grow = false,
}: DockSectionProps) {
  return (
    <section className={cn("flex min-h-0 flex-col", grow && open && "flex-1")}>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className="flex shrink-0 cursor-pointer items-center gap-1.5 px-3 py-1.5 text-left text-muted-foreground transition-colors hover:bg-muted/30 hover:text-foreground"
      >
        {open ? (
          <ChevronDown className="size-3 shrink-0 text-muted-foreground-faint" />
        ) : (
          <ChevronRight className="size-3 shrink-0 text-muted-foreground-faint" />
        )}
        <span className="shrink-0">{icon}</span>
        <span className="font-medium text-[10px] uppercase tracking-[0.12em]">{title}</span>
        {mark && <span className="ml-auto shrink-0">{mark}</span>}
      </button>
      {open && <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">{children}</div>}
    </section>
  );
}
