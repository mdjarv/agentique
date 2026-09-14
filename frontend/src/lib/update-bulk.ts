/**
 * "Upgrade all": which machines one click starts, and how (docs/upgrades.md,
 * decision U3).
 *
 * Every step is the action that machine's own row already offers, so the bulk
 * button can never do something no row would: release when the row offers a
 * release, else the checkout's rebuild or restart. Nothing here decides a new
 * verdict — it reads `status.behind`/`installable` and `sourceVerdict`, the
 * same facts the rows read.
 *
 * Two rules are the drain gate's, applied to a fleet:
 *   - A busy machine is ARMED (`whenIdle`), never forced. Ending turns is a
 *     per-machine second click that states its cost, and a bulk button that
 *     could end turns on five machines at once would say none of it.
 *   - The primary goes last. It serves this page and the machine catalog, so
 *     its restart must not land while the requests to the others are still
 *     going out.
 */

import type { UpdateStatus } from "~/lib/generated-types";
import { PRIMARY_MACHINE_KEY, type UpdateKind } from "~/lib/update-api";
import { sourceVerdict } from "~/lib/update-source";

/** What the planner needs to know about one machine. */
export interface BulkCandidate {
  key: string;
  label: string;
  online: boolean;
  status?: UpdateStatus;
  /** An upgrade already running there — its row is busy narrating it. */
  inFlight: boolean;
}

export interface BulkStep {
  key: string;
  label: string;
  /** Absent means release, matching `useUpdateStore.apply`. */
  kind?: UpdateKind;
  /** True for a machine with turns running: it upgrades when idle. */
  whenIdle: boolean;
}

/** The action one machine's row offers right now, or null when it offers none. */
function stepFor(c: BulkCandidate): BulkStep | null {
  const status = c.status;
  if (!c.online || c.inFlight || !status || status.armed) return null;
  const whenIdle = Boolean(status.busy);

  if (status.behind && status.installable) {
    return { key: c.key, label: c.label, whenIdle };
  }
  const action = sourceVerdict(status.source).action;
  if (action) return { key: c.key, label: c.label, kind: action.kind, whenIdle };
  return null;
}

/**
 * The steps "Upgrade all" would run, remotes first in the order given and the
 * primary last. The button is offered only when this has two or more steps: one
 * machine already has its own button, and a second control for it is the same
 * offer twice.
 */
export function planUpgradeAll(candidates: BulkCandidate[]): BulkStep[] {
  const steps = candidates.map(stepFor).filter((s): s is BulkStep => s !== null);
  const remotes = steps.filter((s) => s.key !== PRIMARY_MACHINE_KEY);
  const primary = steps.filter((s) => s.key === PRIMARY_MACHINE_KEY);
  return [...remotes, ...primary];
}

/** Run a plan: every remote at once, then the primary. Resolves with each
 *  machine that refused, keyed by label, so one refusal never hides the rest. */
export async function runUpgradeAll(
  steps: BulkStep[],
  apply: (key: string, opts: { kind?: UpdateKind; whenIdle?: boolean }) => Promise<void>,
): Promise<{ label: string; error: unknown }[]> {
  const failures: { label: string; error: unknown }[] = [];
  const run = async (step: BulkStep) => {
    try {
      await apply(step.key, {
        ...(step.kind ? { kind: step.kind } : {}),
        ...(step.whenIdle ? { whenIdle: true } : {}),
      });
    } catch (error) {
      failures.push({ label: step.label, error });
    }
  };

  const remotes = steps.filter((s) => s.key !== PRIMARY_MACHINE_KEY);
  await Promise.all(remotes.map(run));
  for (const step of steps.filter((s) => s.key === PRIMARY_MACHINE_KEY)) {
    await run(step);
  }
  return failures;
}
