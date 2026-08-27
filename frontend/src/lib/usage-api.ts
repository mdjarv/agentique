/**
 * Subscription usage: what each vendor says is left, and when it resets
 * (docs/usage.md).
 *
 * The client knows nothing about vendors. It renders whatever records arrive —
 * the set of limits is not fixed, because model-scoped allowances come and go
 * as the account spends against them. Never hardcode a count or a label.
 */

import type { UsageAgent, UsageDocument, UsageLimit } from "~/lib/generated-types";
import { apiFetch } from "~/lib/machines/api";

/** Ask this machine what it has left. `refresh` bypasses the server's reuse
 *  window — that interval absorbs incidental opens, it does not overrule
 *  somebody who pressed the button. */
export async function fetchUsage(refresh = false): Promise<UsageDocument> {
  const resp = await apiFetch(undefined, `/api/usage${refresh ? "?refresh=1" : ""}`);
  if (!resp.ok) throw new Error(`usage failed (${resp.status})`);
  return (await resp.json()) as UsageDocument;
}

/**
 * The one filter both surfaces share.
 *
 * A negative percent means UNKNOWN, not zero — a window we could not read is
 * not a window at zero, and drawing it as an empty track would be a lie. The
 * indicator and the panel must always agree about which windows exist, so this
 * is the only place that decides.
 */
export function usableLimits(agent: UsageAgent): UsageLimit[] {
  return (agent.limits ?? []).filter((l) => l.percent >= 0);
}

/** Agents worth drawing at all: anything with a usable window, or anything
 *  with something to say about why it has none. */
export function renderableAgents(doc: UsageDocument | null): UsageAgent[] {
  if (!doc) return [];
  return doc.agents.filter((a) => usableLimits(a).length > 0 || Boolean(a.usageStatusText));
}

/** Agents that contribute meters to the compact indicator. */
export function meteredAgents(doc: UsageDocument | null): UsageAgent[] {
  if (!doc) return [];
  return doc.agents.filter((a) => usableLimits(a).length > 0);
}

/** A gauge is a level, not an allowance: it never escalates to a warning
 *  colour and never shows a countdown, because it has nothing to reset to. */
export function isGauge(agent: UsageAgent): boolean {
  return agent.kind === "gauge";
}

/** The disk gauge's id, as the storage collector reports it. */
export const STORAGE_AGENT_ID = "storage";

/**
 * The compact indicator's two halves, because they lead different places.
 *
 * An allowance has nowhere to go — what there is to know about it is the meter
 * and the reset, which is the usage popover. A level does: the disk is what
 * `/storage` is a page about, so its mark IS the way there and the popover
 * needs no row repeating it (CLAUDE.md, "a destination gets one home").
 *
 * Order is preserved, so the cluster reads the same left-to-right as it did
 * when one control drew all of it.
 */
export function splitMetered(doc: UsageDocument | null): {
  allowances: UsageAgent[];
  storage: UsageAgent | null;
} {
  const metered = meteredAgents(doc);
  return {
    allowances: metered.filter((a) => a.id !== STORAGE_AGENT_ID),
    storage: metered.find((a) => a.id === STORAGE_AGENT_ID) ?? null,
  };
}

export type Tier = "normal" | "warning" | "critical";

/**
 * How hot a window is.
 *
 * The vendor's own `severity` wins where it gives one — the server knows what
 * counts as a warning for its own limit, and a client-side threshold is a
 * guess about somebody else's allowance. Thresholds are the fallback.
 *
 * A gauge is always `normal`, whatever its height.
 */
export function limitTier(limit: UsageLimit, gauge: boolean): Tier {
  if (gauge) return "normal";
  switch (limit.severity) {
    case "critical":
    case "exceeded":
      return "critical";
    case "warning":
      return "warning";
    case "normal":
      return "normal";
    default:
      break;
  }
  if (limit.percent >= 0.95) return "critical";
  if (limit.percent >= 0.85) return "warning";
  return "normal";
}

/** Clamp for drawing only. Reporting a value above 1 is the honest thing;
 *  clamping belongs to the bar, never to the record. */
export function drawFraction(percent: number): number {
  return Math.max(0, Math.min(percent, 1));
}

/**
 * How long until a window rolls over: "12m", "3h 20m", "2d 4h".
 *
 * Reset countdowns are minutes-scale, so this is ticked every 30 seconds and
 * only while the panel is open — a per-second tick repaints sixty times for a
 * label that changes once.
 */
export function countdown(resetsAt: string | undefined, now = Date.now()): string | null {
  if (!resetsAt) return null;
  const at = Date.parse(resetsAt);
  if (Number.isNaN(at)) return null;
  const ms = at - now;
  if (ms <= 0) return "now";
  const mins = Math.floor(ms / 60_000);
  if (mins < 60) return `${mins}m`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) {
    const rem = mins % 60;
    return rem > 0 ? `${hours}h ${rem}m` : `${hours}h`;
  }
  const days = Math.floor(hours / 24);
  const remH = hours % 24;
  return remH > 0 ? `${days}d ${remH}h` : `${days}d`;
}

/** Token counts get large fast; the panel has room for three characters. */
export function compactTokens(n: number): string {
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${(n / 1000).toFixed(n < 10_000 ? 1 : 0)}K`;
  if (n < 1_000_000_000) return `${(n / 1_000_000).toFixed(n < 10_000_000 ? 1 : 0)}M`;
  return `${(n / 1_000_000_000).toFixed(1)}B`;
}
