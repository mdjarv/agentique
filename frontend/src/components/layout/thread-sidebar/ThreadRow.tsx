import {
  Archive,
  ArchiveRestore,
  BookOpen,
  Bot,
  Check,
  CircleHelp,
  Diamond,
  GitMerge,
  Globe,
  Hash,
  ListChecks,
  Pencil,
  Pin,
  PinOff,
  Plug,
  Settings2,
  Terminal,
  TriangleAlert,
  X,
} from "lucide-react";
import { memo } from "react";
import { REST_GLYPH, type RestToken } from "~/lib/session/rest-state";
import { WORKSPACE_GLYPH, workspaceTitle } from "~/lib/session/workspace";
import { cn } from "~/lib/utils";
import { Chip, MachineTag } from "./RowIdentity";
import type { MachineTone, ThreadBadge, ThreadRowVM, WorkKind, WorkspaceKind } from "./types";

const TONE_CLASS: Record<MachineTone, string> = {
  work: "text-teal",
  attn: "text-orange",
  fail: "text-destructive",
  merge: "text-primary",
  muted: "text-muted-foreground-faint",
};

/** Spoken state for the row's aria-label. */
const BADGE_ARIA: Record<Exclude<ThreadBadge, null>, string> = {
  working: "working",
  planning: "planning",
  attention: "needs your approval",
  question: "waiting on your answer",
  unread: "finished, unread",
  failed: "failed",
  merging: "merging",
  off: "evicted",
};

/**
 * The state's glyph — one marker per row, and the only thing naming the state
 * now that the phrase carries specifics instead. It replaces the standalone
 * amber ball: the blocked glyphs are the ones that pulse.
 */
const BADGE_GLYPH: Record<Exclude<ThreadBadge, null | "off">, typeof Check> = {
  working: Terminal,
  planning: Diamond,
  attention: TriangleAlert,
  question: CircleHelp,
  unread: Check,
  failed: X,
  merging: GitMerge,
};

/**
 * A working row refines its glyph by what the agent is actually doing, so the
 * marker distinguishes a compile from an edit from a delegated subagent
 * without spending a word. Every other badge ignores the work kind.
 */
const WORK_GLYPH: Record<WorkKind, typeof Check> = {
  run: Terminal,
  edit: Pencil,
  read: BookOpen,
  web: Globe,
  delegate: Bot,
  task: ListChecks,
  plan: Diamond,
  configure: Settings2,
  tool: Plug,
  generic: Terminal,
};

/** Only a row blocked on a human pulses — the amber monopoly, on the glyph. */
function stateGlyph(badge: ThreadBadge, workKind?: WorkKind) {
  if (badge === null || badge === "off") return null;
  const Glyph = badge === "working" ? WORK_GLYPH[workKind ?? "generic"] : BADGE_GLYPH[badge];
  return (
    <Glyph
      className={cn(
        "size-[11px] shrink-0",
        (badge === "attention" || badge === "question") &&
          "animate-pulse motion-reduce:animate-none",
      )}
    />
  );
}

/** The row's machine tag, from the session VM. */
function SessionMachineTag({ vm }: { vm: ThreadRowVM }) {
  return (
    <MachineTag
      label={vm.remoteMachineLabel}
      icon={vm.remoteMachineIcon}
      offline={vm.remoteMachineOffline}
      fault={vm.remoteMachineFault}
    />
  );
}

/** The row's project chip, from the session VM. Hue is the caller's call: the
 *  filed sections render grey whatever the row's own rule says. The unread
 *  notch is not — an archived row you have not read is still unread. */
function SessionChip({ vm, hued }: { vm: ThreadRowVM; hued: boolean }) {
  return (
    <Chip
      iconId={vm.projectIconId}
      initials={vm.projectInitials}
      colorBg={vm.projectColorBg}
      colorFg={vm.projectColorFg}
      hued={hued}
      unread={vm.unread}
      parked={vm.parked}
    />
  );
}

/**
 * The right-aligned timestamp. It yields the corner to {@link RowActions} —
 * on hover, and for as long as the row is the focused one — rather than being
 * overlapped by them; `opacity` and not `hidden`, so the row never reflows.
 */
