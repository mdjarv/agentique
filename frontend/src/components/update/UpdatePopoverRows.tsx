/**
 * This machine's waiting upgrades, at popover density (docs/upgrades.md).
 *
 * The same subject as the Versions dialog, rendered smaller — the precedent is
 * `AgentFlightStrip`, which draws one set of runs at three densities. What is
 * NOT duplicated is the deciding: `sourceVerdict` remains the one closed union
 * that says what the checkout wants, and the store actions are the same ones
 * the dialog calls. Only the presentation is local.
 *
 * Scope is deliberately the local machine. A fleet is the dialog's job — it has
 * room for a row per machine and this has room for a verb.
 *
 * Every rule the dialog obeys still holds here:
 *   - Never offer a button that cannot work.
 *   - A restart is not a pause: the cost of a badly-timed one is the current
 *     TURN, and the override says so before it is taken.
 *   - Cancel is real until something is installed, and then it goes.
 */
import { ArrowUpCircle, GitBranch, Loader2, X } from "lucide-react";
import { useState } from "react";
import { Button } from "~/components/ui/button";
import type { UpdateStatus } from "~/lib/generated-types";
import type { UpdateKind } from "~/lib/update-api";
import { PRIMARY_MACHINE_KEY } from "~/lib/update-api";
import { sourceVerdict } from "~/lib/update-source";
import { cn, getErrorMessage } from "~/lib/utils";
import type { Flight } from "~/stores/update-store";
import { useUpdateStore } from "~/stores/update-store";

/** One thing this machine could do about its version. */
interface Waiting {
  kind: UpdateKind;
  label: string;
  detail?: string;
  action: string;
  icon: typeof GitBranch;
}

/**
 * What the local machine is waiting on, in the order it should be offered.
 *
 * A release and a moved checkout are different claims with different costs, so
 * both appear when both hold — neither hides the other.
 */
function waitingFor(status: UpdateStatus | undefined): Waiting[] {
  if (!status) return [];
  const out: Waiting[] = [];

  if (status.behind && status.installable) {
    out.push({
      kind: "release",
      label: `${status.latest} available`,
      detail: status.current,
      action: "Upgrade",
      icon: ArrowUpCircle,
    });
  }

  const source = sourceVerdict(status.source);
  if (source.action) {
    out.push({
      kind: source.action.kind,
      label: source.text,
      detail: source.detail,
      action: source.action.kind === "restart" ? "Restart" : "Rebuild",
      icon: GitBranch,
    });
  }
  return out;
}

export function UpdatePopoverRows() {
  const status = useUpdateStore((s) => s.statuses[PRIMARY_MACHINE_KEY]);
  const flight = useUpdateStore((s) => s.flights[PRIMARY_MACHINE_KEY]);

  const waiting = waitingFor(status);
  // A flight outlives the verdict that started it — the row must keep
  // narrating after `behind` flips false, or a finished upgrade vanishes
  // mid-sentence.
  if (waiting.length === 0 && !flight) return null;

  return (
    <div className="flex flex-col">
      <div className="px-3 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground-faint">
        Waiting
      </div>
      {flight ? (
        <FlightRow flight={flight} />
      ) : (
        waiting.map((w) => <WaitingRow key={w.kind} waiting={w} status={status} />)
      )}
      <div className="mx-2 my-1 h-px bg-border/60" />
    </div>
  );
}

