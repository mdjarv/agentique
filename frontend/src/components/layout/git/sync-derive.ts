/**
 * Pure derivation for the sync dock — no React, no stores.
 *
 * The dock's subject is a *checkout*: one row per physical project that has
 * drifted from its remote, because that is what a push or a pull acts on. Its
 * collapsed line's subject is a *repo*: the chip stack counts logical repos
 * (a repo drifted on two machines is one chip, two rows), so the line answers
 * "which repos" and the list answers "what to do".
 */
import type { ProjectGitStatus } from "~/lib/generated-types";
import { displaySlug } from "~/lib/machines/slug";
import type { Project } from "~/lib/types";

/**
 * What the row's one button does.
 * - `push`   — ahead only. Mechanical.
 * - `pull`   — behind, clean, nothing local to replay. Mechanical (`--ff-only`).
 * - `rebase` — diverged, or behind with uncommitted work. Never run from the
 *   dock: it opens a session with a rebase prompt instead.
 */
export type SyncAction = "push" | "pull" | "rebase";

export interface SyncRowVM {
  projectId: string;
  /** Routing slug — machine-qualified for a remote checkout. */
  slug: string;
  /** The slug as read; the row's `@machine` already says where it lives. */
  label: string;
  initials: string;
  /** Bright project color (hex) — tinted for the chip background. */
  colorBg: string;
  /** Theme-appropriate project accent (hex) — chip glyph/initials. */
  colorFg: string;
  iconId?: string;
  /** Machine label, only when the checkout lives on a remote machine. */
  machineLabel?: string;
  /** That machine's icon id — this host's presentation of it. */
  machineIcon?: string;
  /** That machine is unreachable: the row is real, its buttons are not. */
  machineOffline?: boolean;
  /** A proven fault on that machine, if any — the tag says so in rose. */
  machineFault?: string;
  ahead: number;
  behind: number;
  uncommitted: number;
  action: SyncAction;
  /** Groups checkouts of the same repo — canonical remote, else the id. */
  repoKey: string;
}

/** One chip in the collapsed line's stack. */
export interface SyncChip {
  repoKey: string;
  slug: string;
  label: string;
  initials: string;
  colorBg: string;
  colorFg: string;
  iconId?: string;
}

export interface SyncSummary {
  /** Rows — i.e. actions outstanding, not repos. */
  total: number;
  /** How many of those are one-click safe AND reachable right now. */
  mechanical: number;
  /** Diverged rows — real work, but never run from the dock. */
  diverged: number;
  /** Rows on a machine that is currently away. Not a failure, just later. */
  offline: number;
  /** Distinct repos, in row order — the collapsed line's faces. */
  chips: SyncChip[];
}

export interface SyncRowInput {
  project: Project;
  status: ProjectGitStatus | undefined;
  /** Resolved once by the caller — the VM stays free of store lookups. */
  machineLabel?: string;
  machineIcon?: string;
  machineOffline?: boolean;
  machineFault?: string;
  colorBg: string;
  colorFg: string;
}

