import type { DiffResult } from "~/lib/session/actions";
import { cn } from "~/lib/utils";
import { extractFileDiff } from "../DiffView";

export interface MergedFile {
  path: string;
  insertions: number;
  deletions: number;
  status: string;
  diff: string;
  uncommitted: boolean;
}

export interface GroupedFiles {
  committed: MergedFile[];
  uncommitted: MergedFile[];
}

export function groupFiles(
  committed: DiffResult | null,
  uncommitted: DiffResult | null,
): GroupedFiles {
  const seen = new Set<string>();
  const committedFiles: MergedFile[] = [];
  const uncommittedFiles: MergedFile[] = [];

  // Collect uncommitted files first to know which paths are overridden.
  if (uncommitted?.hasDiff) {
    for (const f of uncommitted.files) {
      seen.add(f.path);
      uncommittedFiles.push({
        path: f.path,
        insertions: f.insertions,
        deletions: f.deletions,
        status: f.status,
        diff: extractFileDiff(uncommitted.diff, f.path),
        uncommitted: true,
      });
    }
  }

  // Committed files that weren't overridden by uncommitted.
  if (committed?.hasDiff) {
    for (const f of committed.files) {
      if (seen.has(f.path)) continue;
      committedFiles.push({
        path: f.path,
        insertions: f.insertions,
        deletions: f.deletions,
        status: f.status,
        diff: extractFileDiff(committed.diff, f.path),
        uncommitted: false,
      });
    }
  }

  return { committed: committedFiles, uncommitted: uncommittedFiles };
}

const TOTAL_BLOCKS = 5;

function blockColor(pos: number, greenCount: number): string {
  return pos < greenCount ? "bg-success" : "bg-destructive";
}

export function DiffStatBar({ insertions, deletions }: { insertions: number; deletions: number }) {
  const total = insertions + deletions;
  if (total === 0) return null;

  const g = Math.round((insertions / total) * TOTAL_BLOCKS);

  return (
    <span className="inline-flex items-center gap-px">
      {Array.from({ length: TOTAL_BLOCKS }, (_, i) => (
        <span key={i} className={cn("inline-block h-2 w-2 rounded-[1px]", blockColor(i, g))} />
      ))}
    </span>
  );
}
