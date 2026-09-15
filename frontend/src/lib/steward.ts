/**
 * This machine's steward findings (docs/peers.md, the steward), as one closed
 * table: which kinds earn the footer's mark, and the words a row says.
 *
 * The precedent is `lib/update-mark.ts`: the mark in the footer and the rows in
 * the popover it opens read the same table, so they cannot report two things
 * about one fact.
 *
 * Only four kinds are the mark's. The others already have a home on this
 * line or on a row, and one mark means one thing across surfaces:
 * `update-waiting` is `UpdateMark`, `disk-low` is the disk meter turning amber,
 * `session-blocked-long` is the triangle on the session's own row. What is left
 * is what nothing else on the line says — a CLI that is signed out, a loop that
 * paused itself, backups that stopped landing, a brain whose vector index has
 * been gone for ten minutes — and all four need a hand. The Memory page's
 * badge says the last one too, but nobody reads a page they have no reason to
 * open, which is how an outage once ran eleven hours unnoticed.
 */

import { Stethoscope } from "lucide-react";
import { semanticFindingDetail } from "~/lib/brain-semantic";
import { formatTurnTime } from "~/lib/format";
import { apiFetch } from "~/lib/machines/api";

export type FindingKind =
  | "cli-signed-out"
  | "disk-low"
  | "loop-paused"
  | "session-blocked-long"
  | "update-waiting"
  | "backup-failing"
  | "semantic-recall-down";

export interface Finding {
  kind: FindingKind | string;
  subject?: string;
  severity: "warning" | "notice" | string;
  remedy: "hand" | "reclaim" | "update" | string;
  facts?: Record<string, unknown>;
  openedAt?: string;
}

/** The one glyph: a machine asking for a look. Not `TriangleAlert`, which
 *  already means "someone is waiting on you" on rows, the dock and the deck. */
export const FINDING_GLYPH = Stethoscope;

const FOOTER_KINDS: ReadonlySet<string> = new Set([
  "cli-signed-out",
  "loop-paused",
  "backup-failing",
  "semantic-recall-down",
]);

/** The findings the footer's mark stands for, oldest first. */
export function footerFindings(findings: readonly Finding[]): Finding[] {
  return findings.filter((f) => FOOTER_KINDS.has(f.kind));
}

function fact(f: Finding, key: string): string {
  const v = f.facts?.[key];
  return typeof v === "string" ? v : "";
}

/** A row: what is wrong, and what fixes it. */
export function findingRow(f: Finding): { label: string; detail: string } {
  switch (f.kind) {
    case "cli-signed-out":
      return {
        label: `${fact(f, "agent") || "A provider CLI"} is signed out`,
        detail: fact(f, "help") || "Sign in again on this machine.",
      };
    case "loop-paused":
      return {
        label: `${fact(f, "loop") || "A loop"} paused itself`,
        detail: "It failed repeatedly. Open the session's Loops tab to look and re-enable it.",
      };
    case "backup-failing": {
      const newest = Date.parse(fact(f, "newest"));
      return {
        label: "Database backups stopped",
        detail: Number.isNaN(newest)
          ? "None has been written yet."
          : `The newest is from ${formatTurnTime(newest)}.`,
      };
    }
    case "semantic-recall-down":
      return {
        label: "Semantic recall is down",
        detail: semanticFindingDetail(fact(f, "reason"), fact(f, "since")),
      };
    default:
      return { label: String(f.kind), detail: "" };
  }
}

/** The sentence for the trigger's accessible name and tooltip. Null when the
 *  mark has nothing to stand for — the same predicate as the mark itself. */
export function findingsLabel(findings: readonly Finding[]): string | null {
  const shown = footerFindings(findings);
  const [only] = shown;
  if (!only) return null;
  if (shown.length === 1) return findingRow(only).label;
  return `${shown.length} things on this machine need a look`;
}

/**
 * This machine's open findings. A server from before the steward has no such
 * route, and an unmounted /api path falls through to the SPA with a 200 and
 * text/html (CLAUDE.md, the brain) — so anything that is not JSON is "no
 * findings", never a parse error in the footer.
 */
export async function fetchFindings(): Promise<Finding[]> {
  const resp = await apiFetch(undefined, "/api/steward/findings");
  if (!resp.ok) throw new Error(`findings failed (${resp.status})`);
  if (!resp.headers.get("content-type")?.includes("application/json")) return [];
  const body = (await resp.json()) as { findings?: Finding[] };
  return Array.isArray(body.findings) ? body.findings : [];
}
