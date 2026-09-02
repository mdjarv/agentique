/**
 * An unsent New-session prompt, as a sidebar row.
 *
 * Two lines, mirroring a resting session row: the repo line it will be created
 * against, then what it says. It is not a session — no state, no outcome, no
 * pin, nothing to archive — so the only action is discarding the text, and the
 * row wears a dashed outline: the shape of a session, not one yet.
 */
import { Trash2 } from "lucide-react";
import { memo } from "react";
import { cn } from "~/lib/utils";
import { Chip, MachineTag } from "./RowIdentity";
import type { DraftRowVM } from "./types";

interface DraftRowProps {
  vm: DraftRowVM;
  /** The New-session view for this project is the one on screen. */
  selected: boolean;
  onClick: () => void;
  onDiscard: () => void;
}

export const DraftRow = memo(function DraftRow({
  vm,
  selected,
  onClick,
  onDiscard,
}: DraftRowProps) {
  return (
    <div
      className={cn(
        // The outline is the mark: a row drawn but not yet filled in. It sits on
        // the wrapper so the whole row reads as the placeholder, and it is the
        // only thing separating a draft from a session at a glance.
        "group/thread relative rounded-lg border border-dashed transition-colors",
        selected
          ? "border-border bg-sidebar-accent"
          : "border-border/60 group-hover/thread:border-border",
      )}
    >
      <button
        type="button"
        aria-label={`${vm.title}, unsent draft, ${vm.projectLabel}`}
        onClick={onClick}
        className={cn(
          "block w-full cursor-pointer select-none rounded-lg px-2.5 py-1.5 text-left transition-colors",
          "focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring/50",
          "max-md:min-h-11",
          !selected && "group-hover/thread:bg-sidebar-accent/60",
        )}
      >
        {/* Repo line: chip · slug · @machine. A draft has no time — the store
            keeps text and nothing else — so the slot stays empty rather than
            carrying a number that would be invented. */}
        <span className="flex items-center gap-1.5">
          <Chip
            iconId={vm.projectIconId}
            initials={vm.projectInitials}
            colorBg={vm.projectColorBg}
            colorFg={vm.projectColorFg}
            hued
          />
          <span
            className="min-w-0 shrink truncate font-mono text-[10px] font-medium"
            style={{ color: vm.projectColorFg }}
          >
            {vm.projectLabel}
          </span>
          <MachineTag
            label={vm.remoteMachineLabel}
            icon={vm.remoteMachineIcon}
            platform={vm.remoteMachinePlatform}
            offline={vm.remoteMachineOffline}
          />
        </span>

        {/* The draft's own words are its title — there is no name to show. */}
        <span
          className={cn(
            "mt-px block truncate text-[13px] font-medium",
            selected ? "text-foreground-bright" : "text-foreground",
          )}
        >
          {vm.title}
          {vm.more && "…"}
        </span>
      </button>

      {selected ? (
        <span className="mt-0.5 flex gap-1 px-2.5 pb-1.5">
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onDiscard();
            }}
            className={cn(
              "cursor-pointer rounded-md border border-border/50 px-2 py-1 text-[10px] font-semibold",
              "text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground-bright",
            )}
          >
            Discard
          </button>
        </span>
      ) : (
        <button
          type="button"
          aria-label="Discard draft"
          title="Discard draft"
          onClick={(e) => {
            e.stopPropagation();
            onDiscard();
          }}
          className={cn(
            "absolute right-2 top-1.5 hidden size-5 cursor-pointer items-center justify-center rounded-md",
            "text-muted-foreground hover:bg-secondary hover:text-foreground-bright",
            "md:group-hover/thread:flex",
          )}
        >
          <Trash2 className="size-3" />
        </button>
      )}
    </div>
  );
});
