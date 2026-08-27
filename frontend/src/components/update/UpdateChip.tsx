/**
 * The sidebar-footer update chip (docs/upgrades.md, decision U2).
 *
 * Appears when any machine has a published upgrade waiting, and can be waved
 * away — but the dismissal is deliberately page-session-scoped and never
 * touches storage. Reload and it is back. An update that can be permanently
 * silenced is one nobody applies.
 */
import { ArrowUpCircle, X } from "lucide-react";
import { useMemo, useState } from "react";
import { UpdateDialog } from "~/components/update/UpdateDialog";
import type { UpdateStatus } from "~/lib/generated-types";
import { sourceVerdict } from "~/lib/update-source";
import { cn } from "~/lib/utils";
import { behindKeys, useUpdateStore } from "~/stores/update-store";

/**
 * What the chip says.
 *
 * Two claims can light it — a published release, or a local checkout that has
 * moved — and only one of them has a version to name. So a single machine
 * behind a release still says which version, and everything else says what kind
 * of thing is waiting rather than inventing a number for it.
 */
function chipLabel(behind: string[], statuses: Record<string, UpdateStatus>): string {
  if (behind.length !== 1) return `${behind.length} machines behind`;
  const status = statuses[behind[0] as string];
  if (status?.behind) return `Update ${status.latest || "available"}`;
  const verdict = sourceVerdict(status?.source);
  if (verdict.token === "staged") return "Restart to finish";
  if (verdict.token === "ready") return "Rebuild available";
  return "Update available";
}

export function UpdateChip({ className }: { className?: string }) {
  const [open, setOpen] = useState(false);
  const statuses = useUpdateStore((s) => s.statuses);
  const dismissed = useUpdateStore((s) => s.dismissed);
  const dismiss = useUpdateStore((s) => s.dismiss);

  // Computed outside the selector: behindKeys builds a new array every call,
  // which as a selector return value would re-render forever.
  const behind = useMemo(() => behindKeys(statuses), [statuses]);

  const visible = !dismissed && behind.length > 0;
  const label = useMemo(() => chipLabel(behind, statuses), [behind, statuses]);

  return (
    <>
      {visible && (
        <span
          className={cn(
            "flex items-center gap-0.5 rounded-full bg-primary/15 py-0.5 pl-2 pr-0.5 text-[10px] font-semibold text-primary",
            className,
          )}
        >
          <button
            type="button"
            onClick={() => setOpen(true)}
            title="See what every machine is running"
            className="flex cursor-pointer items-center gap-1"
          >
            <ArrowUpCircle className="size-2.5 shrink-0" />
            {label}
          </button>
          <button
            type="button"
            onClick={dismiss}
            aria-label="Hide until reload"
            title="Hide until reload"
            className="flex cursor-pointer items-center rounded-full p-0.5 text-primary/60 transition-colors hover:bg-primary/15 hover:text-primary"
          >
            <X className="size-2.5" />
          </button>
        </span>
      )}
      {/* Kept mounted independently of the chip: dismissing while the dialog
          is open must close the chip, not the dialog under the cursor. */}
      <UpdateDialog open={open} onOpenChange={setOpen} />
    </>
  );
}
