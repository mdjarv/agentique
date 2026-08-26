import { Bot, Clock, FileDiff, Globe, TriangleAlert, X } from "lucide-react";
import { useEffect, useRef } from "react";
import { DOCK_LABELS, type DockView } from "~/lib/session/dock";
import { cn } from "~/lib/utils";

/**
 * What one tab is currently asking for. The glyph vocabulary is the sidebar's
 * (`ThreadRow`) and the old tab strip's, unchanged: **X means it failed, the
 * triangle means someone is waiting on you**, and a pulsing dot means live
 * activity. One mark, one meaning, on every surface.
 */
export type DockTabMark =
  | { kind: "live"; count: number }
  | { kind: "failed"; count: number }
  | { kind: "blocked"; count: number }
  | { kind: "count"; label: string }
  | null;

const ICONS: Record<DockView, typeof Bot> = {
  work: Bot,
  changes: FileDiff,
  loops: Clock,
  browser: Globe,
};

function Mark({ mark }: { mark: NonNullable<DockTabMark> }) {
  if (mark.kind === "count") {
    return <span className="text-[10px] text-muted-foreground tabular-nums">{mark.label}</span>;
  }
  if (mark.kind === "live") {
    return (
      <span className="flex items-center gap-1">
        <span className="size-1.5 rounded-full bg-agent motion-safe:animate-pulse" />
        <span className="font-medium text-[10px] text-agent tabular-nums">{mark.count}</span>
      </span>
    );
  }
  if (mark.kind === "blocked") {
    return (
      <span className="flex items-center gap-0.5 text-warning">
        <TriangleAlert className="size-3" />
        <span className="font-medium text-[10px] tabular-nums">{mark.count}</span>
      </span>
    );
  }
  return (
    <span className="flex items-center gap-0.5 text-destructive">
      <X className="size-3" />
      <span className="font-medium text-[10px] tabular-nums">{mark.count}</span>
    </span>
  );
}

interface DockTabBarProps {
  views: readonly DockView[];
  active: DockView;
  marks: Partial<Record<DockView, DockTabMark>>;
  onSelect: (view: DockView) => void;
  /** Project accent hex — the active tab wears the session's hue, as tabs did. */
  accentColor?: string;
}

/**
 * The dock's tab row. Deliberately narrow — icon plus label, and the row
 * scrolls rather than wrapping or truncating the set — because the dock is
 * 300px at its narrowest and a row that wraps steals height from the content
 * it is navigating.
 */
export function DockTabBar({ views, active, marks, onSelect, accentColor }: DockTabBarProps) {
  const listRef = useRef<HTMLDivElement>(null);
  const color = accentColor || "var(--primary)";

  // Keep the active tab reachable when the row overflows: selecting a tab that
  // is scrolled out (from a deep link, or a fallback after its view vanished)
  // otherwise leaves the row looking like nothing happened.
  useEffect(() => {
    const el = listRef.current?.querySelector<HTMLElement>("[data-active-tab='true']");
    el?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }, []);

  return (
    <div
      ref={listRef}
      className="scrollbar-none flex shrink-0 items-center gap-1 overflow-x-auto border-b px-1.5 py-1"
    >
      {views.map((view) => {
        const Icon = ICONS[view];
        const isActive = view === active;
        const mark = marks[view] ?? null;
        return (
          <button
            key={view}
            type="button"
            data-active-tab={isActive}
            onClick={() => onSelect(view)}
            className={cn(
              "flex shrink-0 cursor-pointer items-center gap-1.5 rounded-md px-2 py-1 text-xs transition-colors",
              isActive
                ? "font-medium"
                : "text-muted-foreground hover:bg-muted/40 hover:text-foreground",
            )}
            style={
              isActive
                ? {
                    backgroundColor: `${color}1f`,
                    color: `color-mix(in srgb, ${color}, var(--foreground) 40%)`,
                  }
                : undefined
            }
          >
            <Icon className="size-3.5 shrink-0" />
            {DOCK_LABELS[view]}
            {mark && <Mark mark={mark} />}
          </button>
        );
      })}
    </div>
  );
}
