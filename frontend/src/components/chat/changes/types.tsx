import type { DiffResult } from "~/lib/session/actions";
import { cn } from "~/lib/utils";
import { extractFileDiff } from "../DiffView";
import type { DiffScope } from "./ChangesToolbar";

export interface MergedFile {
  path: string;
  insertions: number;
  deletions: number;
  status: string;
  diff: string;
  /** Has changes that are not committed. A fact about the file, not a section. */
  uncommitted: boolean;
}

function byPath(a: MergedFile, b: MergedFile): number {
  return a.path.localeCompare(b.path, undefined, { numeric: true, sensitivity: "base" });
}

/**
 * The files one scope shows.
 *
 * The two RPCs behind this are not the split their names suggest.
 * `session.diff` is a **working-tree-vs-base** diff, so it already contains
 * uncommitted edits to tracked files — what it lacks is untracked ones, which
 * only `session.uncommitted-diff` reports. So the session scope is the session
 * diff plus the untracked files the other one found, and the working scope is
 * the uncommitted diff alone.
 */
export function filesForScope(
  scope: DiffScope,
  sessionDiff: DiffResult | null,
  uncommittedDiff: DiffResult | null,
): MergedFile[] {
  const uncommittedPaths = new Set(
    uncommittedDiff?.hasDiff ? uncommittedDiff.files.map((f) => f.path) : [],
  );

  if (scope === "working") {
    if (!uncommittedDiff?.hasDiff) return [];
    return uncommittedDiff.files
      .map((f) => ({
        path: f.path,
        insertions: f.insertions,
        deletions: f.deletions,
        status: f.status,
        diff: extractFileDiff(uncommittedDiff.diff, f.path),
        uncommitted: true,
      }))
      .sort(byPath);
  }

  const files: MergedFile[] = [];
  const seen = new Set<string>();
  if (sessionDiff?.hasDiff) {
    for (const f of sessionDiff.files) {
      seen.add(f.path);
      files.push({
        path: f.path,
        insertions: f.insertions,
        deletions: f.deletions,
        status: f.status,
        diff: extractFileDiff(sessionDiff.diff, f.path),
        uncommitted: uncommittedPaths.has(f.path),
      });
    }
  }
  // Untracked files are in neither commit nor index, so the base-relative diff
  // cannot see them — but they are unmistakably part of what the session did.
  if (uncommittedDiff?.hasDiff) {
    for (const f of uncommittedDiff.files) {
      if (seen.has(f.path)) continue;
      files.push({
        path: f.path,
        insertions: f.insertions,
        deletions: f.deletions,
        status: f.status,
        diff: extractFileDiff(uncommittedDiff.diff, f.path),
        uncommitted: true,
      });
    }
  }
  return files.sort(byPath);
}

export interface DiffTotals {
  insertions: number;
  deletions: number;
}

export function diffTotals(files: readonly MergedFile[]): DiffTotals {
  let insertions = 0;
  let deletions = 0;
  for (const file of files) {
    insertions += file.insertions;
    deletions += file.deletions;
  }
  return { insertions, deletions };
}

const TOTAL_BLOCKS = 5;
const BLOCK_POSITIONS = Array.from({ length: TOTAL_BLOCKS }, (_, i) => i);

function blockColor(pos: number, greenCount: number): string {
  return pos < greenCount ? "bg-success" : "bg-destructive";
}

export function DiffStatBar({ insertions, deletions }: { insertions: number; deletions: number }) {
  const total = insertions + deletions;
  if (total === 0) return null;

  const g = Math.round((insertions / total) * TOTAL_BLOCKS);

  return (
    <span className="inline-flex items-center gap-px">
      {BLOCK_POSITIONS.map((pos) => (
        <span key={pos} className={cn("inline-block h-2 w-2 rounded-[1px]", blockColor(pos, g))} />
      ))}
    </span>
  );
}
