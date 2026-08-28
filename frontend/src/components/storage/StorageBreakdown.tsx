/**
 * The disk breakdown: one row per thing the disk is spent on, ordered and
 * coloured by what you can do about it (`lib/storage/breakdown.ts`).
 *
 * Three things carry the design, and each answers a question the old two-group
 * bar list could not:
 *
 * - **A detail line under every label.** "Worktrees" is a word for a directory,
 *   not an explanation; "one per session — removed when its session is
 *   reclaimed" is why the number is what it is and what would change it.
 * - **Colour is the verdict, not an identity.** The old bars were sky, amber,
 *   violet, emerald, rose — eight hues carrying no information, so a category
 *   nothing can remove looked exactly like one a button clears. Three inks
 *   now, and the legend names them.
 * - **The verb sits on the row that owns it.** Reclaim is offered against
 *   finished worktrees, where the bytes are, rather than only at the end of a
 *   selection made three scrolls further down.
 *
 * Rows without a verb get no button. Backups are held by a retention setting
 * and temp files go when their session does — a "clean" control on either
 * would name an action the server cannot perform.
 */
import { Loader2, RotateCcw } from "lucide-react";
import type { Breakdown, BreakdownClass } from "~/lib/storage/breakdown";
import { cn, formatBytes } from "~/lib/utils";

/** One ink per verdict. Grey reports, green offers, blue is somebody else's rule. */
const CLASS_BAR: Record<BreakdownClass, string> = {
  live: "bg-muted-foreground/45",
  sweep: "bg-success",
  policy: "bg-primary/70",
};

const LEGEND: { cls: BreakdownClass; text: string }[] = [
  { cls: "live", text: "In use — nothing to do" },
  { cls: "sweep", text: "Reclaim frees this" },
  { cls: "policy", text: "Kept by a setting" },
];

export function StorageBreakdown({
  breakdown,
  busy,
  onReclaim,
}: {
  breakdown: Breakdown;
  busy: boolean;
  onReclaim: () => void;
}) {
  const { rows, total, sweepBytes } = breakdown;
  if (rows.length === 0) return null;

  return (
    <div className="rounded-lg border bg-card/40 px-4 py-3">
      <div className="mb-3 flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
        <span className="text-xs uppercase tracking-wider text-muted-foreground">
          Where the disk went
        </span>
        <span className="font-mono text-[11px] tabular-nums text-muted-foreground-faint">
          {formatBytes(total)} measured
        </span>
      </div>

      <div className="flex flex-col gap-2.5">
        {rows.map((row) => {
          // Against the whole measured total, so the bars compare to each
          // other. A floor keeps a real-but-tiny row from drawing as nothing.
          const pct = total > 0 ? (row.bytes / total) * 100 : 0;
          return (
            <div key={row.key} className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
              <div className="min-w-0 flex-1 md:max-w-[17.5rem] md:flex-none md:basis-70">
                <span className="block truncate text-xs text-foreground">{row.label}</span>
                {/* Wraps while narrow rather than truncating: on the row that
                    also carries the Reclaim button the detail is where the
                    reassurance lives ("branches and history are kept"), and a
                    reassurance cut off mid-word is worse than a second line. */}
                <span className="block text-[11px] leading-snug text-muted-foreground-faint md:truncate">
                  {row.detail}
                </span>
              </div>

              {/* Full width on its own line while narrow — a 40px track beside
                  a label and a figure reports nothing. */}
              <div className="order-last h-1.5 w-full overflow-hidden rounded-full bg-muted md:order-none md:w-auto md:flex-1">
                <div
                  className={cn("h-full rounded-full", CLASS_BAR[row.cls])}
                  style={{ width: `${Math.max(pct, 1)}%` }}
                />
              </div>

              <span className="w-16 shrink-0 text-right font-mono text-xs tabular-nums text-foreground">
                {formatBytes(row.bytes)}
              </span>

              {row.action === "reclaim" ? (
                <button
                  type="button"
                  onClick={onReclaim}
                  disabled={busy}
                  className="flex shrink-0 cursor-pointer items-center gap-1 rounded-md border border-success/40 px-2 py-1 text-[11px] font-medium text-success transition-colors hover:bg-success/10 disabled:cursor-default disabled:opacity-50"
                  title={`Free ${formatBytes(sweepBytes)} — the sessions and their branches stay`}
                >
                  {busy ? (
                    <Loader2 className="size-3 animate-spin" />
                  ) : (
                    <RotateCcw className="size-3" />
                  )}
                  Reclaim
                </button>
              ) : (
                // Holds the column so every bar ends at the same x, whether or
                // not its row has a verb.
                <span aria-hidden className="hidden w-[5.4rem] shrink-0 md:block" />
              )}
            </div>
          );
        })}
      </div>

      <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 border-t border-border/50 pt-2.5 font-mono text-[10px] text-muted-foreground-faint">
        {LEGEND.map((l) => (
          <span key={l.cls} className="flex items-center gap-1.5">
            <span className={cn("size-2 rounded-[2px]", CLASS_BAR[l.cls])} />
            {l.text}
          </span>
        ))}
      </div>
    </div>
  );
}
