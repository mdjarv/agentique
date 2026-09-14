/**
 * The footer's steward mark (docs/peers.md): this machine has something only a
 * hand can fix. A glyph, not a sentence, on the footer's rule — the words are
 * one click away in `FindingsPopoverRows`, and the trigger it rides says them
 * for hover and for a screen reader.
 *
 * It rides the usage cluster's trigger beside `UpdateMark`, for the reason that
 * mark does: that control opens the popover holding the rows. In the warning
 * colour rather than the accent: an upgrade is an offer, a signed-out CLI is a
 * fault.
 */
import { useMemo } from "react";
import { FINDING_GLYPH, findingsLabel } from "~/lib/steward";
import { cn } from "~/lib/utils";
import { useStewardStore } from "~/stores/steward-store";

/** The trigger's words, or null when the mark renders nothing. */
export function useFindingsLabel(): string | null {
  const findings = useStewardStore((s) => s.findings);
  return useMemo(() => findingsLabel(findings), [findings]);
}

export function FindingsMark({ className }: { className?: string }) {
  const label = useFindingsLabel();
  if (!label) return null;
  const Icon = FINDING_GLYPH;
  return <Icon aria-hidden className={cn("size-3.5 shrink-0 text-warning", className)} />;
}