function TimeSlot({ label, yielded }: { label: string; yielded: boolean }) {
  return (
    <span
      className={cn(
        "ml-auto shrink-0 font-mono text-[10.5px] tabular-nums text-muted-foreground-faint",
        yielded ? "opacity-0" : "md:group-hover/thread:opacity-0",
      )}
    >
      {label}
    </span>
  );
}

interface ThreadRowProps {
  vm: ThreadRowVM;
  selected: boolean;
  /** Dim settled row (title over identity) for the shelf and Archived sections. */
  compact?: boolean;
  onClick: () => void;
  onTogglePin: () => void;
  onArchive: () => void;
}

function rowAriaLabel(vm: ThreadRowVM): string {
  const name = vm.untitled ? "Untitled" : vm.name;
  // `off` IS the resting state, so its word is the rest token — which knows the
  // difference between a CLI we reclaimed ("evicted") and a machine we cannot
  // see ("away"). Every other badge names something happening right now.
  const spoken = vm.badge && vm.badge !== "off" ? BADGE_ARIA[vm.badge] : vm.restToken;
  return [name, spoken || "at rest", vm.projectLabel, vm.timeLabel].filter(Boolean).join(", ");
}

/**
 * Where the session's edits land, beside the project it lands in.
 *
 * Glyph only, and in the repo line's own faint ink rather than the header's
 * amber: the header warns once, at the top of the thing you are about to type
 * into, while the rail shows every row at once. Painting a colour on each local
 * row would be a claim on attention that repeats down the whole list.
 */
function WorkspaceMark({ kind }: { kind: WorkspaceKind }) {
  const Glyph = WORKSPACE_GLYPH[kind];
  return (
    <Glyph className="size-2.5 shrink-0" role="img" aria-label={workspaceTitle(kind)}>
      <title>{workspaceTitle(kind)}</title>
    </Glyph>
  );
}

/** The outcome word with its mark, folded into the repo line at rest. */
function RestMark({ token }: { token: Exclude<RestToken, ""> }) {
  const Glyph = REST_GLYPH[token];
  return (
    <span className="flex shrink-0 items-center gap-1 font-mono text-[10px] text-muted-foreground-faint">
      ·<Glyph className="size-2.5 shrink-0" />
      {token}
    </span>
  );
}

/**
 * The unread marker's spoken half: one word at the right edge of the **title**
 * line, in the same 10px mono the rest tokens use one line above it. Only the
 * colour differs, and that is the point — every state on this row is stated as
 * a lowercase word (`stopped`, `finished`, `merged`), so unread is too. It was
 * a filled and outlined pill, which made it the only object anywhere in the
 * sidebar that was both, and 42px wide on a 252px line.
 *
 * It sits on the title line and not the identity line's time slot because that
 * slot is where the row's actions come in on hover and stay on the focused row,
 * and a mark that means *unread* cannot yield the way a timestamp can.
 *
 * The other half is the notch on the chip ({@link Chip}) — this says what that
 * one means.
 */
function NewMark() {
  return (
    <span className="shrink-0 font-mono text-[10px] font-medium tracking-[0.04em] text-success">
      new
    </span>
  );
}

/**
 * Pin and archive, in the row's top-right corner over the time slot.
 *
 * `persistent` is the focused row (and the touch path with it): the same two
 * buttons in the same corner, just no longer hover-gated. They stay a corner
 * affordance rather than growing a labelled row beneath the card, because that
 * row's height is paid by every selection, on a list whose whole point is
 * density.
 */
