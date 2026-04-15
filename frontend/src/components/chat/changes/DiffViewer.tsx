import { Code2 } from "lucide-react";
import { DiffLines, statusIcon } from "../DiffView";
import { FilePath } from "../git/FilePath";
import { DiffStatBar, type MergedFile } from "./types";

interface DiffViewerProps {
  file: MergedFile | null;
}

export function DiffViewer({ file }: DiffViewerProps) {
  if (!file) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center gap-2 text-sm text-muted-foreground-dim min-h-0">
        <Code2 className="h-6 w-6" />
        <span className="text-xs">Select a file to view diff</span>
      </div>
    );
  }

  const hasDiff = file.diff.length > 0;

  return (
    <div className="flex-1 flex flex-col min-h-0 min-w-0">
      {/* File header */}
      <div className="flex items-center gap-2 px-4 py-2 border-b text-xs shrink-0 bg-muted/15">
        {statusIcon(file.status)}
        <FilePath path={file.path} className="font-mono truncate min-w-0 flex text-foreground" />
        <span className="ml-auto flex items-center gap-2 shrink-0 text-[11px]">
          {file.insertions > 0 && <span className="text-success">+{file.insertions}</span>}
          {file.deletions > 0 && <span className="text-destructive">-{file.deletions}</span>}
          <DiffStatBar insertions={file.insertions} deletions={file.deletions} />
        </span>
      </div>

      {/* Diff content */}
      {hasDiff ? (
        <div className="flex-1 overflow-auto min-h-0">
          <DiffLines text={file.diff} />
        </div>
      ) : (
        <div className="flex-1 flex items-center justify-center text-xs text-muted-foreground-dim">
          {file.status === "deleted" ? "File deleted" : "Binary file or no diff available"}
        </div>
      )}
    </div>
  );
}
