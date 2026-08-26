/**
 * One file's unified diff, parsed into addressable lines.
 *
 * The Changes view renders every file in one scroll and lets the reader select
 * a range to ask about, so a line has to know two things a raw string does not:
 * what kind of line it is, and which line of the file it is. Both come out of
 * the hunk headers, which is why parsing happens once here rather than being
 * re-derived at every render.
 */

export type DiffLineKind = "hunk" | "add" | "del" | "context" | "meta";

export interface DiffLine {
  kind: DiffLineKind;
  /** The line exactly as git wrote it, marker included. What a quote reproduces. */
  text: string;
  /** Line number in the old file. Absent on additions and hunk headers. */
  oldNo?: number;
  /** Line number in the new file. Absent on deletions and hunk headers. */
  newNo?: number;
}

const HUNK_HEADER = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;

/**
 * Parse one file's diff. Everything before the first `@@` is `meta` — the
 * `diff --git`, `index`, `---`/`+++` lines, and, for a file with no textual
 * diff, the `Binary files ... differ` note that stands in for one.
 */
export function parseDiffLines(diff: string): DiffLine[] {
  const raw = diff.split("\n");
  // git ends the patch with a newline, so the split leaves one empty tail.
  if (raw.length > 0 && raw[raw.length - 1] === "") raw.pop();

  const lines: DiffLine[] = [];
  let oldNo = 0;
  let newNo = 0;
  let inHunk = false;

  for (const text of raw) {
    const header = HUNK_HEADER.exec(text);
    if (header) {
      inHunk = true;
      oldNo = Number(header[1]);
      newNo = Number(header[2]);
      lines.push({ kind: "hunk", text });
      continue;
    }
    // "\ No newline at end of file" belongs to the hunk but numbers nothing.
    if (!inHunk || text.startsWith("\\")) {
      lines.push({ kind: "meta", text });
      continue;
    }
    if (text.startsWith("+")) {
      lines.push({ kind: "add", text, newNo: newNo++ });
      continue;
    }
    if (text.startsWith("-")) {
      lines.push({ kind: "del", text, oldNo: oldNo++ });
      continue;
    }
    lines.push({ kind: "context", text, oldNo: oldNo++, newNo: newNo++ });
  }

  return lines;
}

/** The content of a line without git's leading marker. */
export function lineContent(line: DiffLine): string {
  if (line.kind === "add" || line.kind === "del" || line.kind === "context") {
    return line.text.slice(1);
  }
  return line.text;
}

/**
 * Which line of the file this is, for a gutter that has room for one column.
 *
 * The new file's numbering wins wherever it exists, because that is the file
 * you would open; a deletion has no line there and falls back to the old one.
 */
export function lineNumber(line: DiffLine): number | undefined {
  return line.newNo ?? line.oldNo;
}

/** Whether the file has anything to render below its header. */
export function hasRenderableDiff(lines: readonly DiffLine[]): boolean {
  return lines.some((line) => line.kind !== "meta");
}

/**
 * The stand-in a file shows when it has no textual diff: git's own note, if it
 * wrote one, and nothing invented when it did not.
 */
export function diffNote(lines: readonly DiffLine[]): string | undefined {
  return lines.find((line) => line.text.startsWith("Binary files"))?.text;
}
