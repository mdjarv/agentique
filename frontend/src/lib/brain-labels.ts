import type { Memory } from "~/lib/brain-api";

// Display vocabulary for the Band-1 controlled-vocabulary labels, shared by the memory
// list (BrainPage) and the review surface (MemoryReview). Centralizes the value→label
// map so there are no inline magic strings/colours (brain-ui-spec.md F1).
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

// isCapture reports whether a memory is a raw capture (never injected, awaiting churn
// promotion) — the ingest tier, badged distinctly from durable facts.
export function isCapture(memory: Memory): boolean {
  return memory.source === "capture";
}