function WaitingRow({ waiting, status }: { waiting: Waiting; status?: UpdateStatus }) {
  const apply = useUpdateStore((s) => s.apply);
  const cancel = useUpdateStore((s) => s.cancel);
  const [error, setError] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);
  const [confirmingOverride, setConfirmingOverride] = useState(false);
  const Icon = waiting.icon;

  const start = async (opts: { force?: boolean; whenIdle?: boolean }) => {
    setError(null);
    setStarting(true);
    try {
      await apply(PRIMARY_MACHINE_KEY, { ...opts, kind: waiting.kind });
      setConfirmingOverride(false);
    } catch (err) {
      setError(getErrorMessage(err, "Could not start"));
    } finally {
      setStarting(false);
    }
  };

  const busy = Boolean(status?.busy);
  const turns = status?.busyTurns === 1 ? "1 turn" : `${status?.busyTurns ?? 0} turns`;

  return (
    <div className="flex flex-col gap-1 px-3 py-1.5">
      <div className="flex items-center gap-2">
        <Icon className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[11.5px] text-foreground">{waiting.label}</span>
          {waiting.detail && (
            <span className="block truncate font-mono text-[9.5px] text-muted-foreground-faint">
              {waiting.detail}
            </span>
          )}
        </span>

        {/* Already armed: say what it waits for, and offer the way out. */}
        {status?.armed ? (
          <span className="flex shrink-0 items-center gap-1">
            <span className="text-[10px] text-muted-foreground">when idle</span>
            <button
              type="button"
              onClick={() => void cancel(PRIMARY_MACHINE_KEY)}
              aria-label="Cancel the armed upgrade"
              className="cursor-pointer rounded p-0.5 text-muted-foreground transition-colors hover:bg-muted/50 hover:text-foreground"
            >
              <X className="size-3" />
            </button>
          </span>
        ) : confirmingOverride ? (
          <Button
            size="sm"
            variant="destructive"
            disabled={starting}
            onClick={() => void start({ force: true })}
            className="h-6 shrink-0 px-2 text-[10px]"
          >
            End {turns}
          </Button>
        ) : (
          <Button
            size="sm"
            disabled={starting}
            onClick={() => void start(busy ? { whenIdle: true } : {})}
            className="h-6 shrink-0 px-2 text-[10px]"
          >
            {starting ? "…" : busy ? "When idle" : waiting.action}
          </Button>
        )}
      </div>

      {/* The cost of a badly-timed restart is the current turn, and the
          override is a second, deliberate click that states it. */}
      {busy && !status?.armed && (
        <span className="pl-[22px] text-[9.5px] leading-snug text-muted-foreground-faint">
          {confirmingOverride ? (
            <>{turns} will be terminated. Sessions survive; the work in flight does not.</>
          ) : (
            <>
              {turns} running ·{" "}
              <button
                type="button"
                onClick={() => setConfirmingOverride(true)}
                className="cursor-pointer underline-offset-2 hover:underline"
              >
                do it now
              </button>
            </>
          )}
        </span>
      )}
      {error && <span className="pl-[22px] text-[9.5px] text-destructive">{error}</span>}
    </div>
  );
}

/** An upgrade in flight narrates here rather than sending you to the dialog. */
function FlightRow({ flight }: { flight: Flight }) {
  const cancel = useUpdateStore((s) => s.cancel);
  const clearFlight = useUpdateStore((s) => s.clearFlight);
  const { progress, clientPhase, foundVersion } = flight;

  if (clientPhase === "confirmed") {
    return (
      <div className="px-3 py-1.5 text-[11.5px] font-medium text-success">
        now on {foundVersion}
      </div>
    );
  }
  if (clientPhase === "unconfirmed") {
    return (
      <div className="px-3 py-1.5 text-[11.5px] text-warning">
        still on {foundVersion} — check the service
      </div>
    );
  }
  if (clientPhase === "reconnecting") {
    return (
      <div className="flex items-center gap-2 px-3 py-1.5 text-[11.5px] text-muted-foreground">
        <Loader2 className="size-3 animate-spin motion-reduce:animate-none" />
        restarting…
      </div>
    );
  }
  if (!progress) return null;

  const terminal = progress.phase === "failed" || progress.phase === "cancelled";
  if (terminal) {
    const failed = progress.phase === "failed";
    return (
      <div className="flex items-start gap-2 px-3 py-1.5">
        <span
          className={cn(
            "min-w-0 flex-1 text-[10.5px] leading-snug",
            failed ? "text-destructive" : "text-muted-foreground",
          )}
        >
          {failed ? progress.error || "failed" : "cancelled"}
        </span>
        <button
          type="button"
          onClick={() => clearFlight(PRIMARY_MACHINE_KEY)}
          aria-label="Dismiss"
          className="shrink-0 cursor-pointer rounded p-0.5 text-muted-foreground transition-colors hover:bg-muted/50 hover:text-foreground"
        >
          <X className="size-3" />
        </button>
      </div>
    );
  }

  return (
    <div className="flex items-center gap-2 px-3 py-1.5">
      <Loader2 className="size-3 shrink-0 animate-spin text-muted-foreground motion-reduce:animate-none" />
      <span className="min-w-0 flex-1 truncate text-[11px] text-muted-foreground">
        {phaseLabel(progress.phase)}
      </span>
      {progress.cancellable && (
        <button
          type="button"
          onClick={() => void cancel(PRIMARY_MACHINE_KEY)}
          aria-label="Cancel"
          title="Cancel — nothing has been installed yet"
          className="shrink-0 cursor-pointer rounded p-0.5 text-muted-foreground transition-colors hover:bg-muted/50 hover:text-foreground"
        >
          <X className="size-3" />
        </button>
      )}
    </div>
  );
}

function phaseLabel(phase: string): string {
  switch (phase) {
    case "queued":
      return "starting…";
    case "downloading":
      return "downloading…";
    case "verifying":
      return "verifying…";
    case "building":
      return "building…";
    case "waiting-idle":
      return "built — waiting for idle";
    case "replacing":
      return "installing — no going back";
    case "restarting":
      return "restarting…";
    default:
      // An unrecognised phase is a peer newer than this client: render it as
      // activity rather than rejecting it.
      return phase;
  }
}
