/**
 * The identity marks a sidebar row wears: which project, and which machine.
 *
 * Shared by every row shape — session rows and draft rows — because a repo has
 * to look the same whichever list it turns up in. Props are primitives, not a
 * view-model, so a row type that is not a session can still wear them.
 */
import { useProjectIcon } from "~/hooks/useProjectIcon";
import { DEFAULT_MACHINE_ICON, getMachineIcon } from "~/lib/machines/icons";
import { cn } from "~/lib/utils";

export interface ChipProps {
  iconId?: string;
  /** Two-letter fallback when the project has no icon. */
  initials: string;
  colorBg: string;
  colorFg: string;
  /**
   * Carry the project hue. Tracks "still mine to deal with", never "a CLI is
   * attached" — see {@link import("./derive").isHued}. Filed rows are grey.
   */
  hued: boolean;
}

/**
 * The 14px inline project chip on the repo line. The identity colour lives here
 * and on the slug, nowhere else.
 */
export function Chip({ iconId, initials, colorBg, colorFg, hued }: ChipProps) {
  const Icon = useProjectIcon(iconId ?? "");
  return (
    <span
      className={cn(
        "flex size-3.5 shrink-0 items-center justify-center rounded",
        !hued && "bg-border/40 text-muted-foreground",
      )}
      style={hued ? { backgroundColor: `${colorBg}26`, color: colorFg } : undefined}
    >
      {Icon ? (
        <Icon className="size-2.5" />
      ) : (
        <span className="text-[7px] font-bold">{initials}</span>
      )}
    </span>
  );
}

export interface MachineTagProps {
  /** Absent for a local row — the tag then renders nothing. */
  label?: string;
  icon?: string;
  offline?: boolean;
  /** A proven fault on that machine — away is silent, this is not. */
  fault?: string;
}

/**
 * The machine a remote row belongs to: its face and its name. Presentation is
 * this host's (docs/multi-machine.md) — the face is a recognition aid, so an
 * unset icon falls back to the generic server glyph rather than nothing.
 */
export function MachineTag({ label, icon, offline, fault }: MachineTagProps) {
  if (!label) return null;
  const Icon = getMachineIcon(icon ?? "") ?? DEFAULT_MACHINE_ICON;
  return (
    <span
      title={fault ?? (offline ? `${label} is offline — showing its last known state` : undefined)}
      className={cn(
        "flex shrink-0 items-center gap-0.5 font-mono text-[10px]",
        // Away is a dimmer, not an alarm: the row stays readable and navigable,
        // it just stops claiming to be live. A *proven* fault is the exception —
        // it will never clear on its own, so it gets the one colour that means
        // something is wrong.
        fault ? "text-destructive" : "text-muted-foreground-faint",
        offline && !fault && "opacity-55",
      )}
    >
      <Icon className="size-2.5 shrink-0" />
      {label}
    </span>
  );
}
