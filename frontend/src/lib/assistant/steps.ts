/**
 * A turn's working: the verbs the head called and the reasoning it did, in
 * order (`backend/internal/assistant/steps.go`).
 *
 * The server records a step where the work happens — a verb at the one door
 * every head call passes, a thought from the runtime — and pushes it twice for a
 * verb, running then settled, naming it by `seq`. The finished list rides the
 * message the turn ends. Everything here is pure, so the rules about what a
 * step list says are tested without a component.
 *
 * Kinds and statuses are `string` on the wire: a peer one release ahead can
 * send one this build has never heard of, and such a step still renders as a
 * plain row rather than being dropped.
 */
import type { AssistantStep } from "~/lib/assistant/wire";

export const STEP_KIND_VERB = "verb";
export const STEP_KIND_THOUGHT = "thought";

export const STEP_STATUS_RUNNING = "running";
export const STEP_STATUS_REFUSED = "refused";
export const STEP_STATUS_FAILED = "failed";

/**
 * Merges one pushed step into a turn's list by `seq`, in order.
 *
 * A settled verb replaces its running self. A step with no `seq` cannot be
 * placed, so it is ignored — the stored message carries the whole list anyway,
 * which is what a missed or unplaceable push costs: a live row, never the record.
 * Returns the held reference when nothing changed.
 */
export function mergeStep(held: AssistantStep[], step: AssistantStep): AssistantStep[] {
  const seq = step.seq;
  if (!seq || seq < 1) return held;
  const at = held.findIndex((s) => s.seq === seq);
  if (at === -1) {
    return [...held, step].sort((a, b) => (a.seq ?? 0) - (b.seq ?? 0));
  }
  if (JSON.stringify(held[at]) === JSON.stringify(step)) return held;
  const next = held.slice();
  next[at] = step;
  return next;
}

/**
 * The collapsed line's words: "2 steps, 1 thought".
 *
 * The session transcript's activity line counts tool calls and thoughts the
 * same way, so the two surfaces read alike. Steps the server did not keep are
 * counted in, because the line says how much the turn did.
 */
export function stepsTitle(steps: AssistantStep[], omitted = 0): string {
  let thoughts = 0;
  let verbs = omitted;
  for (const step of steps) {
    if (step.kind === STEP_KIND_THOUGHT) thoughts++;
    else verbs++;
  }
  const parts: string[] = [];
  if (verbs > 0) parts.push(`${verbs} ${verbs === 1 ? "step" : "steps"}`);
  if (thoughts > 0) parts.push(`${thoughts} ${thoughts === 1 ? "thought" : "thoughts"}`);
  return parts.join(", ") || "Worked";
}

/** The verb the turn is waiting on right now, if any: the newest one running. */
export function runningStep(steps: AssistantStep[]): AssistantStep | undefined {
  for (let i = steps.length - 1; i >= 0; i--) {
    const step = steps[i];
    if (step?.kind !== STEP_KIND_THOUGHT && step?.status === STEP_STATUS_RUNNING) return step;
  }
  return undefined;
}

/** Whether a message has a turn's working to show. */
export function hasSteps(message: { steps?: AssistantStep[]; stepsOmitted?: number }): boolean {
  return (message.steps?.length ?? 0) > 0 || (message.stepsOmitted ?? 0) > 0;
}

/** Verbs that read or write memory, which wear memory's glyph on their row. */
const MEMORY_VERBS: ReadonlySet<string> = new Set([
  "recall",
  "remember",
  "confirm_memory",
  "flag_memory",
]);

export function isMemoryVerb(verb: string | undefined): boolean {
  return !!verb && MEMORY_VERBS.has(verb);
}

/** "140ms", "2.4s" — how long a verb took, or nothing when it was instant. */
export function stepDuration(ms: number | undefined): string {
  if (!ms || ms < 1) return "";
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)}s`;
}
