/**
 * A machine's local checkout, under its version row (docs/upgrades.md).
 *
 * The release row answers "is there a newer tag published". This one answers
 * the question a machine someone develops on actually has: is the server
 * running what I wrote. They are different claims with different costs, so they
 * get their own lines and neither hides the other.
 *
 * The rules are the release row's rules, unchanged:
 *   - Never offer a button that cannot work. A dirty checkout, a checkout on
 *     another branch and a missing toolchain all say what they are instead.
 *   - A restart is not a pause: the cost of a badly-timed one is the current
 *     TURN, and the override has to say so.
 */
import { GitBranch, Loader2, X } from "lucide-react";
import { useState } from "react";
import { Button } from "~/components/ui/button";
import type { UpdateStatus } from "~/lib/generated-types";
import type { UpdateKind } from "~/lib/update-api";
import { sourceVerdict } from "~/lib/update-source";
import { cn, getErrorMessage } from "~/lib/utils";
import type { Flight } from "~/stores/update-store";

/** How much of the build log the row shows. The server sends a short tail
 *  already; this is what fits without turning a row into a terminal. */
const LOG_LINES = 3;

export function SourceRow({
  status,
  flight,
  online,
  onApply,
  onCancel,
}: {
  status?: UpdateStatus;
  flight?: Flight;
  online: boolean;
  onApply: (opts: { force?: boolean; whenIdle?: boolean; kind: UpdateKind }) => Promise<void>;
  onCancel: () => Promise<void>;
}) {
  const [error, setError] = useState<string | null>(null);
  const [confirmingOverride, setConfirmingOverride] = useState(false);
  const [starting, setStarting] = useState(false);

  const source = status?.source;
  const verdict = sourceVerdict(source);
  if (verdict.token === "off") return null;

  // A build in flight belongs to this row: it is the only one that can start
  // one, and the release row is already narrating its own.
  const building = flight?.progress?.kind === "source" || flight?.progress?.kind === "restart";

  return (
    <div className="flex items-start gap-3 border-t border-border/40 bg-secondary/20 px-3.5 py-2.5">
      <span className="flex size-8 shrink-0 items-center justify-center text-muted-foreground-faint">
        <GitBranch className="size-3.5" />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span
          className={cn(
            "truncate text-[12px]",
            verdict.attention ? "font-medium text-foreground" : "text-muted-foreground",
          )}
        >
          {verdict.text}
        </span>
        {verdict.detail ? (
          <span className="truncate font-mono text-[10.5px] text-muted-foreground-faint">
            {verdict.detail}
          </span>
        ) : null}
        {building && flight?.progress?.log?.length ? (
          <pre className="mt-1 overflow-x-auto whitespace-pre rounded bg-background/60 px-2 py-1 font-mono text-[10px] leading-[1.5] text-muted-foreground-faint">
            {flight.progress.log.slice(-LOG_LINES).join("\n")}
          </pre>
        ) : null}
        {error ? <span className="truncate text-[10.5px] text-destructive">{error}</span> : null}
      </div>

      <span className="flex min-w-0 max-w-[52%] shrink-0 justify-end">
        {building ? (
          <BuildFlight flight={flight} onCancel={() => void onCancel()} />
        ) : !online || !verdict.action ? null : (
          <SourceAction
            status={status}
            kind={verdict.action.kind}
            label={verdict.action.label}
            starting={starting}
            confirmingOverride={confirmingOverride}
            onConfirmOverride={() => setConfirmingOverride(true)}
            onStart={async (opts) => {
              setError(null);
              setStarting(true);
              try {
                await onApply({ ...opts, kind: verdict.action?.kind ?? "source" });
                setConfirmingOverride(false);
              } catch (err) {
                setError(getErrorMessage(err, "Could not start"));
              } finally {
                setStarting(false);
              }
            }}
          />
        )}
      </span>
    </div>
  );
}

/** The drain gate, told in this row's words. Identical rules to the release
 *  row: wait for idle by default, override on a second deliberate click that
 *  states its cost in turns. */
