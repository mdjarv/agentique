/**
 * The sidebar-footer update mark (docs/upgrades.md, decision U2).
 *
 * A MARK, not a sentence, and not an element of its own. The footer is one
 * 271px line already carrying identity, liveness and the usage cluster — and
 * the cluster grows, because the set of allowance windows is never hardcoded
 * (docs/usage.md). A pill spelling "Rebuild available" was the longest string
 * on that line: it wrapped to two rows and pushed the codex and disk marks
 * outside the sidebar entirely.
 *
 * The words were never the chip's to carry anyway. `UpdatePopoverRows` renders
 * the label, the detail AND the button in the popover one click away, so the
 * pill spent a third of the footer duplicating what it fronts. What is
 * irreducible is "there is something waiting, here" — a dot.
 *
 * It rides the usage trigger rather than sitting beside it, for the reason the
 * sidebar's notches ride the chip: a mark at a constant position is found by
 * glancing, and one that claims width can push its neighbours off the row. That
 * trigger already opens the popover whose first section is the verb, so the dot
 * needs no click of its own — and must not take one, or it would swallow the
 * click meant for the control underneath.
 *
 * A dot cannot say WHICH kind of thing waits, so the control it rides says it:
 * `useUpdateWaiting` hands the footer the sentence for that button's tooltip
 * and accessible name. Nothing is hidden from a screen reader by a mark.
 *
 * The dismissal went with the pill. It existed because a sentence in the footer
 * is loud; a dot is not, and an update that can be waved away is one nobody
 * applies.
 */
import { useMemo } from "react";
import type { UpdateStatus } from "~/lib/generated-types";
import { sourceVerdict } from "~/lib/update-source";
import { cn } from "~/lib/utils";
import { behindKeys, useUpdateStore } from "~/stores/update-store";

/**
 * The one line describing what is waiting on this fleet, or null when nothing
 * is.
 *
 * Two claims can light the mark — a published release, or a local checkout that
 * has moved — and only one of them has a version to name. Several machines have
 * no single thing to name at all, so they say how many.
 */
function waitingLabel(behind: string[], statuses: Record<string, UpdateStatus>): string | null {
  if (behind.length === 0) return null;
  if (behind.length !== 1) return `${behind.length} machines behind`;

  const status = statuses[behind[0] as string];
  if (status?.behind) return `Update ${status.latest || "available"}`;

  const verdict = sourceVerdict(status?.source);
  if (verdict.token === "staged") return "Restart to finish";
  if (verdict.token === "ready") return "Rebuild available";
  return "Update available";
}

/** What is waiting, as a sentence — for the accessible name of whatever control
 *  the mark rides. Null when nothing is waiting, which is also when
 *  `UpdateMark` renders nothing. */
export function useUpdateWaiting(): string | null {
  const statuses = useUpdateStore((s) => s.statuses);
  // Computed outside the selector: behindKeys builds a new array every call,
  // which as a selector return value would re-render forever.
  const behind = useMemo(() => behindKeys(statuses), [statuses]);
  return useMemo(() => waitingLabel(behind, statuses), [behind, statuses]);
}

/**
 * The dot. Positioned by its caller, which owns the control it marks.
 *
 * `pointer-events-none` is load-bearing: the mark sits over a button, and a dot
 * that ate the click would make the affordance it advertises unreachable.
 * `aria-hidden` for the same division of labour — the button says the words.
 */
export function UpdateMark({ className }: { className?: string }) {
  const waiting = useUpdateWaiting();
  if (!waiting) return null;

  return (
    <span
      aria-hidden
      className={cn(
        "pointer-events-none block size-2 rounded-full bg-primary ring-2 ring-sidebar",
        className,
      )}
    />
  );
}