export function projectInitials(slug: string): string {
  return slug
    .split("-")
    .map((w) => w[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);
}

/** A pull that would have to replay local work, or stash it, is not mechanical. */
export function deriveAction(status: ProjectGitStatus): SyncAction {
  const messy = status.behindRemote > 0 && (status.aheadRemote > 0 || status.uncommittedCount > 0);
  if (messy) return "rebase";
  return status.aheadRemote > 0 ? "push" : "pull";
}

/** Mechanical first (push, then pull), messy last; stable by slug within a rank. */
const ACTION_RANK: Record<SyncAction, number> = { push: 0, pull: 1, rebase: 2 };

/**
 * Drifted checkouts, ordered so the top of the dock is always the part that
 * clears without a decision. Uncommitted-only projects are deliberately absent
 * — dirt is not a sync problem, and including it would keep half the repos
 * permanently docked.
 */
export function deriveSyncRows(inputs: SyncRowInput[]): SyncRowVM[] {
  const rows: SyncRowVM[] = [];
  for (const {
    project,
    status,
    machineLabel,
    machineIcon,
    machineOffline,
    machineFault,
    colorBg,
    colorFg,
  } of inputs) {
    if (!status?.hasRemote) continue;
    if (status.aheadRemote === 0 && status.behindRemote === 0) continue;
    rows.push({
      projectId: project.id,
      slug: project.slug,
      label: displaySlug(project.slug),
      initials: projectInitials(displaySlug(project.slug)),
      colorBg,
      colorFg,
      iconId: project.icon || undefined,
      machineLabel,
      machineIcon,
      machineOffline,
      machineFault,
      ahead: status.aheadRemote,
      behind: status.behindRemote,
      uncommitted: status.uncommittedCount,
      action: deriveAction(status),
      repoKey: project.remote_url ? `r:${project.remote_url}` : `p:${project.id}`,
    });
  }
  return rows.sort(
    (a, b) => ACTION_RANK[a.action] - ACTION_RANK[b.action] || a.label.localeCompare(b.label),
  );
}

export function summarize(rows: SyncRowVM[]): SyncSummary {
  const chips: SyncChip[] = [];
  const seen = new Set<string>();
  let mechanical = 0;
  let diverged = 0;
  let offline = 0;
  for (const row of rows) {
    if (row.machineOffline) offline++;
    if (row.action === "rebase") diverged++;
    else if (!row.machineOffline) mechanical++;
    if (seen.has(row.repoKey)) continue;
    seen.add(row.repoKey);
    chips.push({
      repoKey: row.repoKey,
      slug: row.slug,
      label: row.label,
      initials: row.initials,
      colorBg: row.colorBg,
      colorFg: row.colorFg,
      iconId: row.iconId,
    });
  }
  return { total: rows.length, mechanical, diverged, offline, chips };
}

/**
 * The rows "Sync N" would actually run: mechanical, and on a machine that can
 * answer. A sleeping laptop's checkout is left where it is rather than
 * counted into a bulk action that can only time out.
 */
export function mechanicalRows(rows: SyncRowVM[]): SyncRowVM[] {
  return rows.filter((r) => r.action !== "rebase" && !r.machineOffline);
}

/**
 * The rows the dock can't run: diverged, or on a machine that is away.
 * Diverged first — it is work waiting on a decision, where away is only work
 * waiting on a machine to come back.
 */
export function exceptionRows(rows: SyncRowVM[]): SyncRowVM[] {
  return rows
    .filter((r) => r.action === "rebase" || r.machineOffline)
    .sort((a, b) => Number(a.action !== "rebase") - Number(b.action !== "rebase"));
}

/**
 * The collapsed meter's three segments, in commits. Direction is colour —
 * green ahead, blue behind — except for a diverged checkout, whose commits are
 * amber whichever way they point: the bar's job is to separate what one press
 * can clear from what needs a person, and that distinction outranks direction.
 */
export interface SyncSegments {
  ahead: number;
  behind: number;
  diverged: number;
  /** ahead + behind + diverged; 0 means nothing to draw. */
  total: number;
}

export function syncSegments(rows: SyncRowVM[]): SyncSegments {
  let ahead = 0;
  let behind = 0;
  let diverged = 0;
  for (const row of rows) {
    if (row.action === "rebase") {
      diverged += row.ahead + row.behind;
      continue;
    }
    ahead += row.ahead;
    behind += row.behind;
  }
  return { ahead, behind, diverged, total: ahead + behind + diverged };
}

/**
 * What the bulk button will actually do, in its own units. Never a generic
 * "sync N": the plan names each half it contains, so the number in the label
 * can be checked against the rows above it.
 */
/** The two directions a bulk button runs. */
export type BulkAction = "push" | "pull";

export interface BulkPlan {
  /** Checkouts to push / to pull — reachable and mechanical only. */
  pushes: number;
  pulls: number;
  /** Commits moving in each direction. */
  ahead: number;
  behind: number;
  /** Nothing to run: the button doesn't render. */
  empty: boolean;
}

export function bulkPlan(rows: SyncRowVM[]): BulkPlan {
  const targets = mechanicalRows(rows);
  let pushes = 0;
  let pulls = 0;
  let ahead = 0;
  let behind = 0;
  for (const row of targets) {
    if (row.action === "push") {
      pushes++;
      ahead += row.ahead;
    } else {
      pulls++;
      behind += row.behind;
    }
  }
  return { pushes, pulls, ahead, behind, empty: targets.length === 0 };
}

/**
 * One bulk button's label. Push and pull are separate actions, never one
 * "sync": sending your own work is the thing you almost always want to do
 * first, and folding it in with taking someone else's makes that impossible to
 * do on its own.
 *
 * `compact` is for the case where both buttons share the rail's width: the
 * checkout count goes, because the rows it counts are directly above the
 * button, and the commits stay, because nothing else states them.
 */
export function bulkLabel(plan: BulkPlan, action: BulkAction, compact = false): string {
  if (action === "push") {
    return plan.pushes === 1 || compact
      ? `Push ↑${plan.ahead}`
      : `Push ${plan.pushes} · ↑${plan.ahead}`;
  }
  return plan.pulls === 1 || compact
    ? `Pull ↓${plan.behind}`
    : `Pull ${plan.pulls} · ↓${plan.behind}`;
}

/** The mechanical rows one bulk button runs. */
export function bulkTargets(rows: SyncRowVM[], action: BulkAction): SyncRowVM[] {
  return mechanicalRows(rows).filter((r) => r.action === action);
}
