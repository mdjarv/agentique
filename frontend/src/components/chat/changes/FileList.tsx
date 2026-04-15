import { cn } from "~/lib/utils";
import { statusIcon } from "../DiffView";
import { FilePath } from "../git/FilePath";
import { DiffStatBar, type MergedFile } from "./types";

interface FileListProps {
  committed: MergedFile[];
  uncommitted: MergedFile[];
  selectedFile: string | null;
  onSelectFile: (path: string) => void;
  truncated: boolean;
}

function SectionLabel({ label, count }: { label: string; count: number }) {
  return (
    <div className="flex items-center gap-2 px-3 py-1.5 text-[10px] font-medium uppercase tracking-wider text-muted-foreground-dim bg-muted/30">
      <span>{label}</span>
      <span className="tabular-nums">{count}</span>
    </div>
  );
}

function FileRow({
  file,
  selected,
  onSelect,
}: {
  file: MergedFile;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "flex items-center gap-1.5 px-3 py-1.5 text-xs w-full text-left transition-colors cursor-pointer",
        selected
          ? "bg-accent text-accent-foreground"
          : "text-muted-foreground hover:bg-muted/40 hover:text-foreground",
      )}
    >
      <span className="shrink-0 w-3.5">{statusIcon(file.status)}</span>
      <FilePath path={file.path} className="font-mono truncate min-w-0 flex" />
      <span className="ml-auto flex items-center gap-1.5 shrink-0 tabular-nums text-[11px]">
        {file.uncommitted && (
          <span className="inline-block h-1.5 w-1.5 rounded-full bg-warning" title="Uncommitted" />
        )}
        {file.insertions > 0 && <span className="text-success">+{file.insertions}</span>}
        {file.deletions > 0 && <span className="text-destructive">-{file.deletions}</span>}
      </span>
    </button>
  );
}

export function FileList({
  committed,
  uncommitted,
  selectedFile,
  onSelectFile,
  truncated,
}: FileListProps) {
  const allFiles = [...committed, ...uncommitted];
  const showSections = committed.length > 0 && uncommitted.length > 0;

  let totalIns = 0;
  let totalDel = 0;
  for (const f of allFiles) {
    totalIns += f.insertions;
    totalDel += f.deletions;
  }

  return (
    <div className="flex flex-col min-h-0">
      {/* Summary header */}
      <div className="flex items-center gap-1.5 px-3 py-2 text-[11px] text-muted-foreground border-b shrink-0">
        <span className="font-medium text-muted-foreground">
          {allFiles.length} file{allFiles.length !== 1 ? "s" : ""}
        </span>
        {totalIns > 0 && <span className="text-success">+{totalIns}</span>}
        {totalDel > 0 && <span className="text-destructive">-{totalDel}</span>}
        <DiffStatBar insertions={totalIns} deletions={totalDel} />
      </div>

      {truncated && (
        <div className="px-3 py-1 text-[10px] text-warning bg-warning/10 border-b">
          Diff truncated
        </div>
      )}

      {/* File list */}
      <div className="flex-1 overflow-y-auto min-h-0">
        {showSections && committed.length > 0 && (
          <>
            <SectionLabel label="Committed" count={committed.length} />
            {committed.map((file) => (
              <FileRow
                key={file.path}
                file={file}
                selected={selectedFile === file.path}
                onSelect={() => onSelectFile(file.path)}
              />
            ))}
          </>
        )}
        {showSections && uncommitted.length > 0 && (
          <>
            <SectionLabel label="Uncommitted" count={uncommitted.length} />
            {uncommitted.map((file) => (
              <FileRow
                key={file.path}
                file={file}
                selected={selectedFile === file.path}
                onSelect={() => onSelectFile(file.path)}
              />
            ))}
          </>
        )}
        {!showSections &&
          allFiles.map((file) => (
            <FileRow
              key={file.path}
              file={file}
              selected={selectedFile === file.path}
              onSelect={() => onSelectFile(file.path)}
            />
          ))}
      </div>
    </div>
  );
}
