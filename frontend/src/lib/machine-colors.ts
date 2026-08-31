/**
 * A hue per machine, so "which machine is this session on" can be answered
 * without reading.
 *
 * Derived, not stored. Machines carry a label and an icon and no colour, and a
 * colour column would have to be added to the catalog, replicated across peers
 * and picked in settings before the first session could wear one. Projects
 * solved this already: `getProjectColor` takes an explicit colour when there is
 * one and otherwise assigns deterministically by sorting the ids and indexing
 * the palette. Point the same rule at machine ids and every machine has a
 * stable hue today; an explicit `color` on the row can follow later without
 * changing a single caller here.
 *
 * The primary machine deliberately gets no hue. A wash means "this session runs
 * somewhere else", so the machine you are sitting at has to be the one that
 * doesn't paint anything — which is also why `machineHue` takes the id as
 * `undefined` for local and returns null rather than a default colour.
 */
import { COLORS } from "~/lib/color-palette";

export interface MachineHue {
  /** Bright variant — tinted grounds (`${bg}20`), the wash, dots. */
  bg: string;
  /** Theme-appropriate ink for text and borders. */
  fg: string;
}

type ResolvedTheme = "light" | "dark";

/**
 * The hue for a remote machine, or null for the primary.
 *
 * `allMachineIds` is sorted here rather than by the caller: the assignment has
 * to be stable across renders and across surfaces, and a caller passing an
 * already-ordered array (say, catalog order, which changes when a machine is
 * renamed) would silently reshuffle every machine's colour.
 */
export function machineHue(
  machineId: string | undefined | null,
  allMachineIds: string[],
  resolvedTheme: ResolvedTheme,
): MachineHue | null {
  if (!machineId) return null;
  const sorted = [...allMachineIds].sort();
  const idx = sorted.indexOf(machineId);
  // An id the catalog has not caught up with still deserves a colour rather
  // than a blank: fall back to the first entry instead of returning null, which
  // would read as "this is the primary machine".
  const c = COLORS[idx >= 0 ? idx % COLORS.length : 0] ?? COLORS[0];
  return { bg: c.bg, fg: resolvedTheme === "dark" ? c.bg : c.fgLight };
}

/**
 * The header's wash: a band of the machine's hue fading out to the right.
 *
 * Returned as a style object rather than a class because the hue is a hex value
 * from the palette, not a token. Away drains it to neutral — the composer
 * disables itself in the same moment, and two quiet signals agreeing is what
 * makes a pane visibly go cold rather than a placeholder nobody reads.
 */
export function machineWash(
  hue: MachineHue | null,
  opts: { away?: boolean } = {},
): React.CSSProperties | undefined {
  if (!hue) return undefined;
  const tint = opts.away ? "#8b95b0" : hue.bg;
  return {
    backgroundImage: `linear-gradient(90deg, ${tint}26, transparent 58%)`,
  };
}
