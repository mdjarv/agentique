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
 * What it looks like is not decided here. `lib/update-mark.ts` owns the kind and
 * its glyph, and `UpdatePopoverRows` reads the same table, so the mark and the
 * row it opens onto cannot disagree. The sentence belongs to the button, which
 * is what a reader hovers and what a screen reader announces.
 *
 * There is no dismissal. It existed because a sentence in the footer is loud; a
 * glyph is not, and an update that can be waved away is one nobody applies.
 */
import { useMemo } from "react";
import { MARK_GLYPH, type Waiting, waitingFor } from "~/lib/update-mark";
import { cn } from "~/lib/utils";
import { behindKeys, useUpdateStore } from "~/stores/update-store";

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
 * 14px, a little larger than the 11px vendor marks, because a glyph whose job is
 * to say WHICH kind of thing waits cannot afford to be unreadable. `GitBranch`
 * held this slot until it turned out to say "git" rather than "upgrade" — and it
 * was already losing its branch node by 12px, the way `FolderGit2` does at 10px
 * (CLAUDE.md, "Where a session's edits land").
 */
export function UpdateMark({ className }: { className?: string }) {
  const waiting = useUpdateWaiting();
  if (!waiting) return null;

  const Icon = MARK_GLYPH[waiting.kind];
  return <Icon aria-hidden className={cn("size-3.5 shrink-0 text-primary", className)} />;
}
