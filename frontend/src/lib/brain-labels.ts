import type { Memory } from "~/lib/brain-api";

// Display vocabulary for the Band-1 controlled-vocabulary labels, shared by the memory
// list (BrainPage) and the review surface (MemoryReview). Centralizes the value→label
// map so there are no inline magic strings/colours (brain.md#brain-ui F1).
//
// The defaults — evidence "inferred", volatility "slow" — intentionally have NO chip:
// rows are dense, so only a fact that deviates from the default earns one. Likewise
// lifecycle "active" gets no badge (it's the unremarkable common case).

export interface LabelChip {
  label: string;
  title: string;
}

const EVIDENCE_CHIPS: Record<string, LabelChip> = {
  user_stated: { label: "stated", title: "Evidence: asserted by you" },
  code_verified: { label: "✓ code", title: "Evidence: checked against live code" },
  corroborated: { label: "corroborated", title: "Evidence: independently re-observed" },
  observed_once: {
    label: "seen once",
    title: "Evidence: seen a single time, not yet promoted",
  },
  // inferred (the default for non-human facts) is deliberately omitted — no chip.
};

// evidenceChip returns the compact chip for a noteworthy evidence tier, or null for the
// default (inferred) / an unknown value.
export function evidenceChip(evidence?: string): LabelChip | null {
  if (!evidence) return null;
  return EVIDENCE_CHIPS[evidence] ?? null;
}

const VOLATILITY_CHIPS: Record<string, LabelChip> = {
  evergreen: { label: "evergreen", title: "Volatility: never erodes" },
  ephemeral: { label: "ephemeral", title: "Volatility: erodes fast — tied to a moment" },
  // slow (the default) is deliberately omitted — no chip.
};

// volatilityChip returns the compact chip for a noteworthy volatility, or null for the
// default (slow) / an unknown value.
export function volatilityChip(volatility?: string): LabelChip | null {
  if (!volatility) return null;
  return VOLATILITY_CHIPS[volatility] ?? null;
}

export interface LifecycleBadge {
  variant: "archived" | "superseded";
  label: string;
  title: string;
}

// lifecycleBadge returns the tier badge for a non-active lifecycle, or null for active.
// (capture is a Source, not a Lifecycle — a capture is active-but-raw — so it is badged
// separately off `source`.)
export function lifecycleBadge(memory: Memory): LifecycleBadge | null {
  if (memory.lifecycle === "archived") {
    return {
      variant: "archived",
      label: "archived",
      title: "Cold tier — out of recall, kept on disk, restorable",
    };
  }
  if (memory.lifecycle === "superseded") {
    return {
      variant: "superseded",
      label: "superseded",
      title: "Replaced by a newer fact",
    };
  }
  return null;
}

// STAGED_SOURCES are the sources that put a fact in the CAPTURE tier: raw episodic
// material, never recalled, waiting for the churn to judge it. There are two because
// provenance survives the door it came through — `capture` is the assistant's own
// sentence, `reported` is agent-written text about a repository nobody here authored
// (memory.Source.Staged() is the same predicate on the Go side). Module-level so the
// array is one stable reference.
export const STAGED_SOURCES: readonly string[] = ["capture", "reported"];

// isCapture reports whether a memory is in the capture tier (never recalled, awaiting
// churn promotion) — badged distinctly from durable facts. One predicate, because a
// surface that treats `reported` as durable shows an agent's claim as a settled fact.
export function isCapture(memory: Memory): boolean {
  return STAGED_SOURCES.includes(memory.source);
}

// pendingCaptures totals the capture tier from a `bySource` histogram (the shape
// /api/brain/status answers with, which counts every source separately).
export function pendingCaptures(bySource: Record<string, number>): number {
  return STAGED_SOURCES.reduce((sum, source) => sum + (bySource[source] ?? 0), 0);
}

/** The scope every project shares. */
export const GLOBAL_SCOPE = "global";

const PROJECT_SCOPE_PREFIX = "project:";

/**
 * What to call a memory scope: "Global", a project's name, or — for a project
 * this client does not hold — "Project" and the first eight of its id.
 *
 * One place, because the Memory page groups facts under these words and the
 * thread's recall rows label a fact with them, and a fact filed under one name
 * on the page cannot wear another in the conversation.
 */
export function scopeLabel(scope: string, projectName: (id: string) => string | undefined): string {
  if (scope === GLOBAL_SCOPE) return "Global";
  if (scope.startsWith(PROJECT_SCOPE_PREFIX)) {
    const id = scope.slice(PROJECT_SCOPE_PREFIX.length);
    return projectName(id) ?? `Project ${id.slice(0, 8)}`;
  }
  return scope;
}
