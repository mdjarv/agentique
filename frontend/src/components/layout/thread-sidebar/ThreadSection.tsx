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

interface ArchivedBlockProps {
  count: number;
  expanded: boolean;
  onToggle: () => void;
  children: React.ReactNode;
  className?: string;
}

/** Collapsed "Archived · N" line that expands into its rows. */
export function ArchivedBlock({
  count,
  expanded,
  onToggle,
  children,
  className,
}: ArchivedBlockProps) {
  return (
    <section className={className}>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        className={cn(
          "flex w-full cursor-pointer items-center gap-1.5 px-2 py-1 text-left",
          "text-muted-foreground-dim transition-colors hover:text-muted-foreground",
        )}
      >
        {expanded ? (
          <ChevronDown className="size-3 shrink-0" />
        ) : (
          <ChevronRight className="size-3 shrink-0" />
        )}
        <span className="text-[10px] font-semibold uppercase tracking-wider">Archived</span>
        <span className="text-[10px] tabular-nums text-muted-foreground-faint">{count}</span>
      </button>
      {expanded && children}
    </section>
  );
}
