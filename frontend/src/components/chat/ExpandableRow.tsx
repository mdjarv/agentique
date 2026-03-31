import { ChevronDown, ChevronRight } from "lucide-react";
import { cn } from "~/lib/utils";

interface ExpandableRowProps {
  expanded: boolean;
  onToggle: () => void;
  expandable?: boolean;
  trailing?: React.ReactNode;
  trailingClassName?: string;
  childrenClassName?: string;
  className?: string;
  children: React.ReactNode;
}

export function ExpandableRow({
  expanded,
  onToggle,
  expandable = true,
  trailing,
  trailingClassName,
  childrenClassName,
  className,
  children,
}: ExpandableRowProps) {
  return (
    <button
      type="button"
      onClick={() => expandable && onToggle()}
      className={cn(
        "flex items-center gap-2 px-2 py-1.5 text-xs text-muted-foreground w-full text-left min-w-0 transition-colors",
        expandable && "hover:bg-muted/80 cursor-pointer",
        className,
      )}
    >
      {childrenClassName ? (
        <span className={cn("flex items-center gap-2 min-w-0", childrenClassName)}>{children}</span>
      ) : (
        children
      )}
      {trailing ? (
        <span
          className={cn(
            "ml-auto flex items-center gap-1.5 min-w-0 overflow-hidden",
            trailingClassName,
          )}
        >
          {trailing}
        </span>
      ) : null}
      <span className={cn("shrink-0 pl-0.5", !expandable && "opacity-30", !trailing && "ml-auto")}>
        {expanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
      </span>
    </button>
  );
}
