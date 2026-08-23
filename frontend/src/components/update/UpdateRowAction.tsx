/**
 * The right-hand end of a machine's row: what it needs, or what it is doing
 * about it (docs/upgrades.md).
 *
 * Three rules shape everything here:
 *   - Never offer a button that cannot work. An unverified platform, an
 *     unwritable install dir and a machine that is away all say what they are
 *     instead of offering an action.
 *   - The cost of a badly-timed restart is the current TURN, not the session,
 *     and the override has to say so — "will this lose my work" is the
 *     question that stops someone clicking.
 *   - Cancel is real through verification and then disappears. A control that
 *     stays visible and quietly stops working is worse than no control.
 */
import { Loader2, X } from "lucide-react";
import { useState } from "react";
import { Button } from "~/components/ui/button";
import type { UpdateProgress, UpdateStatus } from "~/lib/generated-types";
import { cn, formatBytes, getErrorMessage } from "~/lib/utils";
import type { Flight } from "~/stores/update-store";

/** A byte counter only earns its place on a download big or slow enough that
 *  "is it hung?" is a real question. 33 MB on a fast link is over first. */
const BYTES_THRESHOLD = 4 * 1024 * 1024;

function phaseLabel(progress: UpdateProgress): string {
  switch (progress.phase) {
    case "queued":
      return "starting…";
    case "downloading":
      if (progress.total > BYTES_THRESHOLD && progress.downloaded > 0) {
        return `downloading ${formatBytes(progress.downloaded)} / ${formatBytes(progress.total)}`;
      }
      return "downloading…";
    case "verifying":
      return "verifying…";
    case "replacing":
      return "installing — no going back";
    case "restarting":
      return "restarting…";
    case "cancelled":
      return "cancelled";
    case "failed":
      return "failed";
    default:
      return progress.phase;
  }
}

/** The narration for a flight, and whether cancel is still real. */
function FlightState({
  flight,
  onCancel,
  onDismiss,
}: {
  flight: Flight;
  onCancel: () => void;
  onDismiss: () => void;
}) {
  const { progress, clientPhase, foundVersion } = flight;

  if (clientPhase === "confirmed") {
    return (
      <span className="truncate text-[11.5px] font-medium text-success">now on {foundVersion}</span>
    );
  }
  if (clientPhase === "unconfirmed") {
    // Report the version actually found, never the one we hoped for.
    return (
      <span className="truncate text-[11.5px] text-warning">
        still on {foundVersion} — check the service
      </span>
    );
  }
  if (clientPhase === "reconnecting") {
    // A dropped socket on THIS one command means it worked.
    return (
      <span className="flex items-center gap-1.5 text-[11.5px] text-muted-foreground">
        <Loader2 className="size-3 animate-spin motion-reduce:animate-none" />
        restarting…
      </span>
    );
  }
  if (!progress) return null;

  // Terminal, and not what anyone wanted: say why in one line the row can
  // hold (the full text is in the tooltip), and let it be dismissed so the row
  // goes back to offering the upgrade.
  if (progress.phase === "failed" || progress.phase === "cancelled") {
    const failed = progress.phase === "failed";
    return (
      <span className="flex min-w-0 items-center gap-1.5">
        <span
          className={cn(
            "min-w-0 truncate text-[11.5px]",
            failed ? "text-destructive" : "text-muted-foreground",
          )}
          title={progress.error || progress.phase}
        >
          {failed ? progress.error || "failed" : "cancelled"}
        </span>
        <button
          type="button"
          onClick={onDismiss}
          aria-label="Dismiss"
          title="Dismiss"
          className="shrink-0 cursor-pointer rounded-md p-0.5 text-muted-foreground transition-colors hover:bg-muted/50 hover:text-foreground"
        >
          <X className="size-3.5" />
        </button>
      </span>
    );
  }

  return (
    <span className="flex min-w-0 items-center gap-2">
      <span className="flex min-w-0 items-center gap-1.5 truncate text-[11.5px] text-muted-foreground">
        <Loader2 className="size-3 animate-spin motion-reduce:animate-none" />
        {phaseLabel(progress)}
      </span>
      {progress.cancellable && (
        <button
          type="button"
          onClick={onCancel}
          title="Cancel — nothing has been installed yet"
          aria-label="Cancel upgrade"
          className="cursor-pointer rounded-md p-0.5 text-muted-foreground transition-colors hover:bg-muted/50 hover:text-foreground"
        >
          <X className="size-3.5" />
        </button>
      )}
    </span>
  );
}

