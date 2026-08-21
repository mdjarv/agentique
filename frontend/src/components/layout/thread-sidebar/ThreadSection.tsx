import { ChevronDown, ChevronRight } from "lucide-react";
import { cn } from "~/lib/utils";

interface ThreadSectionProps {
  label: string;
  count?: number;
  children: React.ReactNode;
  className?: string;
}

/** Static section label (Pinned / Open) wrapping its rows — SectionLabel voice. */
export function ThreadSection({ label, count, children, className }: ThreadSectionProps) {
  return (
    <section className={className}>
      <div className="flex items-center gap-1.5 px-2 py-1 text-muted-foreground-dim">
        {count !== undefined && (
          <span className="text-[10px] tabular-nums text-muted-foreground-faint">{count}</span>
        )}
        <span className="text-[10px] font-semibold uppercase tracking-wider">{label}</span>
      </div>
      {children}
    </section>
  );
}

interface CollapsibleBlockProps {
  label: string;
  count: number;
  expanded: boolean;
  onToggle: () => void;
  /** Optional right-aligned control on the header line (e.g. Archive all). */
  action?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}

/** Collapsed "<label> · N" line that expands into its rows — the stale shelf
 *  and the Archived tail share this shape. */
export function CollapsibleBlock({
  label,
  count,
  expanded,
  onToggle,
  action,
  children,
  className,
}: CollapsibleBlockProps) {
  return (
    <section className={className}>
      <div className="flex items-center gap-1.5 px-2 py-1 text-muted-foreground-dim">
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={expanded}
          className={cn(
            "flex min-w-0 flex-1 cursor-pointer items-center gap-1.5 text-left",
            "transition-colors hover:text-muted-foreground",
          )}
        >
          {expanded ? (
            <ChevronDown className="size-3 shrink-0" />
          ) : (
            <ChevronRight className="size-3 shrink-0" />
          )}
          <span className="text-[10px] font-semibold uppercase tracking-wider">{label}</span>
          <span className="text-[10px] tabular-nums text-muted-foreground-faint">{count}</span>
        </button>
        {action}
      </div>
      {expanded && children}
    </section>
  );
}
