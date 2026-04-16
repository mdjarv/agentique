import { FileMinus, FilePlus, FileSymlink, FileText } from "lucide-react";
import { useMemo, useState } from "react";
import { ExpandableRow } from "~/components/chat/ExpandableRow";
import { FilePath } from "~/components/chat/git/FilePath";
import type { DiffResult } from "~/lib/session/actions";

interface DiffViewProps {
  result: DiffResult;
}

export function statusIcon(status: string) {
  switch (status) {
    case "added":
      return <FilePlus className="h-3.5 w-3.5 text-success" />;
    case "deleted":
      return <FileMinus className="h-3.5 w-3.5 text-destructive" />;
    case "renamed":
      return <FileSymlink className="h-3.5 w-3.5 text-primary" />;
    default:
      return <FileText className="h-3.5 w-3.5 text-warning" />;
  }
}

function totalStats(result: DiffResult) {
  let ins = 0;
  let del = 0;
  for (const f of result.files) {
    ins += f.insertions;
    del += f.deletions;
  }
  return { ins, del };
}

export function extractFileDiff(fullDiff: string, path: string): string {
  const marker = `diff --git a/${path} b/${path}`;
  const start = fullDiff.indexOf(marker);
  if (start === -1) return "";
  const nextDiff = fullDiff.indexOf("\ndiff --git ", start + marker.length);
  if (nextDiff === -1) return fullDiff.slice(start);
  return fullDiff.slice(start, nextDiff);
}

export function classifyLine(line: string): string {
  if (line.startsWith("+") && !line.startsWith("+++")) {
    return "px-3 bg-success/10 text-success";
  }
  if (line.startsWith("-") && !line.startsWith("---")) {
    return "px-3 bg-destructive/10 text-destructive";
  }
  if (line.startsWith("@@")) {
    return "px-3 text-primary";
  }
  return "px-3 text-muted-foreground";
}

/** Parse a hunk header like `@@ -74,6 +74,15 @@ export class WsClient {` */
function HunkHeader({ line }: { line: string }) {
  const match = line.match(/^(@@\s*-[\d,]+\s+\+[\d,]+\s*@@)\s?(.*)/);
  if (!match) return <>{line}</>;
  const [, range, context] = match;
  return (
    <>
      <span className="text-primary/50">{range}</span>
      {context && <span className="text-primary ml-1">{context}</span>}
    </>
  );
}

export function DiffLines({ text }: { text: string }) {
  const keyed = useMemo(
    () => text.split("\n").map((line, n) => ({ key: `L${n + 1}`, line })),
    [text],
  );
  return (
    <pre className="text-xs leading-relaxed overflow-x-auto">
      {keyed.map(({ key, line }) => {
        const isHunk = line.startsWith("@@");
        return (
          <div
            key={key}
            className={isHunk ? "px-3 bg-primary/5 py-0.5 mt-1 first:mt-0" : classifyLine(line)}
          >
            {isHunk ? <HunkHeader line={line} /> : line}
          </div>
        );
      })}
    </pre>
  );
}

export function FileEntry({
  path,
  insertions,
  deletions,
  status,
  diff,
}: {
  path: string;
  insertions: number;
  deletions: number;
  status: string;
  diff: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const hasDiff = diff.length > 0;

  return (
    <div className="border-b last:border-b-0">
      <ExpandableRow
        expanded={expanded}
        onToggle={() => setExpanded(!expanded)}
        expandable={hasDiff}
        className="px-3"
        trailing={
          <span className="flex items-center gap-2 text-xs">
            {insertions > 0 && <span className="text-success">+{insertions}</span>}
            {deletions > 0 && <span className="text-destructive">-{deletions}</span>}
          </span>
        }
      >
        {statusIcon(status)}
        <FilePath path={path} className="font-mono truncate min-w-0 flex" />
      </ExpandableRow>
      {expanded && (
        <div className="relative border-t bg-muted/30 max-h-80 overflow-y-auto">
          <DiffLines text={diff} />
          <div className="pointer-events-none absolute right-0 top-0 h-full w-6 bg-gradient-to-l from-muted/60 to-transparent md:hidden" />
        </div>
      )}
    </div>
  );
}

export function DiffView({ result }: DiffViewProps) {
  if (!result.hasDiff) {
    return <div className="px-4 py-3 text-sm text-muted-foreground">No changes detected.</div>;
  }

  const { ins, del } = totalStats(result);

  return (
    <div className="border-t">
      {result.truncated && (
        <div className="px-4 py-2 text-xs text-warning bg-warning/10 border-b">
          Diff too large, showing summary only.
        </div>
      )}
      <div className="px-4 py-2 text-xs text-muted-foreground border-b">
        {result.files.length} file{result.files.length !== 1 ? "s" : ""} changed
        {ins > 0 && (
          <span className="text-success">
            , {ins} insertion{ins !== 1 ? "s" : ""}(+)
          </span>
        )}
        {del > 0 && (
          <span className="text-destructive">
            , {del} deletion{del !== 1 ? "s" : ""}(-)
          </span>
        )}
      </div>
      <div>
        {result.files.map((file) => (
          <FileEntry
            key={file.path}
            path={file.path}
            insertions={file.insertions}
            deletions={file.deletions}
            status={file.status}
            diff={extractFileDiff(result.diff, file.path)}
          />
        ))}
      </div>
    </div>
  );
}