export function UpdateRowAction({
  status,
  flight,
  online,
  verdict,
  onApply,
  onCancel,
  onDismiss,
}: {
  status?: UpdateStatus;
  flight?: Flight;
  online: boolean;
  /** The row's plain-language state, shown when there is nothing to do. */
  verdict: { text: string; strong: boolean };
  onApply: (opts: { force?: boolean; whenIdle?: boolean }) => Promise<void>;
  onCancel: () => Promise<void>;
  /** Forget a finished flight, so the row goes back to offering the upgrade. */
  onDismiss: () => void;
}) {
  const [error, setError] = useState<string | null>(null);
  const [confirmingOverride, setConfirmingOverride] = useState(false);
  const [starting, setStarting] = useState(false);

  if (flight) {
    return <FlightState flight={flight} onCancel={() => void onCancel()} onDismiss={onDismiss} />;
  }

  const actionable = online && status?.behind && status.installable;
  if (!actionable) {
    return (
      <span
        className={cn(
          "min-w-0 truncate text-[11.5px]",
          verdict.strong ? "font-medium text-foreground-bright" : "text-muted-foreground",
        )}
        title={status?.blocker}
      >
        {verdict.text}
      </span>
    );
  }

  const start = async (opts: { force?: boolean; whenIdle?: boolean }) => {
    setError(null);
    setStarting(true);
    try {
      await onApply(opts);
      setConfirmingOverride(false);
    } catch (err) {
      setError(getErrorMessage(err, "Upgrade failed to start"));
    } finally {
      setStarting(false);
    }
  };

  // Already waiting for idle: say what it is waiting for and offer the way out.
  if (status.armed) {
    return (
      <span className="flex min-w-0 flex-col items-end gap-1">
        <span className="flex items-center gap-1.5">
          <span className="truncate text-[11.5px] text-muted-foreground">
            {status.busy ? "upgrades when idle" : "waiting to upgrade"}
          </span>
          <button
            type="button"
            onClick={() => void onCancel()}
            aria-label="Cancel the armed upgrade"
            title="Cancel — nothing has been installed"
            className="shrink-0 cursor-pointer rounded-md p-0.5 text-muted-foreground transition-colors hover:bg-muted/50 hover:text-foreground"
          >
            <X className="size-3.5" />
          </button>
        </span>
        <span className="truncate text-[10.5px] text-muted-foreground-faint">
          {status.armed.target} · until {shortTime(status.armed.deadlineAt)}
        </span>
      </span>
    );
  }

  // Busy: a restart is not a pause. Waiting for idle is the default offer; the
  // override is a second, deliberate click that states its cost in turns.
  if (status.busy) {
    const turns = status.busyTurns === 1 ? "1 turn" : `${status.busyTurns} turns`;
    return (
      <span className="flex min-w-0 flex-col items-end gap-1">
        {confirmingOverride ? (
          <Button
            size="sm"
            variant="destructive"
            disabled={starting}
            onClick={() => void start({ force: true })}
          >
            End {turns} and upgrade
          </Button>
        ) : (
          <span className="flex items-center gap-1.5">
            <Button size="sm" disabled={starting} onClick={() => void start({ whenIdle: true })}>
              Upgrade when idle
            </Button>
            <button
              type="button"
              onClick={() => setConfirmingOverride(true)}
              className="cursor-pointer text-[10.5px] text-muted-foreground underline-offset-2 hover:underline"
            >
              now
            </button>
          </span>
        )}
        <span className="truncate text-right text-[10.5px] text-muted-foreground-faint">
          {confirmingOverride
            ? `${turns} will be terminated. Sessions survive; the work in flight does not.`
            : `${turns} running here`}
        </span>
        {error && <span className="truncate text-[10.5px] text-destructive">{error}</span>}
      </span>
    );
  }

  return (
    <span className="flex min-w-0 flex-col items-end gap-1">
      <Button size="sm" disabled={starting} onClick={() => void start({})}>
        {starting ? "Starting…" : `Upgrade to ${status.latest}`}
      </Button>
      {error && <span className="truncate text-[10.5px] text-destructive">{error}</span>}
    </span>
  );
}

/** A deadline, in the least words that still say when. */
function shortTime(iso: string): string {
  const at = Date.parse(iso);
  if (Number.isNaN(at)) return iso;
  const sameDay = new Date(at).toDateString() === new Date().toDateString();
  return new Date(at).toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    ...(sameDay ? {} : { month: "short", day: "numeric" }),
  });
}
