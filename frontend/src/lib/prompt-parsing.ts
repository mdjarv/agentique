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
const RE_FENCE_INDENT = /^( {0,3})(`{3,})(.*)$/;

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
// Nested-fence repair — agents commonly wrap quoted prompts in a bare ```
// fence that itself contains ```yaml/```go/etc. CommonMark closes the outer
// fence at the first inner bare ```, breaking the rest of the message (the
// agent's intended outer-closer becomes an unclosed fence at EOF). We repair
// these by upgrading the outer fence length (e.g. ``` → ````) so it survives
// the nested fences.
// ---------------------------------------------------------------------------

/** Lookahead used inside a wrapper to decide whether a bare ``` should open
 *  a sub-block (rather than close the wrapper). Mirrors shouldOpenInnerBlock
 *  but doesn't stop at prompt openers — generic wrappers have no such marker. */
function shouldOpenInnerBlockGeneric(lines: string[], currentIndex: number): boolean {
  let bare = 0;
  let info = 0;
  for (let j = currentIndex + 1; j < lines.length; j++) {
    const line = lines[j] ?? "";
    if (RE_BARE_FENCE.test(line)) bare++;
    else if (RE_INFO_FENCE.test(line)) info++;
  }
  return bare - info >= 2;
}

/** Repair markdown where outer fences would be prematurely closed by nested
 *  fences. Walks each fenced block, finds its intended close via the same
 *  insideInner + lookahead state machine used for prompt blocks, and upgrades
 *  the outer fence length to maxInnerFence + 1 when needed. */
export function repairNestedFences(markdown: string): string {
  const lines = markdown.split("\n");
  const result = lines.slice();
  let i = 0;

  while (i < lines.length) {
    const line = lines[i] ?? "";
    const bareOpen = RE_BARE_FENCE.exec(line);
    const infoOpen = RE_INFO_FENCE.exec(line);
    const openerLen = (infoOpen ?? bareOpen)?.[1]?.length ?? 0;
    if (!openerLen) {
      i++;
      continue;
    }

    let insideInner = false;
    let innerFenceLen = 0;
    let maxInnerFence = 0;
    let closeLine = -1;

    for (let j = i + 1; j < lines.length; j++) {
      const l = lines[j] ?? "";
      const bareMatch = RE_BARE_FENCE.exec(l);
      const infoMatch = RE_INFO_FENCE.exec(l);

      if (insideInner) {
        if (bareMatch?.[1] && bareMatch[1].length >= innerFenceLen) {
          insideInner = false;
          maxInnerFence = Math.max(maxInnerFence, bareMatch[1].length);
        }
        continue;
      }

      if (bareMatch?.[1]) {
        if (shouldOpenInnerBlockGeneric(lines, j)) {
          insideInner = true;
          innerFenceLen = bareMatch[1].length;
          maxInnerFence = Math.max(maxInnerFence, bareMatch[1].length);
        } else if (bareMatch[1].length >= openerLen) {
          closeLine = j;
          break;
        }
      } else if (infoMatch?.[1]) {
        insideInner = true;
        innerFenceLen = infoMatch[1].length;
        maxInnerFence = Math.max(maxInnerFence, infoMatch[1].length);
      }
    }

    if (closeLine !== -1 && maxInnerFence >= openerLen) {
      const newLen = maxInnerFence + 1;
      const newFence = "`".repeat(newLen);
      const openMatch = RE_FENCE_INDENT.exec(line);
      if (openMatch) {
        result[i] = (openMatch[1] ?? "") + newFence + (openMatch[3] ?? "");
      }
      const closeLineText = lines[closeLine] ?? "";
      const closeIndent = RE_BARE_FENCE.exec(closeLineText)?.[0]?.match(/^( {0,3})/)?.[1] ?? "";
      result[closeLine] = closeIndent + newFence;
    }

    i = closeLine !== -1 ? closeLine + 1 : i + 1;
  }

  return result.join("\n");
}

// ---------------------------------------------------------------------------
// <agentique type="prompt"> tag pre-processor
//
// Newer authoring format that avoids fence-nesting issues entirely. Closed
// tags are rewritten to ```prompt fenced blocks (with an upgraded fence length
// when the body contains code) so the rest of the pipeline can stay
// fence-based. An unclosed trailing tag becomes an unclosed ```prompt opener
// so the existing pending_prompt detection picks it up during streaming.
// ---------------------------------------------------------------------------

const RE_AGENTIQUE_OPEN = /<agentique\b([^>]*)>/i;
const RE_AGENTIQUE_CLOSE = /<\/agentique>/i;
const AGENTIQUE_CLOSE = "</agentique>";
const RE_ATTR = /([\w-]+)\s*=\s*"([^"]*)"/g;

/** Find the closing `</agentique>` that matches the opener at openEnd,
 *  tracking nesting depth. Returns the position of the matching `</agentique>`,
 *  or null if unclosed (streaming). Mirrors a balanced-bracket scan so a
 *  prompt body that itself mentions `<agentique ...>` tags doesn't close
 *  the outer prematurely. */
function findMatchingAgentiqueClose(markdown: string, openEnd: number): number | null {
  let depth = 1;
  let cursor = openEnd;

  while (cursor < markdown.length) {
    const remaining = markdown.slice(cursor);
    const openMatch = RE_AGENTIQUE_OPEN.exec(remaining);
    const closeMatch = RE_AGENTIQUE_CLOSE.exec(remaining);
    if (!closeMatch) return null;

    const openIdx = openMatch ? openMatch.index : Number.POSITIVE_INFINITY;
    const closeIdx = closeMatch.index;

    if (openIdx < closeIdx && openMatch) {
      depth++;
      cursor += openIdx + openMatch[0].length;
    } else {
      depth--;
      if (depth === 0) return cursor + closeIdx;
      cursor += closeIdx + closeMatch[0].length;
    }
  }

  return null;
}

