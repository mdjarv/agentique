/**
 * The session dock's vocabulary: which views exist, which of them a given
 * session actually has, and what the collapsed toggle is allowed to say.
 *
 * A dock view is *derived*, never curated — `Changes` exists because the
 * session has changes, `Loops` because it has schedules. Nothing appears
 * because the user opened it, and nothing lingers after its subject is gone.
 * That is the whole reason `resolveDockView` exists: a stored view outlives
 * the thing it showed.
 */
import type { AgentBadgeState } from "~/lib/agent-runs";
import type { LoopBadgeState } from "~/lib/loop-attention";

/**
 * `work` is the only group: Todos and Agents stack inside it, because the plan
 * and who is out working it are true at the same time. The rest are single
 * views and say so by having no sections.
 */
export type DockView = "work" | "changes" | "loops" | "browser";

/** Fixed order — the tab row never reorders under the user. */
export const DOCK_VIEWS: readonly DockView[] = ["work", "changes", "loops", "browser"];

export const DOCK_LABELS: Record<DockView, string> = {
  work: "Work",
  changes: "Changes",
  loops: "Loops",
  browser: "Browser",
};

/** What a session has to show. Every field is a fact about the session. */
export interface DockAvailability {
  work: boolean;
  changes: boolean;
  loops: boolean;
  browser: boolean;
}

export function availableDockViews(available: DockAvailability): DockView[] {
  return DOCK_VIEWS.filter((view) => available[view]);
}

/**
 * The reconciler. A persisted view whose subject has since vanished — the diff
 * was merged away, the schedule deleted — falls back to another available view
 * rather than collapsing the dock, because collapsing would read as the user's
 * own gesture. Returns null only when the session has nothing at all to dock.
 */
export function resolveDockView(
  stored: DockView | null | undefined,
  available: DockAvailability,
): DockView | null {
  if (stored && available[stored]) return stored;
  return availableDockViews(available)[0] ?? null;
}

/**
 * Legacy `?tab=` values, from before the dock. Chat was a tab then and is the
 * page now, so it maps to "nothing open" rather than to a view.
 *
 * Kept indefinitely: these links live in other people's clipboards, and in
 * deep-links this app itself minted (`schedule.deep-link`, "view turn").
 */
export function legacyTabToDock(tab: string | undefined): DockView | null {
  switch (tab) {
    case "todos":
    case "agents":
      return "work";
    case "git":
    case "changes":
      return "changes";
    case "loops":
      return "loops";
    default:
      return null;
  }
}

/**
 * What the dock's toggle says while the dock is shut.
 *
 * Collapsing the dock is the one thing that costs information — the per-view
 * badges go with it — so the toggle carries a single aggregate mark in the
 * app's usual ranking (`lib/session/priority.ts`): someone waiting on you
 * outranks something that failed, which outranks something merely live.
 *
 * One mark, never a summary. A toggle that tries to report three states at
 * once reports none of them.
 */
export type DockAlertKind = "blocked" | "failed" | "live";

export interface DockAlert {
  kind: DockAlertKind;
  /** Only `live` and `failed` carry a number; `blocked` is a mark, not a tally. */
  count: number;
}

export function dockAlertState(
  agents: AgentBadgeState,
  loops: LoopBadgeState | null,
): DockAlert | null {
  if (loops?.kind === "blocked") return { kind: "blocked", count: loops.count };
  if (agents.failed > 0) return { kind: "failed", count: agents.failed };
  if (loops?.kind === "paused") return { kind: "failed", count: loops.count };
  if (agents.running > 0) return { kind: "live", count: agents.running };
  return null;
}
