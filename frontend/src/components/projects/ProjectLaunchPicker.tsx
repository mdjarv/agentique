/**
 * "Where should this run" as a searchable palette — the sidebar New button's
 * shape (filter field, keyboard nav, favorites first), narrowed to one job:
 * naming a launch target.
 *
 * Rows are physical checkouts, not repos (`lib/machines/launch-targets.ts`).
 * A repo held on three machines lists three rows, because a command targets
 * one machine and the caller has to be able to say which. The machine is part
 * of the search text, so "agentique zbook" is one query rather than a repo
 * pick followed by a second control. A single-machine repo shows no machine
 * chrome at all — there is nothing to choose.
 *
 * The trigger is the caller's (`children`), so a split button, a menu row or a
 * plain button can all open the same list.
 */
import { Check, Search } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import { ProjectPill } from "~/components/ui/project-pill";
import { useLaunchTargets } from "~/hooks/useLaunchTargets";
import type { LaunchTarget } from "~/lib/machines/launch-targets";
import { matchesLaunchTarget } from "~/lib/machines/launch-targets";
import { resolveMachineGlyph } from "~/lib/machines/platform";
import { cn } from "~/lib/utils";

export function ProjectLaunchPicker({
  /** Physical project id the caller is currently pointed at — ticked in the list. */
  targetProjectId,
  onPick,
  placeholder = "Start in…",
  align = "end",
  children,
}: {
  targetProjectId?: string;
  onPick: (target: LaunchTarget) => void;
  placeholder?: string;
  align?: "start" | "center" | "end";
  children: React.ReactNode;
}) {
  const targets = useLaunchTargets();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [rawSelectedIdx, setSelectedIdx] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  const filtered = useMemo(
    () => targets.filter((t) => matchesLaunchTarget(t, search)),
    [targets, search],
  );
  const selectedIdx = filtered.length === 0 ? 0 : Math.min(rawSelectedIdx, filtered.length - 1);

  useEffect(() => {
    if (!open) return;
    setSearch("");
    // Open on the current target rather than the top of the list: the list is
    // long, and "where am I now" is the question the first glance asks.
    const current = targets.findIndex((t) => t.projectId === targetProjectId);
    setSelectedIdx(current === -1 ? 0 : current);
  }, [open, targets, targetProjectId]);

  const pick = useCallback(
    (target: LaunchTarget) => {
      if (target.offline) return;
      setOpen(false);
      onPick(target);
    },
    [onPick],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSelectedIdx((i) => Math.min(i + 1, filtered.length - 1));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setSelectedIdx((i) => Math.max(i - 1, 0));
      } else if (e.key === "Enter") {
        e.preventDefault();
        const target = filtered[selectedIdx];
        // Enter obeys the same rule the row does: an offline machine takes
        // nothing, however it was reached.
        if (target) pick(target);
      }
    },
    [filtered, selectedIdx, pick],
  );

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      <PopoverContent
        align={align}
        collisionPadding={8}
        onOpenAutoFocus={(e) => {
          e.preventDefault();
          inputRef.current?.focus();
        }}
        className="w-72 max-w-[calc(100vw-16px)] overflow-hidden p-0 shadow-xl"
      >
        <div className="flex items-center gap-2 border-b border-border/50 px-3 py-2">
          <Search className="size-3.5 shrink-0 text-muted-foreground-faint" />
          <input
            ref={inputRef}
            type="text"
            placeholder={placeholder}
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              // The selection is an index into the FILTERED list, so a new
              // query has to put it back at the top: keeping the old number
              // lands on whatever now happens to sit there.
              setSelectedIdx(0);
            }}
            onKeyDown={handleKeyDown}
            className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground-faint"
          />
        </div>
        <div className="max-h-64 overflow-y-auto py-1">
          {filtered.map((target, i) => (
            <TargetRow
              key={target.projectId}
              target={target}
              active={i === selectedIdx}
              current={target.projectId === targetProjectId}
              onHover={() => setSelectedIdx(i)}
              onPick={() => pick(target)}
            />
          ))}
          {filtered.length === 0 && (
            <div className="px-3 py-2.5 text-xs text-muted-foreground-faint">
              No matching projects
            </div>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function TargetRow({
  target,
  active,
  current,
  onHover,
  onPick,
}: {
  target: LaunchTarget;
  active: boolean;
  current: boolean;
  onHover: () => void;
  onPick: () => void;
}) {
  const ref = useRef<HTMLButtonElement>(null);
  // The keyboard moves the selection through a list taller than its box, so
  // the active row has to bring itself into view or the arrow keys walk off
  // the bottom into nothing.
  useEffect(() => {
    if (active) ref.current?.scrollIntoView({ block: "nearest" });
  }, [active]);

  // The machine only earns a line when there is a choice to make: a repo that
  // spans machines, or one that lives somewhere other than here.
  const showMachine = target.spansMachines || !!target.machineId;
  const MachineIcon = resolveMachineGlyph(
    target.machineId ? target.machineIcon : "",
    target.machinePlatform,
  );

  return (
    <button
      ref={ref}
      type="button"
      onMouseEnter={onHover}
      onClick={onPick}
      disabled={target.offline}
      title={target.offline ? `${target.machineLabel} is offline — ${target.path}` : target.path}
      className={cn(
        "flex w-full items-center gap-2 px-3 py-1.5 text-left",
        active && !target.offline && "bg-accent",
        target.offline ? "cursor-not-allowed opacity-45" : "cursor-pointer",
      )}
    >
      <Check className={cn("size-3 shrink-0", current ? "opacity-100" : "opacity-0")} />
      {/* Presentation is the representative's: the same repo wears one hue
          however many machines hold a copy. */}
      <ProjectPill slug={target.rowSlug} showIcon background={false} className="min-w-0 truncate" />
      {showMachine && (
        <span className="ml-auto flex shrink-0 items-center gap-1 text-[10px] text-muted-foreground">
          <MachineIcon className="size-3" />
          {target.machineLabel}
          {target.offline && <span className="text-muted-foreground-faint">offline</span>}
        </span>
      )}
    </button>
  );
}
