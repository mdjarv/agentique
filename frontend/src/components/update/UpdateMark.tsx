/**
 * The sidebar-footer update mark (docs/upgrades.md, decision U2).
 *
 * A MARK, not a sentence, and not a control of its own. The footer is one 271px
 * line already carrying identity, liveness and the usage cluster — and the
 * cluster grows, because the set of allowance windows is never hardcoded
 * (docs/usage.md). A pill spelling "Rebuild available" was the longest string on
 * that line: it wrapped to two rows and pushed the codex and disk marks outside
 * the sidebar entirely.
 *
 * The words were never the chip's to carry. `UpdatePopoverRows` renders the
 * label, the detail AND the button in the popover one click away, so the pill
 * spent a third of the footer duplicating what it fronts. What is irreducible is
 * "something is waiting, and it is this kind of thing".
 *
 * So it is a glyph, and it **leads the usage cluster** inside that cluster's own
 * trigger — the control that already opens the popover holding the verb. Inline
 * rather than notched onto a corner: a mark overlapping the last vendor's logo
 * reads as a claim about that vendor, and one floating in the gap is dead pixels
 * beside the control it is about. Inline it also costs width, which is the
 * honest thing for it to do — the account name truncates to pay for it, the way
 * everything else on this line is arranged to.
 *
 * A glyph can say WHICH kind of thing waits, and this one does: `MARK_GLYPH`
 * pairs each with the icon `UpdatePopoverRows` gives its row, so the mark and
 * the row it opens onto cannot disagree. The sentence still belongs to the
 * button, which is what a reader hovers and what a screen reader announces —
 * `useUpdateWaiting` is where it comes from.
 *
 * There is no dismissal. It existed because a sentence in the footer is loud; a
 * glyph is not, and an update that can be waved away is one nobody applies.
 */
import { ArrowUpCircle, GitBranch, type LucideIcon, RotateCw } from "lucide-react";
import { useMemo } from "react";
import type { UpdateStatus } from "~/lib/generated-types";
import { sourceVerdict } from "~/lib/update-source";
import { cn } from "~/lib/utils";
import { behindKeys, useUpdateStore } from "~/stores/update-store";

/**
 * What kind of thing is waiting. Closed, so a new one has to choose its glyph
 * and its words here rather than inherit a blank mark.
 */
type MarkKind =
  /** A published release to download. */
  | "release"
  /** A local checkout that has moved: compile it. */
  | "rebuild"
  /** A newer binary already installed; only the process is old. */
  | "restart"
  /** Several machines, with no single thing to name. */
  | "fleet";

const MARK_GLYPH: Record<MarkKind, LucideIcon> = {
  release: ArrowUpCircle,
  rebuild: GitBranch,
  restart: RotateCw,
  fleet: ArrowUpCircle,
};

interface Waiting {
  kind: MarkKind;
  /** The one line, for the accessible name of the control the mark rides. */
  label: string;
}

/**
 * What is waiting on this fleet, or null when nothing is.
 *
 * Two claims can light the mark — a published release, or a local checkout that
 * has moved — and only one of them has a version to name. Several machines have
 * no single thing to name at all, so they say how many.
 */
function waitingFor(behind: string[], statuses: Record<string, UpdateStatus>): Waiting | null {
  if (behind.length === 0) return null;
  if (behind.length !== 1) {
    return { kind: "fleet", label: `${behind.length} machines behind` };
  }

  const status = statuses[behind[0] as string];
  if (status?.behind) {
    return { kind: "release", label: `Update ${status.latest || "available"}` };
  }

  const verdict = sourceVerdict(status?.source);
  if (verdict.token === "staged") return { kind: "restart", label: "Restart to finish" };
  if (verdict.token === "ready") return { kind: "rebuild", label: "Rebuild available" };
  return { kind: "release", label: "Update available" };
}

/** The sentence for the accessible name and tooltip of whatever control the
 *  mark rides. Null when nothing waits, which is also when `UpdateMark` renders
 *  nothing — one predicate, so the two can never disagree. */
export function useUpdateWaiting(): Waiting | null {
  const statuses = useUpdateStore((s) => s.statuses);
  // Computed outside the selector: behindKeys builds a new array every call,
  // which as a selector return value would re-render forever.
  const behind = useMemo(() => behindKeys(statuses), [statuses]);
  return useMemo(() => waitingFor(behind, statuses), [behind, statuses]);
}

/**
 * The glyph, in the accent colour: the cluster beside it is deliberately muted,
 * so colour is what separates a thing asking for something from a thing merely
 * reporting. `aria-hidden` because the button it sits in carries the words — a
 * mark and its control must not announce the same fact twice.
 *
 * 14px, a little larger than the 11px vendor marks. At 12px `GitBranch` loses
 * its branch node, the same way `FolderGit2` does at 10px (CLAUDE.md, "Where a
 * session's edits land") — and a glyph whose whole job is to say WHICH kind of
 * thing waits cannot afford to be unreadable.
 */
export function UpdateMark({ className }: { className?: string }) {
  const waiting = useUpdateWaiting();
  if (!waiting) return null;

  const Icon = MARK_GLYPH[waiting.kind];
  return <Icon aria-hidden className={cn("size-3.5 shrink-0 text-primary", className)} />;
}
