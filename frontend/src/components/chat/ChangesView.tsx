import { Circle } from "lucide-react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { useState } from "react";
import type { DiffResult } from "~/lib/session-actions";
import { cn } from "~/lib/utils";
import { DiffLines, extractFileDiff, statusIcon } from "./DiffView";

interface ChangesViewProps {
  committedDiff: DiffResult | null;
  uncommittedDiff: DiffResult | null;
}

interface MergedFile {
  path: string;
  insertions: number;
  deletions: number;
  status: string;
  diff: string;
  uncommitted: boolean;
}

function mergeFiles(committed: DiffResult | null, uncommitted: DiffResult | null): MergedFile[] {
  const fileMap = new Map<string, MergedFile>();

  // Committed files first.
  if (committed?.hasDiff) {
    for (const f of committed.files) {
      fileMap.set(f.path, {
        path: f.path,
        insertions: f.insertions,
        deletions: f.deletions,
        status: f.status,
        diff: extractFileDiff(committed.diff, f.path),
        uncommitted: false,
      });
    }
  }

  // Uncommitted files override (they represent the current state).
  if (uncommitted?.hasDiff) {
    for (const f of uncommitted.files) {
      fileMap.set(f.path, {
        path: f.path,
        insertions: f.insertions,
        deletions: f.deletions,
        status: f.status,
        diff: extractFileDiff(uncommitted.diff, f.path),
        uncommitted: true,
      });
    }
  }

  return Array.from(fileMap.values());
}

function FileRow({ file }: { file: MergedFile }) {
  const [expanded, setExpanded] = useState(false);
  const hasDiff = file.diff.length > 0;

  return (
    <div className="border-b last:border-b-0">
      <button
        type="button"
        onClick={() => hasDiff && setExpanded(!expanded)}
        className={cn(
          "flex items-center gap-2 px-3 py-1.5 text-xs w-full text-left transition-colors",
          hasDiff && "hover:bg-muted/80 cursor-pointer",
        )}
      >
        {hasDiff ? (
          expanded ? (
            <ChevronDown className="h-3 w-3 shrink-0 text-muted-foreground" />
          ) : (
            <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground" />
          )
        ) : (
          <span className="w-3 shrink-0" />
        )}
        {statusIcon(file.status)}
        <span className="font-mono truncate min-w-0">{file.path}</span>
        <span className="ml-auto flex items-center gap-2 shrink-0 text-xs">
          {file.insertions > 0 && <span className="text-success">+{file.insertions}</span>}
          {file.deletions > 0 && <span className="text-destructive">-{file.deletions}</span>}
          {file.uncommitted && <Circle className="h-2 w-2 fill-warning text-warning" />}
        </span>
      </button>
      {expanded && (
        <div className="border-t bg-muted/30 max-h-80 overflow-y-auto">
          <DiffLines text={file.diff} />
        </div>
      )}
    </div>
  );
}

export function ChangesView({ committedDiff, uncommittedDiff }: ChangesViewProps) {
  const files = mergeFiles(committedDiff, uncommittedDiff);

  if (files.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center text-sm text-muted-foreground">
        No changes detected.
      </div>
    );
  }

  let totalIns = 0;
  let totalDel = 0;
  for (const f of files) {
    totalIns += f.insertions;
    totalDel += f.deletions;
  }

  const hasUncommitted = files.some((f) => f.uncommitted);
  const truncated = (committedDiff?.truncated ?? false) || (uncommittedDiff?.truncated ?? false);

  return (
    <div className="flex-1 overflow-y-auto">
      {truncated && (
        <div className="px-4 py-2 text-xs text-warning bg-warning/10 border-b">
          Diff too large, showing summary only.
        </div>
      )}
      <div className="px-4 py-2 text-xs text-muted-foreground border-b flex items-center gap-2">
        <span>
          {files.length} file{files.length !== 1 ? "s" : ""} changed
        </span>
        {totalIns > 0 && <span className="text-success">+{totalIns}</span>}
        {totalDel > 0 && <span className="text-destructive">-{totalDel}</span>}
        {hasUncommitted && (
          <span className="ml-auto flex items-center gap-1 text-warning/70">
            <Circle className="h-2 w-2 fill-current" />
            uncommitted
          </span>
        )}
      </div>
      <div>
        {files.map((file) => (
          <FileRow key={file.path} file={file} />
        ))}
      </div>
    </div>
  );
}
