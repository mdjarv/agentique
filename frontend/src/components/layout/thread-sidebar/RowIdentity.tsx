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
  /**
   * There is something here you have not seen. Renders the notch — see
   * {@link Chip} for why the mark rides the chip and not the row's own colour.
   */
  unread?: boolean;
}

/**
 * The 14px inline project chip on the repo line. The identity colour lives here
 * and on the slug, nowhere else.
 *
 * The chip is also where an unread row wears its mark, as a green dot notched
 * out of the top-right corner. It goes here rather than anywhere else on the
 * row because the chips are the one element at exactly the same x on every row
 * shape — full, compact, selected — so a break in that column is findable
 * without reading. It is deliberately only half the signal: the word `new` on
 * the title line is the other half, the same way every rest state on the row is
 * a glyph *and* a word (`· ✓ finished`). The glyph is what you scan, the word
 * is what you read.
 *
 * The notch survives the grey rule: a filed chip is grey and the dot stays
 * green, which is right — grey means filed, the dot means unseen, and those are
 * different claims.
 */
export function Chip({ iconId, initials, colorBg, colorFg, hued, unread }: ChipProps) {
  const Icon = useProjectIcon(iconId ?? "");
  const square = (
    <span
      className={cn(
        "flex size-3.5 items-center justify-center rounded",
        !hued && "bg-border/40 text-muted-foreground",
        // Cut the corner away rather than ringing the dot — the gap is then the
        // row's actual ground in every state. See `.chip-notched` in index.css.
        unread && "chip-notched",
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

  // The dot is a *sibling* of the square: a mask paints its children too, so a
  // dot nested inside would be cut away by the very notch it sits in.
  if (!unread) return <span className="flex shrink-0">{square}</span>;
  return (
    <span className="relative flex shrink-0">
      {square}
      <span className="absolute -right-0.5 -top-0.5 size-[7px] rounded-full bg-success" />
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
