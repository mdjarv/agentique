import { Archive, Hash, Pin, PinOff } from "lucide-react";
import { memo } from "react";
import { useProjectIcon } from "~/hooks/useProjectIcon";
import { cn } from "~/lib/utils";
import { RowStateBadge } from "./RowStateBadge";
import type { MachineTone, ThreadBadge, ThreadRowVM } from "./types";

const TONE_CLASS: Record<MachineTone, string> = {
  work: "text-teal",
  attn: "text-orange",
  unread: "text-success",
  fail: "text-destructive",
  merge: "text-primary",
  draft: "text-info",
  muted: "text-muted-foreground-faint",
};

/** Spoken state for the row's aria-label. */
const BADGE_ARIA: Record<Exclude<ThreadBadge, null>, string> = {
  working: "working",
  planning: "planning",
  attention: "needs your attention",
  unread: "finished, unread",
  failed: "failed",
  merging: "merging",
  draft: "draft",
  off: "disconnected",
};

interface ThreadRowProps {
  vm: ThreadRowVM;
  selected: boolean;
  onClick: () => void;
  onTogglePin: () => void;
  onArchive: () => void;
}

function rowAriaLabel(vm: ThreadRowVM): string {
  const name = vm.untitled ? "Untitled" : vm.name;
  const state = vm.badge ? BADGE_ARIA[vm.badge] : "at rest";
  return [name, state, vm.projectSlug, vm.timeLabel].filter(Boolean).join(", ");
}

function ProjectIconGlyph({ vm }: { vm: ThreadRowVM }) {
  const Icon = useProjectIcon(vm.projectIconId ?? "");
  if (Icon) return <Icon className="size-3.5" />;
  return <>{vm.projectInitials}</>;
}

function RowActionButton({
  label,
  onAction,
  children,
}: {
  label: string;
  onAction: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={(e) => {
        e.stopPropagation();
        onAction();
      }}
      className="flex size-5 cursor-pointer items-center justify-center rounded-md text-muted-foreground hover:bg-secondary hover:text-foreground-bright"
    >
      {children}
    </button>
  );
}

/**
 * Icon-anchor session row: 26px project icon (the only identity color) with an
 * 11px corner state dot, name + relative time on line 1, mono machine line +
 * counters on line 2. Presentational only — all data arrives via the VM.
 *
 * The hover pin/archive actions are siblings of the row button (absolutely
 * positioned over the time slot) so no button ever nests inside another.
 */
export const ThreadRow = memo(function ThreadRow({
  vm,
  selected,
  onClick,
  onTogglePin,
  onArchive,
}: ThreadRowProps) {
  const PinIcon = vm.pinned ? PinOff : Pin;
  const showTodo = vm.todo && vm.todo.total > 0;

  return (
    <div className="group/thread relative">
      <button
        type="button"
        aria-label={rowAriaLabel(vm)}
        onClick={onClick}
        className={cn(
          "flex w-full cursor-pointer select-none items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left transition-colors",
          "focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring/50",
          "max-md:min-h-11",
          selected ? "bg-sidebar-accent" : "group-hover/thread:bg-sidebar-accent/60",
        )}
      >
        {/* Project icon — the only place identity color appears */}
        <span
          className={cn(
            "relative flex size-[26px] shrink-0 items-center justify-center rounded-md text-[10px] font-bold",
            vm.struck && "opacity-50",
            vm.badge === "off" && "opacity-60",
          )}
          style={{ backgroundColor: `${vm.projectColorBg}1f`, color: vm.projectColorFg }}
        >
          <ProjectIconGlyph vm={vm} />
          <RowStateBadge badge={vm.badge} selected={selected} />
        </span>

        <span className="min-w-0 flex-1">
          {/* Line 1: name + time (time yields to hover actions on desktop) */}
          <span className="flex items-baseline gap-2">
            <span
              className={cn(
                "min-w-0 flex-1 truncate text-[13px] font-medium text-foreground",
                selected && "text-foreground-bright",
                vm.unread && "font-semibold text-foreground-bright",
                vm.untitled && "font-normal italic text-muted-foreground",
                vm.struck &&
                  "font-normal text-muted-foreground-faint line-through decoration-muted-foreground/50",
              )}
            >
              {vm.untitled ? "Untitled" : vm.name}
            </span>
            <span
              className={cn(
                "shrink-0 font-mono text-[10.5px] tabular-nums text-muted-foreground-faint",
                "md:group-hover/thread:opacity-0",
              )}
            >
              {vm.timeLabel}
            </span>
          </span>

          {/* Line 2: machine line + counters */}
          <span className="mt-px flex items-center gap-2">
            <span
              className={cn(
                "min-w-0 flex-1 truncate font-mono text-[10.5px] leading-[1.4]",
                TONE_CLASS[vm.machineLine.tone],
              )}
            >
              {vm.machineLine.text || " "}
            </span>
            {showTodo && vm.todo && (
              <span className="shrink-0 font-mono text-[10px] font-medium tabular-nums text-muted-foreground">
                {vm.todo.done}/{vm.todo.total}
              </span>
            )}
            {!!vm.workers && (
              <span
                className="inline-flex shrink-0 items-center gap-0.5 font-mono text-[10px] font-medium tabular-nums text-agent"
                title={`Lead of ${vm.workers} worker${vm.workers !== 1 ? "s" : ""}`}
              >
                <Hash className="size-2.5" />
                {vm.workers}
              </span>
            )}
          </span>
        </span>
      </button>

      {/* Desktop-only hover actions, swapped into the time slot */}
      <span className="absolute right-2 top-1.5 hidden gap-0.5 md:group-hover/thread:flex">
        <RowActionButton label={vm.pinned ? "Unpin" : "Pin"} onAction={onTogglePin}>
          <PinIcon className="size-3" />
        </RowActionButton>
        <RowActionButton label="Archive" onAction={onArchive}>
          <Archive className="size-3" />
        </RowActionButton>
      </span>
    </div>
  );
});
