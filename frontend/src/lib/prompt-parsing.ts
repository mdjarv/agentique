// ---------------------------------------------------------------------------
// Markdown prompt-block parser.
//
// Extracts ```prompt blocks from streamed/static markdown so they can be
// rendered as actionable cards instead of code blocks. The parser is a
// lightweight state machine that correctly handles arbitrarily-nested code
// fences inside the prompt body.
// ---------------------------------------------------------------------------

export interface PromptBlock {
  title: string;
  prompt: string;
  projectSlug?: string;
}

/** Parse title + prompt from a code block's inner text. First line must be `# Title`.
 *  Optional second line `project: <slug>` targets a different project. */
export function parsePromptFromCode(code: string): PromptBlock | null {
  const nl = code.indexOf("\n");
  if (nl === -1) return null;
  const heading = code.slice(0, nl).trim();
  if (!heading.startsWith("# ")) return null;
  const title = heading.slice(2).trim();
  let rest = code.slice(nl + 1);

  let projectSlug: string | undefined;
  const metaMatch = rest.match(/^\n*project:\s*(\S+)\s*\n/);
  if (metaMatch) {
    projectSlug = metaMatch[1];
    rest = rest.slice(metaMatch[0].length);
  }

  const prompt = rest.trim();
  if (!title || !prompt) return null;
  return { title, prompt, projectSlug };
}

// ---------------------------------------------------------------------------
// State-machine prompt block finder (handles nested code fences)
// ---------------------------------------------------------------------------

export interface RawPromptBlock {
  startLine: number;
  endLine: number;
  content: string;
  fenceLen: number;
  maxInnerFence: number;
}

