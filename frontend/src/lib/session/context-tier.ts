/**
 * How full a session's context window is, and how loudly to say so.
 *
 * Two surfaces read this — the desktop's `ContextBar` row and the phone's
 * `ContextEdge`, the 2px line along the top of the composer — and they must
 * never disagree about what 84% looks like. Same argument as `REST_GLYPH`: one
 * table, every surface reads it.
 *
 * The tiers escalate, which is the thing that separates this from the disk
 * gauge in `docs/usage.md`. A small disk at 88% is its normal state and never
 * escalates; a context window at 88% is about to be compacted, so the reading
 * earns a colour and eventually words. `calm` is deliberately still a colour
 * rather than a neutral grey — a meter drawn in the ink colour reads as a
 * divider that happens to be short, and the whole point of the edge is that it
 * is legible at 2px.
 */

export type ContextTier = "calm" | "watch" | "high" | "critical";

export interface ContextTierStyle {
  tier: ContextTier;
  /** The words, where a tier has earned them. Empty below `high`. */
  label: string;
  /** Background class for the filled part of the meter. */
  bar: string;
  /**
   * The same fill, addressed through `Progress`'s indicator slot. Spelled out
   * rather than composed from `bar`, because Tailwind scans source text: a
   * class built at runtime is a class that was never generated.
   */
  indicator: string;
  /** Background class for the unfilled track. */
  track: string;
  /** Text class for the percentage and any label beside it. */
  text: string;
}

const TIERS: Record<ContextTier, ContextTierStyle> = {
  calm: {
    tier: "calm",
    label: "",
    bar: "bg-emerald-500",
    indicator: "[&>[data-slot=progress-indicator]]:bg-emerald-500",
    track: "bg-emerald-500/15",
    text: "text-muted-foreground",
  },
  watch: {
    tier: "watch",
    label: "",
    bar: "bg-amber-500",
    indicator: "[&>[data-slot=progress-indicator]]:bg-amber-500",
    track: "bg-amber-500/15",
    text: "text-muted-foreground",
  },
  high: {
    tier: "high",
    label: "High usage",
    bar: "bg-orange-500",
    indicator: "[&>[data-slot=progress-indicator]]:bg-orange-500",
    track: "bg-orange-500/20",
    text: "text-orange-400",
  },
  critical: {
    tier: "critical",
    label: "Critical",
    bar: "bg-red-500",
    indicator: "[&>[data-slot=progress-indicator]]:bg-red-500",
    track: "bg-red-500/20",
    text: "text-red-500",
  },
};

/** Percent of the window used, 0..100, clamped — never NaN for a zero window. */
export function contextPercent(usage: {
  contextWindow: number;
  inputTokens: number;
  outputTokens: number;
  usedTokens?: number;
}): number {
  // usedTokens is whichever signal spoke last — a live measurement after a
  // compaction, the turn/stream numbers otherwise. It is absent only for usage
  // restored from history predating the field.
  const used = usage.usedTokens ?? usage.inputTokens + usage.outputTokens;
  if (!(usage.contextWindow > 0)) return 0;
  return Math.min(Math.round((used / usage.contextWindow) * 100), 100);
}

export function contextTier(pct: number): ContextTierStyle {
  if (pct >= 95) return TIERS.critical;
  if (pct >= 80) return TIERS.high;
  if (pct >= 60) return TIERS.watch;
  return TIERS.calm;
}

/** Whether the reading has escalated far enough to be worth words. */
export function contextEscalated(pct: number): boolean {
  return pct >= 80;
}