function SourceAction({
  status,
  kind,
  label,
  starting,
  confirmingOverride,
  onConfirmOverride,
  onStart,
}: {
  status?: UpdateStatus;
  kind: UpdateKind;
  label: string;
  starting: boolean;
  confirmingOverride: boolean;
  onConfirmOverride: () => void;
  onStart: (opts: { force?: boolean; whenIdle?: boolean }) => Promise<void>;
}) {
  if (!status) return null;

  if (status.busy) {
    const turns = status.busyTurns === 1 ? "1 turn" : `${status.busyTurns} turns`;
    return (
      <span className="flex min-w-0 flex-col items-end gap-1">
        {confirmingOverride ? (
          <Button
            size="sm"
            variant="destructive"
            disabled={starting}
            onClick={() => void onStart({ force: true })}
          >
            End {turns} and {kind === "restart" ? "restart" : "rebuild"}
          </Button>
        ) : (
          <span className="flex items-center gap-1.5">
            <Button size="sm" disabled={starting} onClick={() => void onStart({ whenIdle: true })}>
              {kind === "restart" ? "Restart when idle" : "Build when idle"}
            </Button>
            <button
              type="button"
              onClick={onConfirmOverride}
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
      </span>
    );
  }

  return (
    <Button size="sm" disabled={starting} onClick={() => void onStart({})}>
      {starting ? "Starting…" : label}
    </Button>
  );
}

/** A build narrates with a phase and a log tail, where a download narrates with
 *  bytes: there is no total to count against, and "is it hung?" is asked just
 *  as often — usually while npm is doing something silent. */
function BuildFlight({ flight, onCancel }: { flight?: Flight; onCancel: () => void }) {
  const progress = flight?.progress;
  if (flight?.clientPhase === "confirmed") {
    return (
      <span className="truncate text-[11.5px] font-medium text-success">
        now on {flight.foundVersion}
      </span>
    );
  }
  if (flight?.clientPhase === "unconfirmed") {
    return (
      <span className="truncate text-[11.5px] text-warning">
        still on {flight.foundVersion} — check the service
      </span>
    );
  }
  if (flight?.clientPhase === "reconnecting") {
    return (
      <span className="flex items-center gap-1.5 text-[11.5px] text-muted-foreground">
        <Loader2 className="size-3 animate-spin motion-reduce:animate-none" />
        restarting…
      </span>
    );
  }
  if (!progress) return null;

  if (progress.phase === "failed" || progress.phase === "cancelled") {
    const failed = progress.phase === "failed";
    return (
      <span
        className={cn(
          "min-w-0 truncate text-[11.5px]",
          failed ? "text-destructive" : "text-muted-foreground",
        )}
        title={progress.error || progress.phase}
      >
        {failed ? progress.error || "build failed" : "cancelled"}
      </span>
    );
  }

  return (
    <span className="flex min-w-0 items-center gap-2">
      <span className="flex min-w-0 items-center gap-1.5 truncate text-[11.5px] text-muted-foreground">
        <Loader2 className="size-3 animate-spin motion-reduce:animate-none" />
        {buildPhaseLabel(progress.phase)}
      </span>
      {progress.cancellable ? (
        <button
          type="button"
          onClick={onCancel}
          title="Cancel — nothing has been installed yet"
          aria-label="Cancel build"
          className="cursor-pointer rounded-md p-0.5 text-muted-foreground transition-colors hover:bg-muted/50 hover:text-foreground"
        >
          <X className="size-3.5" />
        </button>
      ) : null}
    </span>
  );
}

function buildPhaseLabel(phase: string): string {
  switch (phase) {
    case "queued":
      return "starting…";
    case "building":
      return "building…";
    case "waiting-idle":
      return "built — waiting for idle";
    case "replacing":
      return "installing — no going back";
    case "restarting":
      return "restarting…";
    default:
      // An unrecognised phase is a peer newer than this client. Render it as
      // activity rather than rejecting it: the wire rule is that a client
      // accepts what it does not know (docs/upgrades.md).
      return phase;
  }
}
