/**
 * Where a session's code *will* live — the location pill, before there is a
 * session to put it on.
 *
 * This is the same element the header carries, in its picked mode: two zones,
 * one caret each, the same glyphs and the same colours. What you choose in the
 * middle of the page is the thing that ends up in the header, in the same
 * shape, which is the whole argument for drawing it this way rather than as a
 * pair of segmented rows.
 *
 * It replaces two halves of one address that used to sit 400px apart: the host
 * picker in this hero, and a Worktree/Local toggle down in the composer. Being
 * a composer control was the worse half of that — it forked the new-session
 * composer from the in-session one, and on the phone that row cannot afford a
 * two-zone pill with two carets, so it would have gone straight into the tools
 * tray, which is the one place a location must never be.
 */
import { Check, ChevronDown, FolderOpen, GitBranch, Monitor } from "lucide-react";
import { useShallow } from "zustand/react/shallow";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { useTheme } from "~/hooks/useTheme";
import { machineHue } from "~/lib/machine-colors";
import { getMachineIcon } from "~/lib/machines/icons";
import { platformGlyph, resolveMachineGlyph } from "~/lib/machines/platform";
import { WORKTREE_LABEL } from "~/lib/session/location";
import type { Project } from "~/lib/types";
import { cn } from "~/lib/utils";
import { useFeatureStore } from "~/stores/feature-store";
import { useMachineStore } from "~/stores/machine-store";

interface LocationPickerProps {
  /** Every checkout of this logical project — one per machine. */
  members: Project[];
  /** The chosen checkout's project id. */
  targetProjectId: string;
  onTargetChange: (projectId: string) => void;
  worktree: boolean;
  onWorktreeChange: (value: boolean) => void;
  /** The project checkout's branch — what the main worktree is named by. */
  projectBranch?: string;
  disabled?: boolean;
}

const ZONE =
  "inline-flex items-center gap-1.5 px-2.5 py-1 text-xs font-mono min-w-0 transition-colors";

export function LocationPicker({
  members,
  targetProjectId,
  onTargetChange,
  worktree,
  onWorktreeChange,
  projectBranch,
  disabled,
}: LocationPickerProps) {
  const machines = useMachineStore((s) => s.machines);
  const statuses = useMachineStore((s) => s.statuses);
  const allIds = useMachineStore(useShallow((s) => Object.keys(s.machines)));
  const primaryLabel = useFeatureStore((s) => s.machineLabel);
  const primaryIcon = useFeatureStore((s) => s.machineIcon);
  const primaryPlatformOs = useFeatureStore((s) => s.machinePlatformOs);
  const { resolvedTheme } = useTheme();

  const target = members.find((m) => m.id === targetProjectId) ?? members[0];
  const targetEntry = target?.machineId ? machines[target.machineId] : undefined;
  const targetLabel = target?.machineId
    ? (targetEntry?.label ?? "remote")
    : primaryLabel || "This machine";
  const hue = machineHue(target?.machineId, allIds, resolvedTheme === "dark" ? "dark" : "light");
  const TargetGlyph = target?.machineId
    ? resolveMachineGlyph(targetEntry?.icon, targetEntry?.platformOs)
    : (getMachineIcon(primaryIcon) ?? platformGlyph(primaryPlatformOs) ?? Monitor);

  const WorktreeGlyph = worktree ? GitBranch : FolderOpen;
  const worktreeLabel = worktree ? "new worktree" : projectBranch || WORKTREE_LABEL.main;

  return (
    <span className="inline-flex items-stretch rounded-md border border-border/60 bg-muted/30 overflow-hidden">
      <DropdownMenu>
        <DropdownMenuTrigger
          disabled={disabled || members.length < 2}
          className={cn(
            ZONE,
            "cursor-pointer hover:brightness-110 disabled:cursor-default",
            target?.machineId ? "font-medium" : "text-muted-foreground",
          )}
          style={hue ? { backgroundColor: `${hue.bg}26`, color: hue.fg } : undefined}
          title={`Runs on ${targetLabel}`}
        >
          <TargetGlyph className="size-3 shrink-0" />
          <span className="truncate max-w-[14ch]">{targetLabel}</span>
          {members.length > 1 && <ChevronDown className="size-3 shrink-0 opacity-70" />}
        </DropdownMenuTrigger>
        <DropdownMenuContent align="center" className="min-w-[14rem]">
          <div className="px-2 pt-1.5 pb-1 text-[10px] uppercase tracking-wide text-muted-foreground/70 select-none">
            Run on
          </div>
          {members.map((m) => {
            const entry = m.machineId ? machines[m.machineId] : undefined;
            const label = m.machineId ? (entry?.label ?? "remote") : primaryLabel || "This machine";
            // A machine that is merely asleep still belongs in the list — it is
            // where the repo lives, and knowing that is worth more than a
            // shorter menu. It just cannot be picked until it wakes.
            const offline = !!m.machineId && statuses[m.machineId] !== "connected";
            const Glyph = m.machineId
              ? resolveMachineGlyph(entry?.icon, entry?.platformOs)
              : (getMachineIcon(primaryIcon) ?? platformGlyph(primaryPlatformOs) ?? Monitor);
            return (
              <DropdownMenuItem
                key={m.id}
                disabled={offline}
                onClick={() => onTargetChange(m.id)}
                className="text-xs gap-2"
              >
                <Check
                  className={cn("h-3 w-3", m.id === targetProjectId ? "opacity-100" : "opacity-0")}
                />
                <Glyph className="h-3.5 w-3.5 shrink-0" />
                <span className="flex-1 truncate">{label}</span>
                {offline && <span className="text-[10px] text-muted-foreground">offline</span>}
              </DropdownMenuItem>
            );
          })}
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Worktree vs main is one bit with no third option coming, so the zone
          is a toggle rather than a menu — a dropdown was two clicks and a read
          for a choice the glyph and tint already state. The title carries what
          each side means and what a click does. */}
      <button
        type="button"
        disabled={disabled}
        aria-pressed={!worktree}
        onClick={() => onWorktreeChange(!worktree)}
        className={cn(
          ZONE,
          "border-l border-border/50 cursor-pointer hover:brightness-110 disabled:cursor-default",
          worktree ? "text-muted-foreground" : "bg-warning/15 text-warning font-medium",
        )}
        title={
          worktree
            ? "A new linked worktree — its own branch and directory, edits are isolated. Click to work in the main worktree instead."
            : `The main worktree${projectBranch ? ` (${projectBranch})` : ""} — edits land in the checkout everything else is linked to. Click to work in a new linked worktree instead.`
        }
      >
        <WorktreeGlyph className="size-3 shrink-0" />
        <span className="truncate max-w-[16ch]">{worktreeLabel}</span>
      </button>
    </span>
  );
}