function RowActions({
  vm,
  persistent = false,
  onTogglePin,
  onArchive,
}: {
  vm: ThreadRowVM;
  persistent?: boolean;
  onTogglePin: () => void;
  onArchive: () => void;
}) {
  const PinIcon = vm.pinned ? PinOff : Pin;
  const ArchiveIcon = vm.archived ? ArchiveRestore : Archive;
  return (
    <span
      className={cn(
        "absolute right-2 top-0.5 gap-0.5",
        persistent ? "flex" : "hidden md:group-hover/thread:flex",
      )}
    >
      {[
        {
          label: vm.pinned ? "Unpin" : "Pin",
          action: onTogglePin,
          icon: <PinIcon className="size-3" />,
        },
        // Archiving is refused while a turn is in flight, so a working row
        // offers only the pin — no button that can only fail.
        ...(vm.archived || vm.canArchive
          ? [
              {
                label: vm.archived ? "Unarchive" : "Archive",
                action: onArchive,
                icon: <ArchiveIcon className="size-3" />,
              },
            ]
          : []),
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
          className={cn(
            "flex size-5 cursor-pointer items-center justify-center rounded-md",
            "text-muted-foreground hover:bg-secondary hover:text-foreground-bright",
            // Persistent is the only variant a finger ever meets.
            persistent && "max-md:size-6",
          )}
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
    // Two lines, even settled: a long remote slug (`webticket-ui~ad3e932
    // @zbook`) shares the line with the name and starves it down to "W…".
    // The title owns line one; identity recedes to a dim mono line under it.
    return (
      <div className="group/thread relative">
        <button
          type="button"
          aria-label={rowAriaLabel(vm)}
          onClick={onClick}
          className={cn(
            "flex w-full cursor-pointer select-none items-start gap-2 rounded-lg px-2.5 py-1.5 text-left transition-colors",
            "focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring/50",
            "max-md:min-h-11",
            selected ? "bg-sidebar-accent" : "group-hover/thread:bg-sidebar-accent/60",
          )}
        >
          <span className="mt-0.5 shrink-0">
            {/* The shelf and Archived are the filed sections — grey by
                construction, whatever the row's own hue rule says. */}
            <SessionChip vm={vm} hued={false} />
          </span>
          <span className="min-w-0 flex-1">
            <span className="flex items-baseline gap-2">
              <span
                className={cn(
                  "min-w-0 flex-1 truncate text-[12.5px] text-muted-foreground",
                  vm.struck && "line-through decoration-muted-foreground/50",
                  vm.untitled && "italic",
                )}
              >
                {vm.untitled ? "Untitled" : vm.name}
              </span>
              <TimeSlot label={vm.timeLabel} yielded={selected} />
            </span>
            <span className="mt-px flex items-center gap-1.5 font-mono text-[10px] text-muted-foreground-faint">
              <span className="min-w-0 truncate">{vm.projectLabel}</span>
              <WorkspaceMark kind={vm.workspace} />
              <SessionMachineTag vm={vm} />
              {vm.unread && (
                <span className="ml-auto">
                  <NewMark />
                </span>
              )}
            </span>
          </span>
        </button>
        <RowActions vm={vm} persistent={selected} onTogglePin={onTogglePin} onArchive={onArchive} />
      </div>
    );
  }

  const awake = vm.awake;
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
          <SessionChip vm={vm} hued={vm.hued} />
          <span
            className={cn(
              "min-w-0 shrink truncate font-mono text-[10px] font-medium",
              !vm.hued && "text-muted-foreground",
            )}
            style={vm.hued ? { color: vm.projectColorFg } : undefined}
          >
            {vm.projectLabel}
          </span>
          <WorkspaceMark kind={vm.workspace} />
          <SessionMachineTag vm={vm} />
          {/* Only an OUTCOME earns words here. Parked states ride the chip's
              corner instead: they are the row's least consequential fact and
              were its longest word, and giving them up is what lets "finished"
              and "merged" read louder without being made louder.
              Unread rows show the word too — their third line is gone, so
              "finished" / "merged" has nowhere else to live. */}
          {(!awake || vm.unread) && vm.restToken && !vm.parked && <RestMark token={vm.restToken} />}
          <TimeSlot label={vm.timeLabel} yielded={selected} />
        </span>

        {/* Title line — and the unread pill, which lives a line below the
            corner the actions occupy. */}
        <span className="mt-px flex items-center gap-2">
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
          {vm.unread && <NewMark />}
        </span>

        {/* State line — awake rows only: glyph names the state, words carry
            the specifics. */}
        {awake && vm.livePhrase && (
          <span className={cn("mt-px flex items-center gap-1.5", TONE_CLASS[vm.livePhrase.tone])}>
            {stateGlyph(vm.badge, vm.workKind)}
            <span className="min-w-0 flex-1 truncate font-mono text-[10.5px] leading-[1.4]">
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
            facts and a real todo bar. Its actions are not here: they are the
            corner buttons, un-gated (also the touch path — the selected row is
            the one mobile row with buttons). */}
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

      <RowActions vm={vm} persistent={selected} onTogglePin={onTogglePin} onArchive={onArchive} />
    </div>
  );
});
