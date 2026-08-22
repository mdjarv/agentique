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
  /** How many of those are one-click safe (push / fast-forward pull). */
  mechanical: number;
  /** Distinct repos, in row order — the collapsed line's faces. */
  chips: SyncChip[];
}

export interface SyncRowInput {
  project: Project;
  status: ProjectGitStatus | undefined;
  /** Resolved once by the caller — the VM stays free of store lookups. */
  machineLabel?: string;
  machineIcon?: string;
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
  for (const { project, status, machineLabel, machineIcon, colorBg, colorFg } of inputs) {
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
  for (const row of rows) {
    if (row.action !== "rebase") mechanical++;
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
  return { total: rows.length, mechanical, chips };
}

/** The rows "Push N" would actually run — mechanical only, never a rebase. */
export function mechanicalRows(rows: SyncRowVM[]): SyncRowVM[] {
  return rows.filter((r) => r.action !== "rebase");
}
