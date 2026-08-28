/**
 * What the disk is spent on, ordered and coloured by **what you can do about
 * it** rather than by which directory it happens to sit in.
 *
 * The previous breakdown grouped by location — data directory, then elsewhere —
 * which answered a question nobody arrives with. Worktrees were one 3.8 GB bar
 * whose largest component was a running session no cleanup will ever touch,
 * beside a Backups bar that no button on the page removes, in the same ink. A
 * reader could not tell the two apart, and the one number they came for — how
 * much of this can go — was in neither.
 *
 * So a row's class is its verdict:
 *
 * - `live`   — in use right now. Nothing removes it, and that is the answer.
 * - `sweep`  — a verb on this page removes it.
 * - `policy` — removed by a setting or a retention rule, never a click here.
 *
 * The worktrees category is therefore **split**, because it is the one category
 * that spans two verdicts and holds most of the bytes. The split is exact
 * rather than estimated: every directory under the worktrees root is either an
 * orphan, or a session the server has already judged `reclaimable`, or a
 * session it has not — so the three add back up to the category total the
 * backend reported, and no row is inferred from a size.
 *
 * Keeping this out of the component means the arithmetic is testable without
 * rendering, and the component cannot quietly grow a fourth verdict.
 */
import type { StorageUsage } from "~/lib/generated-types";
import { canReclaim } from "~/lib/storage/selection";

/** What a reader can do about a row. Closed: a new row must pick a verdict. */
export type BreakdownClass = "live" | "sweep" | "policy";

/** The one verb this page offers against a row, when it has one. */
export type BreakdownAction = "reclaim";

export interface BreakdownRow {
  key: string;
  label: string;
  /** What it is, in the reader's terms — never a restatement of the label. */
  detail: string;
  bytes: number;
  cls: BreakdownClass;
  /**
   * Present only where a verb genuinely exists. Most rows have none, and
   * inventing one — a "clean" button on backups, on temp files that only go
   * when their session does — would offer an action the server cannot perform.
   */
  action?: BreakdownAction;
}

export interface Breakdown {
  rows: BreakdownRow[];
  /** The denominator every bar is drawn against: everything measured. */
  total: number;
  /** Bytes the sweep verb would free, for the row that offers it. */
  sweepBytes: number;
}

const CLASS_ORDER: Record<BreakdownClass, number> = { live: 0, sweep: 1, policy: 2 };

function byKey(cats: { key: string; bytes: number }[]): Record<string, number> {
  const out: Record<string, number> = {};
  for (const c of cats) out[c.key] = c.bytes;
  return out;
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/**
 * Build the rows.
 *
 * Zero-byte rows are dropped rather than drawn at 0% — a bar with nothing in it
 * is a category the reader has to rule out by reading its figure. `certs` is
 * routinely 801 bytes and would otherwise be a permanent empty line.
 */
export function buildBreakdown(usage: StorageUsage): Breakdown {
  const cat = byKey(usage.categories);
  const temp = byKey(usage.tempCategories ?? []);

  const sessions = usage.projects.flatMap((p) => p.sessions);
  const reclaimable = sessions.filter(canReclaim);

  // Worktree bytes only: `reclaimableBytes` on the wire is worktree + temp, so
  // using it here would double-count the temp categories below it.
  const sweepWorktree = reclaimable.reduce((a, s) => a + s.bytes, 0);
  const orphanBytes = usage.orphans.reduce((a, o) => a + o.bytes, 0);
  const liveWorktree = Math.max(0, (cat.worktrees ?? 0) - sweepWorktree - orphanBytes);
  const liveCount = sessions.length - reclaimable.length;

  const all: BreakdownRow[] = [
    {
      key: "worktrees-live",
      label: "Live session worktrees",
      detail: `${plural(liveCount, "session", "sessions")} still working`,
      bytes: liveWorktree,
      cls: "live",
    },
    {
      key: "database",
      label: "Database",
      detail: "sessions, transcripts and history",
      bytes: cat.database ?? 0,
      cls: "live",
    },
    {
      key: "certs",
      label: "Certificates",
      detail: "this machine's TLS keypair",
      bytes: cat.certs ?? 0,
      cls: "live",
    },
    {
      key: "worktrees-finished",
      label: "Finished session worktrees",
      detail: `${plural(reclaimable.length, "session", "sessions")} · branches and history are kept`,
      bytes: sweepWorktree,
      cls: "sweep",
      action: reclaimable.length > 0 ? "reclaim" : undefined,
    },
    {
      key: "worktrees-orphaned",
      // No action here on purpose: the orphans card below lists them one by
      // one and owns "Delete all". An action taken on a listed thing belongs
      // on the surface that lists it.
      label: "Orphaned worktrees",
      detail: "no matching session — listed below",
      bytes: orphanBytes,
      cls: "sweep",
    },
    {
      key: "chrome-profiles",
      label: "Browser profiles",
      detail: "one per session — goes when its session does",
      bytes: temp["chrome-profiles"] ?? 0,
      cls: "sweep",
    },
    {
      key: "scratchpads",
      label: "Agent scratchpads",
      detail: "agent working files — go with their session",
      bytes: temp.scratchpads ?? 0,
      cls: "sweep",
    },
    {
      key: "backups",
      label: "Database backups",
      detail: "kept by retention, never swept from here",
      bytes: cat.backups ?? 0,
      cls: "policy",
    },
    {
      key: "session-files",
      label: "Session files",
      detail: "what agents saved — outlives the worktree",
      bytes: cat["session-files"] ?? 0,
      cls: "policy",
    },
    {
      key: "other",
      label: "Other",
      detail: "everything else in the data directory",
      bytes: cat.other ?? 0,
      cls: "policy",
    },
  ];

  const rows = all
    .filter((r) => r.bytes > 0)
    .sort((a, b) => CLASS_ORDER[a.cls] - CLASS_ORDER[b.cls] || b.bytes - a.bytes);

  return {
    rows,
    total: rows.reduce((a, r) => a + r.bytes, 0),
    sweepBytes: reclaimable.reduce((a, s) => a + (s.totalBytes || s.bytes), 0),
  };
}
