import { Archive, Hash, Pin, PinOff } from "lucide-react";
import { memo } from "react";
import { useProjectIcon } from "~/hooks/useProjectIcon";
import { cn } from "~/lib/utils";
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
  off: "evicted",
};

interface ThreadRowProps {
  vm: ThreadRowVM;
  selected: boolean;
  /** One-line settled row for the shelf and Archived sections. */
  compact?: boolean;
  onClick: () => void;
  onTogglePin: () => void;
  onArchive: () => void;
}

function rowAriaLabel(vm: ThreadRowVM): string {
  const name = vm.untitled ? "Untitled" : vm.name;
  const state = vm.badge ? BADGE_ARIA[vm.badge] : vm.restToken || "at rest";
  return [name, state, vm.projectSlug, vm.timeLabel].filter(Boolean).join(", ");
}

/**
 * The 14px inline project chip on the repo line. Awake rows carry the
 * project's hue; resting rows are grey, evicted rows fainter still — the
 * wake model's identity color lives here and on the slug, nowhere else.
 */
function Chip({ vm, awake }: { vm: ThreadRowVM; awake: boolean }) {
  const Icon = useProjectIcon(vm.projectIconId ?? "");
  const evicted = vm.badge === "off";
  return (
    <span
      className={cn(
        "flex size-3.5 shrink-0 items-center justify-center rounded",
        !awake &&
          (evicted
            ? "bg-border/25 text-muted-foreground-faint"
            : "bg-border/40 text-muted-foreground"),
      )}
      style={
        awake ? { backgroundColor: `${vm.projectColorBg}26`, color: vm.projectColorFg } : undefined
      }
    >
      {Icon ? (
        <Icon className="size-2.5" />
      ) : (
        <span className="text-[7px] font-bold">{vm.projectInitials}</span>
      )}
    </span>
  );
}

function RowActions({
  pinned,
  onTogglePin,
  onArchive,
}: {
  pinned: boolean;
  onTogglePin: () => void;
  onArchive: () => void;
}) {
  const PinIcon = pinned ? PinOff : Pin;
  return (
    <span className="absolute right-2 top-1.5 hidden gap-0.5 md:group-hover/thread:flex">
      {[
        {
          label: pinned ? "Unpin" : "Pin",
          action: onTogglePin,
          icon: <PinIcon className="size-3" />,
        },
        { label: "Archive", action: onArchive, icon: <Archive className="size-3" /> },
      ].map(({ label, action, icon }) => (
        <button
          key={label}
          type="button"
          aria-label={label}
          title={label}
          onClick={(e) => {
            e.stopPropagation();
            action();
          }}
          className="flex size-5 cursor-pointer items-center justify-center rounded-md text-muted-foreground hover:bg-secondary hover:text-foreground-bright"
        >
          {icon}
        </button>
      ))}
    </span>
  );
}

/**
 * Full-wake adaptive session row (C1):
 * - resting rows: two grey lines — repo line (chip · slug · @machine · outcome
 *   · time) over the title;
 * - awake rows: identity wakes into the project hue and a third line appears —
 *   the state phrase in its tone, todo/worker counters right, and the pulsing
 *   amber dot reserved for blocked-on-you.
 * Presentational only — all data arrives via the VM.
 */
