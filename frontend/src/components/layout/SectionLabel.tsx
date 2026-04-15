import { ChevronDown, ChevronRight } from "lucide-react";
import { cn } from "~/lib/utils";

interface SectionLabelProps {
  label: string;
  count: number;
  unreadCount?: number;
  icon?: React.ReactNode;
  collapsible?: boolean;
  expanded?: boolean;
  onToggle?: () => void;
  actions?: React.ReactNode;
  className?: string;
  /** When set, tints the label text with this color. */
  accentColor?: string;
}

export function SectionLabel({
  label,
  count,
  unreadCount,
  icon,
  collapsible,
  expanded,
  onToggle,
  actions,
  className,
  accentColor,
}: SectionLabelProps) {
  const content = (
    <>
      {collapsible &&
        (expanded ? (
          <ChevronDown className="size-3 shrink-0 text-muted-foreground transition-transform" />
        ) : (
          <ChevronRight className="size-3 shrink-0 text-muted-foreground transition-transform" />
        ))}
      {icon}
      <span className="text-[10px] text-muted-foreground-faint tabular-nums">{count}</span>
      <span
        className="text-[10px] font-semibold tracking-wider uppercase"
        style={accentColor ? { color: accentColor } : undefined}
      >
        {label}
      </span>
      {!!unreadCount && (
        <span className="text-[10px] font-semibold text-primary ml-auto tabular-nums">
          {unreadCount} unread
        </span>
      )}
    </>
  );

  if (collapsible) {
    return (
      <div
        className={cn(
          "group/section flex w-full items-center",
          "text-muted-foreground-dim hover:text-muted-foreground transition-colors",
          className,
        )}
      >
        <button
          type="button"
          onClick={onToggle}
          className="flex flex-1 items-center gap-1.5 px-2 py-1 text-left cursor-pointer"
        >
          {content}
        </button>
        {actions && (
          <div className="opacity-0 group-hover/section:opacity-100 transition-opacity pr-1">
            {actions}
          </div>
        )}
      </div>
    );
  }

  return (
    <div className={cn("flex items-center gap-1.5 px-2 py-1 text-muted-foreground-dim", className)}>
      {content}
      {actions && <div className="ml-auto">{actions}</div>}
    </div>
  );
}