const RE_PROMPT_OPEN = /^ {0,3}(`{3,})prompt\s*$/;
const RE_BARE_FENCE = /^ {0,3}(`{3,})\s*$/;
const RE_INFO_FENCE = /^ {0,3}(`{3,})\S/;

/** Lookahead: determine whether a bare fence should open an inner code block
 *  rather than close the prompt block.
 *
 *  Counts remaining bare and info fences (stopping at the next prompt opener).
 *  If there are >= 2 unpaired bare fences ahead, this one opens an inner block
 *  (one to close it, one more to close the prompt). */
function shouldOpenInnerBlock(lines: string[], currentIndex: number): boolean {
  let bare = 0;
  let info = 0;
  for (let j = currentIndex + 1; j < lines.length; j++) {
    const line = lines[j] ?? "";
    if (RE_PROMPT_OPEN.test(line)) break;
    if (RE_BARE_FENCE.test(line)) bare++;
    else if (RE_INFO_FENCE.test(line)) info++;
  }
  return bare - info >= 2;
}

/** Find prompt blocks in raw markdown, correctly handling nested code fences.
 *  Tracks inner code blocks with a boolean flag instead of a depth counter,
 *  using lookahead to distinguish bare fences that open inner blocks from
 *  bare fences that close the prompt. */
export function findRawPromptBlocks(markdown: string): RawPromptBlock[] {
  const lines = markdown.split("\n");
  const blocks: RawPromptBlock[] = [];
  let i = 0;

  while (i < lines.length) {
    const cur = lines[i] ?? "";
    const openMatch = RE_PROMPT_OPEN.exec(cur);
    if (!openMatch?.[1]) {
      i++;
      continue;
    }

    const fenceLen = openMatch[1].length;
    const startLine = i;
    const contentLines: string[] = [];
    let maxInnerFence = 0;
    let insideInner = false;
    let innerFenceLen = 0;

    i++;
    let found = false;
    while (i < lines.length) {
      const line = lines[i] ?? "";
      const bareMatch = RE_BARE_FENCE.exec(line);
      const infoMatch = RE_INFO_FENCE.exec(line);

      if (insideInner) {
        if (bareMatch?.[1] && bareMatch[1].length >= innerFenceLen) {
          insideInner = false;
          maxInnerFence = Math.max(maxInnerFence, bareMatch[1].length);
        }
        contentLines.push(line);
      } else if (bareMatch?.[1]) {
        if (shouldOpenInnerBlock(lines, i)) {
          insideInner = true;
          innerFenceLen = bareMatch[1].length;
          maxInnerFence = Math.max(maxInnerFence, bareMatch[1].length);
          contentLines.push(line);
        } else {
          found = true;
          i++;
          break;
        }
      } else if (infoMatch?.[1]) {
        insideInner = true;
        innerFenceLen = infoMatch[1].length;
        maxInnerFence = Math.max(maxInnerFence, infoMatch[1].length);
        contentLines.push(line);
      } else {
        contentLines.push(line);
      }

      i++;
    }

    if (found) {
      blocks.push({
        startLine,
        endLine: i - 1,
        content: contentLines.join("\n"),
        fenceLen,
        maxInnerFence,
      });
    }
  }

  return blocks;
}

/** Extract all prompt blocks from raw markdown content. */
export function parsePromptBlocks(markdown: string): PromptBlock[] {
  return findRawPromptBlocks(markdown)
    .map((raw) => parsePromptFromCode(raw.content))
    .filter((b): b is PromptBlock => b !== null);
}

// ---------------------------------------------------------------------------
// Content segmentation — splits markdown into text + prompt segments so
// prompt blocks never pass through the markdown parser.
// ---------------------------------------------------------------------------

export type ContentSegment =
  | { type: "markdown"; content: string }
  | { type: "prompt"; block: PromptBlock }
  | { type: "pending_prompt"; content: string; title?: string };

/** Find a trailing unclosed ```prompt opener not captured by findRawPromptBlocks. */
function findPendingPromptBlock(
  lines: string[],
  completedBlocks: RawPromptBlock[],
): { startLine: number; content: string } | null {
  const completedStarts = new Set(completedBlocks.map((b) => b.startLine));
  for (let i = lines.length - 1; i >= 0; i--) {
    const line = lines[i] ?? "";
    if (RE_PROMPT_OPEN.test(line) && !completedStarts.has(i)) {
      return { startLine: i, content: lines.slice(i + 1).join("\n") };
    }
  }
  return null;
}

/** Split markdown into interleaved text/prompt segments.
 *  Prompt blocks are extracted by our state machine parser and never
 *  reach the markdown renderer, eliminating all fence-nesting issues. */
export function splitByPromptBlocks(markdown: string): ContentSegment[] {
  const rawBlocks = findRawPromptBlocks(markdown);
  const lines = markdown.split("\n");
  const segments: ContentSegment[] = [];
  let cursor = 0;

  for (const raw of rawBlocks) {
    if (raw.startLine > cursor) {
      const text = lines.slice(cursor, raw.startLine).join("\n");
      if (text.trim()) segments.push({ type: "markdown", content: text });
    }

    const parsed = parsePromptFromCode(raw.content);
    if (parsed) segments.push({ type: "prompt", block: parsed });

    cursor = raw.endLine + 1;
  }

  // Detect trailing unclosed ```prompt block (streaming in progress)
  const pending = findPendingPromptBlock(lines, rawBlocks);
  if (pending && pending.startLine >= cursor) {
    if (pending.startLine > cursor) {
      const text = lines.slice(cursor, pending.startLine).join("\n");
      if (text.trim()) segments.push({ type: "markdown", content: text });
    }
    const firstLine = pending.content.split("\n")[0]?.trim() ?? "";
    const title = firstLine.startsWith("# ") ? firstLine.slice(2).trim() : undefined;
    segments.push({ type: "pending_prompt", content: pending.content, title });
    return segments;
  }

  if (cursor < lines.length) {
    const text = lines.slice(cursor).join("\n");
    if (text.trim()) segments.push({ type: "markdown", content: text });
  }

  // No prompt blocks at all — return single markdown segment
  if (segments.length === 0) return [{ type: "markdown", content: markdown }];

  return segments;
}