export const ThreadRow = memo(function ThreadRow({
  vm,
  selected,
  compact = false,
  onClick,
  onTogglePin,
  onArchive,
}: ThreadRowProps) {
  if (compact) {
    return (
      <div className="group/thread relative">
        <button
          type="button"
          aria-label={rowAriaLabel(vm)}
          onClick={onClick}
          className={cn(
            "flex w-full cursor-pointer select-none items-center gap-2 rounded-lg px-2.5 py-1.5 text-left transition-colors",
            "focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring/50",
            "max-md:min-h-11",
            selected ? "bg-sidebar-accent" : "group-hover/thread:bg-sidebar-accent/60",
          )}
        >
          <Chip vm={vm} awake={false} />
          <span
            className={cn(
              "min-w-0 flex-1 truncate text-[12.5px] text-muted-foreground",
              vm.struck && "line-through decoration-muted-foreground/50",
              vm.untitled && "italic",
            )}
          >
            {vm.untitled ? "Untitled" : vm.name}
          </span>
          <span className="min-w-0 shrink truncate font-mono text-[10px] text-muted-foreground-faint">
            {vm.projectSlug}
          </span>
          {vm.remoteMachineLabel && (
            <span className="shrink-0 font-mono text-[10px] text-muted-foreground-faint">
              @{vm.remoteMachineLabel}
            </span>
          )}
          <span className="shrink-0 font-mono text-[10.5px] tabular-nums text-muted-foreground-faint md:group-hover/thread:opacity-0">
            {vm.timeLabel}
          </span>
        </button>
        <RowActions pinned={vm.pinned} onTogglePin={onTogglePin} onArchive={onArchive} />
      </div>
    );
  }

  const awake = vm.awake;
  const evicted = vm.badge === "off";
  const showTodo = vm.todo && vm.todo.total > 0;

  return (
    <div className={cn("group/thread relative", selected && "rounded-lg bg-sidebar-accent")}>
      <button
        type="button"
        aria-label={rowAriaLabel(vm)}
        onClick={onClick}
        className={cn(
          "block w-full cursor-pointer select-none rounded-lg px-2.5 py-1.5 text-left transition-colors",
          "focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring/50",
          "max-md:min-h-11",
          !selected && "group-hover/thread:bg-sidebar-accent/60",
        )}
      >
        {/* Repo line: chip · slug · @machine · rest outcome · time */}
        <span className="flex items-center gap-1.5">
          <Chip vm={vm} awake={awake} />
          <span
            className={cn(
              "min-w-0 shrink truncate font-mono text-[10px] font-medium",
              !awake && "text-muted-foreground",
              evicted && "opacity-80",
            )}
            style={awake ? { color: vm.projectColorFg } : undefined}
          >
            {vm.projectSlug}
          </span>
          {vm.remoteMachineLabel && (
            <span className="shrink-0 font-mono text-[10px] text-muted-foreground-faint">
              @{vm.remoteMachineLabel}
            </span>
          )}
          {!awake && vm.restToken && (
            <span className="shrink-0 font-mono text-[10px] text-muted-foreground-faint">
              · {vm.restToken}
            </span>
          )}
          <span className="ml-auto shrink-0 font-mono text-[10.5px] tabular-nums text-muted-foreground-faint md:group-hover/thread:opacity-0">
            {vm.timeLabel}
          </span>
        </span>

        {/* Title line */}
        <span
          className={cn(
            "mt-px block truncate text-[13px] font-medium text-foreground",
            selected && "text-foreground-bright",
            vm.unread && "font-semibold text-foreground-bright",
            vm.untitled && "font-normal italic text-muted-foreground",
            evicted && "text-muted-foreground",
            vm.struck &&
              "font-normal text-muted-foreground-faint line-through decoration-muted-foreground/50",
          )}
        >
          {vm.untitled ? "Untitled" : vm.name}
        </span>

        {/* State line — awake rows only */}
        {awake && vm.livePhrase && (
          <span className="mt-px flex items-center gap-2">
            {vm.badge === "attention" && (
              <span className="size-[7px] shrink-0 animate-pulse rounded-full bg-orange motion-reduce:animate-none" />
            )}
            <span
              className={cn(
                "min-w-0 flex-1 truncate font-mono text-[10.5px] leading-[1.4]",
                TONE_CLASS[vm.livePhrase.tone],
              )}
            >
              {vm.livePhrase.text}
            </span>
            {showTodo && vm.todo && !selected && (
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
        )}

        {/* Focused card (S1) — the row you're inside carries its identity
            facts, a real todo bar, and persistent actions (also the touch
            path: the selected row is the one mobile row with buttons). */}
        {selected && (
          <>
            {(vm.branch || vm.model || vm.turns) && (
              <span className="mt-1 block truncate font-mono text-[10px] text-muted-foreground-faint">
                {[vm.branch, vm.model, vm.turns ? `${vm.turns} turns` : ""]
                  .filter(Boolean)
                  .join(" · ")}
              </span>
            )}
            {showTodo && vm.todo && (
              <span className="mt-1.5 flex items-center gap-2">
                <span className="h-[3px] min-w-0 flex-1 overflow-hidden rounded-full bg-border/60">
                  <span
                    className="block h-full rounded-full bg-primary"
                    style={{ width: `${Math.round((vm.todo.done / vm.todo.total) * 100)}%` }}
                  />
                </span>
                <span className="shrink-0 font-mono text-[10px] font-medium tabular-nums text-muted-foreground">
                  {vm.todo.done}/{vm.todo.total}
                </span>
              </span>
            )}
          </>
        )}
      </button>

      {selected ? (
        <span className="mt-0.5 flex gap-1 px-2.5 pb-1.5">
          <FocusedAction label={vm.pinned ? "Unpin" : "Pin"} onAction={onTogglePin} />
          <FocusedAction label="Archive" onAction={onArchive} />
        </span>
      ) : (
        <RowActions pinned={vm.pinned} onTogglePin={onTogglePin} onArchive={onArchive} />
      )}
    </div>
  );
});

/** Persistent ghost button on the focused card — no hover gating, all devices. */
function FocusedAction({ label, onAction }: { label: string; onAction: () => void }) {
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation();
        onAction();
      }}
      className={cn(
        "cursor-pointer rounded-md border border-border/50 px-2 py-1 text-[10px] font-semibold",
        "text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground-bright",
      )}
    >
      {label}
    </button>
  );
}