interface AgentiqueAttrs {
  type?: string;
  title?: string;
  project?: string;
}

function parseAttrs(raw: string): AgentiqueAttrs {
  const attrs: AgentiqueAttrs = {};
  for (const m of raw.matchAll(RE_ATTR)) {
    const key = m[1]?.toLowerCase();
    const value = m[2];
    if (!key || value === undefined) continue;
    if (key === "type") attrs.type = value;
    else if (key === "title") attrs.title = value;
    else if (key === "project") attrs.project = value;
  }
  return attrs;
}

function maxFenceInBody(body: string): number {
  let max = 0;
  for (const m of body.matchAll(/^ {0,3}(`{3,})/gm)) {
    max = Math.max(max, m[1]?.length ?? 0);
  }
  return max;
}

function buildFencedPrompt(attrs: AgentiqueAttrs, body: string, closed: boolean): string {
  const fenceLen = Math.max(3, maxFenceInBody(body) + 1);
  const fence = "`".repeat(fenceLen);
  const lines: string[] = [`${fence}prompt`];
  if (attrs.title) lines.push(`# ${attrs.title}`);
  if (attrs.project) lines.push(`project: ${attrs.project}`);
  const trimmed = body.replace(/^\n+|\n+$/g, "");
  if (trimmed) lines.push(trimmed);
  const opener = lines.join("\n");
  return closed ? `${opener}\n${fence}` : opener;
}

/** Pre-process `<agentique type="prompt" ...>` tags into ```prompt fenced
 *  blocks so the existing prompt-block pipeline can handle them. Other
 *  `<agentique type="...">` values are left untouched for future features. */
export function preprocessAgentiqueTags(markdown: string): string {
  let result = "";
  let cursor = 0;

  while (cursor < markdown.length) {
    const remaining = markdown.slice(cursor);
    const openMatch = RE_AGENTIQUE_OPEN.exec(remaining);
    if (!openMatch) {
      result += remaining;
      break;
    }

    const openStart = cursor + openMatch.index;
    const openEnd = openStart + openMatch[0].length;
    const attrs = parseAttrs(openMatch[1] ?? "");

    // Only handle type="prompt" for now; pass others through unchanged.
    if (attrs.type !== "prompt") {
      result += markdown.slice(cursor, openEnd);
      cursor = openEnd;
      continue;
    }

    result += markdown.slice(cursor, openStart);

    const closeStart = findMatchingAgentiqueClose(markdown, openEnd);
    const leadingPrefix =
      result.endsWith("\n\n") || result.length === 0 ? "" : result.endsWith("\n") ? "\n" : "\n\n";

    if (closeStart === null) {
      // Unclosed (streaming) — emit an unclosed ```prompt opener so
      // findPendingPromptBlock detects it during pending render.
      const body = markdown.slice(openEnd);
      result += `${leadingPrefix}${buildFencedPrompt(attrs, body, false)}`;
      cursor = markdown.length;
      break;
    }

    const body = markdown.slice(openEnd, closeStart);
    const closeEnd = closeStart + AGENTIQUE_CLOSE.length;
    result += `${leadingPrefix}${buildFencedPrompt(attrs, body, true)}\n`;
    cursor = closeEnd;
  }

  return result;
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
 *  reach the markdown renderer, eliminating all fence-nesting issues.
 *  Markdown segments are passed through repairNestedFences so agent-authored
 *  prose-quoted code blocks render correctly even when nested.
 *
 *  `<agentique type="prompt" ...>` tags are normalized to ```prompt fenced
 *  blocks before parsing — the XML form is the preferred authoring syntax
 *  and the fenced form is kept as a legacy/fallback. */
export function splitByPromptBlocks(rawMarkdown: string): ContentSegment[] {
  const markdown = preprocessAgentiqueTags(rawMarkdown);
  const rawBlocks = findRawPromptBlocks(markdown);
  const lines = markdown.split("\n");
  const segments: ContentSegment[] = [];
  let cursor = 0;

  const pushMarkdown = (text: string) => {
    if (!text.trim()) return;
    segments.push({ type: "markdown", content: repairNestedFences(text) });
  };

  for (const raw of rawBlocks) {
    if (raw.startLine > cursor) {
      pushMarkdown(lines.slice(cursor, raw.startLine).join("\n"));
    }

    const parsed = parsePromptFromCode(raw.content);
    if (parsed) segments.push({ type: "prompt", block: parsed });

    cursor = raw.endLine + 1;
  }

  // Detect trailing unclosed ```prompt block (streaming in progress)
  const pending = findPendingPromptBlock(lines, rawBlocks);
  if (pending && pending.startLine >= cursor) {
    if (pending.startLine > cursor) {
      pushMarkdown(lines.slice(cursor, pending.startLine).join("\n"));
    }
    const firstLine = pending.content.split("\n")[0]?.trim() ?? "";
    const title = firstLine.startsWith("# ") ? firstLine.slice(2).trim() : undefined;
    segments.push({ type: "pending_prompt", content: pending.content, title });
    return segments;
  }

  if (cursor < lines.length) {
    pushMarkdown(lines.slice(cursor).join("\n"));
  }

  // No prompt blocks at all — return single (repaired) markdown segment
  if (segments.length === 0) {
    return [{ type: "markdown", content: repairNestedFences(markdown) }];
  }

  return segments;
}
