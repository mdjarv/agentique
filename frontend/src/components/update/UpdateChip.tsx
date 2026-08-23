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
import { cn } from "~/lib/utils";
import { behindKeys, useUpdateStore } from "~/stores/update-store";

export function UpdateChip({ className }: { className?: string }) {
  const [open, setOpen] = useState(false);
  const statuses = useUpdateStore((s) => s.statuses);
  const dismissed = useUpdateStore((s) => s.dismissed);
  const dismiss = useUpdateStore((s) => s.dismiss);

  // Computed outside the selector: behindKeys builds a new array every call,
  // which as a selector return value would re-render forever.
  const behind = useMemo(() => behindKeys(statuses), [statuses]);

  const visible = !dismissed && behind.length > 0;
  const first = behind[0];
  const label =
    behind.length === 1 && first
      ? `Update ${statuses[first]?.latest ?? "available"}`
      : `${behind.length} machines behind`;

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
