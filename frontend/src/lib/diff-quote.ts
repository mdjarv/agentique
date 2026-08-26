/**
 * Turning a selected range of a diff into something you can say to the session.
 *
 * The selection never sends. It is written into the composer and stops there,
 * next to whatever the reader types about it — the same contract the live call
 * honours, and for the same reason: one path into the session pipeline, the
 * visible send button.
 */
import { type DiffLine, lineNumber } from "~/lib/diff-lines";

/** `path:12` for one line, `path:12-40` for a range. */
export function quoteLabel(path: string, lines: readonly DiffLine[]): string {
  const numbers = lines.map(lineNumber).filter((n): n is number => n !== undefined);
  const first = numbers[0];
  const last = numbers[numbers.length - 1];
  if (first === undefined || last === undefined) return path;
  return first === last ? `${path}:${first}` : `${path}:${first}-${last}`;
}

/**
 * The block that lands in the composer: where it came from, then the lines
 * themselves in a `diff` fence so the markers survive.
 *
 * The enclosing hunk header rides along when the selection has one above it —
 * it is the cheapest way to say *which* part of the file this is, and the model
 * reads it the same way a reviewer does.
 */
export function buildDiffQuote(
  path: string,
  all: readonly DiffLine[],
  from: number,
  to: number,
): string {
  const start = Math.min(from, to);
  const end = Math.max(from, to);
  const selected = all.slice(start, end + 1);
  if (selected.length === 0) return "";

  let hunk: string | undefined;
  for (let i = start; i >= 0; i--) {
    const line = all[i];
    if (line?.kind === "hunk") {
      hunk = line.text;
      break;
    }
  }

  const body = [
    ...(hunk && selected[0]?.kind !== "hunk" ? [hunk] : []),
    ...selected.map((line) => line.text),
  ].join("\n");

  return `${quoteLabel(path, selected)}\n\n\`\`\`diff\n${body}\n\`\`\`\n`;
}

/**
 * Append a quote to whatever is already in the composer, leaving the caret's
 * text alone. A blank line between them, and nothing else invented — the
 * reader's question is the point, and the quote is context under it.
 */
export function appendQuote(existing: string, quote: string): string {
  const base = existing.replace(/\s+$/u, "");
  return base === "" ? quote : `${base}\n\n${quote}`;
}
