/**
 * The Claude scratchpads that are not agentique's, one row each.
 *
 * These are the only directories on this page agentique did not create, so the
 * verb is offered **per directory and never as a sweep**. A "clear all" button
 * would be one click that removes work belonging to checkouts this app knows
 * nothing about; naming each path and its size makes the decision the operator's
 * every time, which is the whole basis on which reporting these was worth doing.
 *
 * Sorted by size, because that is the question being answered — one directory is
 * usually most of the total, and the rest are noise. The dialog itself is the
 * confirmation surface: the path is on screen, in full, next to the button.
 */
import { ExternalLink, Loader2, Trash2 } from "lucide-react";
import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog";
import type { TempArtifact } from "~/lib/generated-types";
import { formatBytes } from "~/lib/utils";

/** Last path segment. Server paths are POSIX-cleaned before they reach here. */
const basename = (p: string) => p.slice(p.lastIndexOf("/") + 1);
/** Everything before it, which is the shared scratchpad root. */
const dirname = (p: string) => p.slice(0, p.lastIndexOf("/")) || "/";

export function ForeignScratchpadsDialog({
  open,
  onOpenChange,
  artifacts,
  onRemove,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  artifacts: TempArtifact[];
  /** Resolves once the removal has been applied and the usage refreshed. */
  onRemove: (path: string) => Promise<void>;
}) {
  const [busyPath, setBusyPath] = useState<string | null>(null);
  const rows = [...artifacts].sort((a, b) => b.bytes - a.bytes);
  const total = rows.reduce((a, r) => a + r.bytes, 0);
  // Every one of these is a direct child of the scratchpad root — the server
  // refuses anything else — so the parent is read off the first path rather
  // than carried as its own wire field.
  const root = rows[0] ? dirname(rows[0].path) : "";

  const remove = async (path: string) => {
    setBusyPath(path);
    try {
      await onRemove(path);
    } finally {
      setBusyPath(null);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Other Claude scratchpads</DialogTitle>
          <DialogDescription>
            Scratch directories under the same root as agentique's, belonging to checkouts it does
            not manage — Claude run directly in a repo rather than through a session. Agentique
            reports them but never removes them on its own.{" "}
            <span className="text-warning">
              One in use by a Claude session running right now will break if removed.
            </span>
          </DialogDescription>
        </DialogHeader>

        {rows.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground">None on this machine.</p>
        ) : (
          <>
            {/* The parent is the same for every row, so it is stated once here
                rather than repeated down a column and truncated in each.
                `min-w-0` is load-bearing on both of these: they are grid items
                of DialogContent, whose default `min-width: auto` sizes the
                track to the longest unbreakable string — one absolute path and
                the dialog grows past its own max-width, taking the Remove
                buttons off screen with it. */}
            <div className="flex min-w-0 items-baseline justify-between gap-3 text-xs text-muted-foreground">
              <span className="min-w-0 flex-1 truncate" title={root}>
                {rows.length} {rows.length === 1 ? "directory" : "directories"} in{" "}
                <span className="font-mono text-[11px]">{root}</span>
              </span>
              <span className="shrink-0 font-mono tabular-nums">{formatBytes(total)}</span>
            </div>
            <ul className="max-h-80 min-w-0 divide-y divide-border/60 overflow-y-auto rounded-md border bg-muted/20">
              {rows.map((a) => (
                <li key={a.path} className="flex items-center gap-2 px-2.5 py-1.5">
                  {/* The directory name, not the whole path: it is the mangled
                      checkout path and therefore the only part that differs
                      between rows. Left-to-right — `dir="rtl"` truncates from
                      the correct end but moves the leading separator to the
                      visual right, printing a trailing slash the path does not
                      have. A confirm that misspells what it is about to delete
                      is worse than one that needs a hover for the full string. */}
                  <span
                    className="min-w-0 flex-1 truncate font-mono text-[11px] text-foreground"
                    title={a.path}
                  >
                    {basename(a.path)}
                  </span>
                  <span className="shrink-0 font-mono text-xs tabular-nums text-muted-foreground">
                    {formatBytes(a.bytes)}
                  </span>
                  <button
                    type="button"
                    onClick={() => remove(a.path)}
                    disabled={busyPath !== null}
                    className="flex shrink-0 cursor-pointer items-center gap-1 rounded border border-border px-1.5 py-0.5 text-[11px] text-muted-foreground transition-colors hover:border-destructive/40 hover:bg-destructive/10 hover:text-destructive disabled:cursor-default disabled:opacity-50"
                    aria-label={`Remove ${a.path}`}
                  >
                    {busyPath === a.path ? (
                      <Loader2 className="size-3 animate-spin" />
                    ) : (
                      <Trash2 className="size-3" />
                    )}
                    Remove
                  </button>
                </li>
              ))}
            </ul>
            <p className="flex items-start gap-1.5 text-[11px] leading-snug text-muted-foreground-faint">
              <ExternalLink className="mt-0.5 size-3 shrink-0" />
              Removed one directory at a time on purpose. Agentique offers no sweep over directories
              it did not create.
            </p>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
