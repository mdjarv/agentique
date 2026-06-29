import { AlertTriangle, Camera, Loader2, RotateCcw } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { Button } from "~/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "~/components/ui/dialog";
import { createSnapshot, listSnapshots, restoreSnapshot, type Snapshot } from "~/lib/brain-api";
import { formatBytes, getErrorMessage, relativeTime } from "~/lib/utils";

// BrainSnapshots is the admin surface for brain snapshots (brain-ui-spec.md F4): list them,
// take one on demand, and roll the whole brain back. Restore is destructive-feeling, so it
// is guarded by an inline confirm and blocked while a consolidation job is running (the
// restore would race the churn's own writes). The server takes a safety snapshot before
// every restore and invalidates the live cache, then broadcasts brain.updated so the memory
// list refetches.
export function BrainSnapshots({
  onClose,
  jobActive,
}: {
  onClose: () => void;
  // True while a consolidation job is running — restore is disabled to avoid racing it.
  jobActive: boolean;
}) {
  const [snaps, setSnaps] = useState<Snapshot[] | null>(null);
  const [taking, setTaking] = useState(false);
  const [restoringId, setRestoringId] = useState<string | null>(null);
  const [confirmId, setConfirmId] = useState<string | null>(null);
  const busy = taking || restoringId !== null;

  const refresh = async () => {
    try {
      setSnaps(await listSnapshots());
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to load snapshots"));
    }
  };

  // biome-ignore lint/correctness/useExhaustiveDependencies: mount-only initial load; refresh is redefined each render, so depending on it would refetch in a loop.
  useEffect(() => {
    void refresh();
  }, []);

  const take = async () => {
    setTaking(true);
    try {
      await createSnapshot();
      toast.success("Snapshot taken");
      await refresh();
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to take snapshot"));
    } finally {
      setTaking(false);
    }
  };

  const restore = async (id: string) => {
    setConfirmId(null);
    setRestoringId(id);
    try {
      await restoreSnapshot(id);
      toast.success("Brain restored — a safety snapshot was taken first");
      // The brain.updated push refetches the memory list; refresh the snapshot list too
      // (the pre-restore safety snapshot is new).
      await refresh();
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to restore snapshot"));
    } finally {
      setRestoringId(null);
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex max-h-[80vh] w-[min(92vw,560px)] max-w-none flex-col gap-0 p-0 sm:max-w-none">
        <DialogHeader className="border-b px-5 py-3">
          <DialogTitle className="flex items-center gap-2 text-base">
            Snapshots
            <span className="text-sm font-normal text-muted-foreground">
              {snaps ? `${snaps.length} kept` : ""}
            </span>
            <Button
              size="sm"
              variant="outline"
              className="ml-auto"
              disabled={busy}
              onClick={take}
              title="Take a snapshot of the whole brain now (non-destructive)"
            >
              {taking ? <Loader2 className="size-4 animate-spin" /> : <Camera className="size-4" />}
              Take snapshot
            </Button>
          </DialogTitle>
        </DialogHeader>

        {jobActive && (
          <div className="flex items-center gap-2 border-b bg-amber-500/10 px-5 py-2 text-xs text-amber-600 dark:text-amber-400">
            <AlertTriangle className="size-3.5 shrink-0" />A consolidation is running — restore is
            paused until it finishes.
          </div>
        )}

        <div className="min-h-0 flex-1 overflow-y-auto px-3 py-2">
          {snaps === null ? (
            <div className="flex items-center justify-center gap-2 py-10 text-sm text-muted-foreground">
              <Loader2 className="size-4 animate-spin" /> Loading…
            </div>
          ) : snaps.length === 0 ? (
            <div className="py-10 text-center text-sm text-muted-foreground">
              No snapshots yet. Take one before a risky change, or one is taken automatically before
              each consolidation.
            </div>
          ) : (
            <ul className="space-y-1.5">
              {snaps.map((s) => (
                <li key={s.id} className="rounded-md border bg-card/50 px-3 py-2">
                  {confirmId === s.id ? (
                    <div className="flex flex-col gap-2">
                      <div className="text-xs text-foreground">
                        This rolls the <span className="font-medium">entire brain</span> back to{" "}
                        {relativeTime(s.createdAt)}. A safety snapshot is taken first.
                      </div>
                      <div className="flex items-center gap-2">
                        <Button
                          size="sm"
                          variant="destructive"
                          disabled={busy}
                          onClick={() => restore(s.id)}
                        >
                          Restore
                        </Button>
                        <Button size="sm" variant="ghost" onClick={() => setConfirmId(null)}>
                          Cancel
                        </Button>
                      </div>
                    </div>
                  ) : (
                    <div className="flex items-center gap-3">
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm tabular-nums">
                          {relativeTime(s.createdAt)}
                        </div>
                        <div className="text-[11px] text-muted-foreground tabular-nums">
                          {s.files} file{s.files === 1 ? "" : "s"} · {formatBytes(s.bytes)} · {s.id}
                        </div>
                      </div>
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={busy || jobActive}
                        onClick={() => setConfirmId(s.id)}
                        title={
                          jobActive
                            ? "Can't restore while a consolidation is running"
                            : "Roll the entire brain back to this snapshot"
                        }
                      >
                        {restoringId === s.id ? (
                          <Loader2 className="size-3.5 animate-spin" />
                        ) : (
                          <RotateCcw className="size-3.5" />
                        )}
                        Restore
                      </Button>
                    </div>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
