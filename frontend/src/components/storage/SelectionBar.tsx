import { Loader2, RotateCcw, Trash2, X } from "lucide-react";
import { Button } from "~/components/ui/button";
import type { SelectionSummary } from "~/lib/storage/selection";
import { formatBytes } from "~/lib/utils";

/**
 * The bar that acts on a Storage selection.
 *
 * It reports what each verb would do to *this* selection rather than offering
 * both unconditionally: a Delete that quietly skipped the ineligible rows would
 * be a different action than the one the button named. So a partially blocked
 * verb is disabled and says how many rows blocked it, and the rows themselves
 * carry the individual reasons.
 */
export function SelectionBar({
  summary,
  busy,
  onClear,
  onReclaim,
  onDelete,
}: {
  summary: SelectionSummary;
  busy: boolean;
  onClear: () => void;
  onReclaim: () => void;
  onDelete: () => void;
}) {
  if (summary.count === 0) return null;

  const canReclaim = summary.reclaimBlockedReason === "" && summary.reclaimable.length > 0;
  const canDelete = summary.deleteBlockedReason === "" && summary.deletable.length > 0;

  return (
    <div className="sticky bottom-0 z-10 -mx-4 mt-2 border-t bg-background/95 px-4 py-2.5 backdrop-blur supports-[backdrop-filter]:bg-background/80">
      <div className="mx-auto flex max-w-4xl flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={onClear}
          disabled={busy}
          className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50"
          title="Clear selection"
        >
          <X className="size-3.5" />
        </button>
        <span className="text-sm tabular-nums">
          {summary.count} selected
          <span className="text-muted-foreground"> · {formatBytes(summary.bytes)}</span>
        </span>

        {(summary.reclaimBlockedReason || summary.deleteBlockedReason) && (
          <span className="text-xs text-muted-foreground">
            {summary.deleteBlockedReason || summary.reclaimBlockedReason}
          </span>
        )}

        <div className="ml-auto flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={onReclaim}
            disabled={busy || !canReclaim}
            title={summary.reclaimBlockedReason || "Free the disk, keep the sessions"}
          >
            {busy ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <RotateCcw className="size-3.5" />
            )}
            Reclaim
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="text-destructive hover:text-destructive"
            onClick={onDelete}
            disabled={busy || !canDelete}
            title={summary.deleteBlockedReason || "Remove the sessions, branches and worktrees"}
          >
            <Trash2 className="size-3.5" />
            Delete
          </Button>
        </div>
      </div>
    </div>
  );
}
