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
  /** Set when the block was recovered from malformed markup (e.g. a wrong or
   *  missing closing tag). Surfaced as a non-blocking warning on the card so the
   *  recovery is never silent. Never set for well-formed blocks. */
  warning?: string;
}

export interface SplitOptions {
  /** True when the source message is complete (not mid-stream). Enables recovery
   *  of a block whose closer is entirely missing instead of treating it as a
   *  still-streaming pending card. A present-but-wrong closer (e.g. `</parameter>`)
   *  is recovered regardless. Defaults to false to preserve streaming behavior. */
  isFinal?: boolean;
}

/** Parse title + prompt from a code block's inner text. First line must be `# Title`.
 *  Optional meta lines immediately after the title (any order, blank lines allowed)
 *  configure the block:
 *    - `project: <slug>` targets a different project
 *    - `warning: <message>` flags an auto-recovered block (set by the parser, not users)
 */
export function parsePromptFromCode(code: string): PromptBlock | null {
  const nl = code.indexOf("\n");
  if (nl === -1) return null;
  const heading = code.slice(0, nl).trim();
  if (!heading.startsWith("# ")) return null;
  const title = heading.slice(2).trim();
  let rest = code.slice(nl + 1);

  let projectSlug: string | undefined;
  let warning: string | undefined;
  for (;;) {
    const projectMatch = rest.match(/^\n*project:\s*(\S+)\s*\n/);
    if (projectMatch?.[1]) {
      projectSlug = projectMatch[1];
      rest = rest.slice(projectMatch[0].length);
      continue;
    }
    const warningMatch = rest.match(/^\n*warning:[ \t]*(.+?)[ \t]*\n/);
    if (warningMatch?.[1]) {
      warning = warningMatch[1];
      rest = rest.slice(warningMatch[0].length);
      continue;
    }
    break;
  }

  const prompt = rest.trim();
  if (!title || !prompt) return null;
  return { title, prompt, projectSlug, warning };
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

/** Extract all prompt blocks from raw markdown content. Normalizes
 *  `<agentique type="prompt">` tags first (with recovery) so both authoring
 *  forms are enumerated identically to splitByPromptBlocks. */
export function parsePromptBlocks(markdown: string, opts: SplitOptions = {}): PromptBlock[] {
  const preprocessed = preprocessAgentiqueTags(markdown, opts.isFinal ?? false);
  return findRawPromptBlocks(preprocessed)
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
//
// Both the top-level opener scan and the close matcher skip markdown code
// regions, so tag syntax shown as documentation (inside a fenced or inline code
// span) is preserved verbatim and never converted into a card. A nested
// `<agentique …>…</agentique>` example in a body is consumed as part of the
// outer block, not parsed as its own card.
// ---------------------------------------------------------------------------

const RE_AGENTIQUE_OPEN_ANCHORED = /^<agentique\b([^>]*)>/i;
const RE_AGENTIQUE_CLOSE_ANCHORED = /^<\/agentique>/i;
const AGENTIQUE_CLOSE = "</agentique>";
const RE_ATTR = /([\w-]+)\s*=\s*"([^"]*)"/g;
const RE_FENCE_CLOSE_LINE = /^ {0,3}(`{3,})\s*$/;
// Any closing tag, e.g. </parameter>, </prompt>, </agentique>. Used only by the
// recovery scanner to find a plausible boundary for a malformed block.
const RE_CLOSE_LIKE_ANCHORED = /^<\/[a-zA-Z][\w-]*\s*>/;

/** Advance past a markdown code region beginning at a backtick run at index `i`
 *  (caller guarantees markdown[i] === "`"). Handles both fenced blocks (a ``` run
 *  alone on its line) and inline code spans, so tag-like tokens inside code never
 *  affect block delimitation. Returns the index just past the region; an
 *  unterminated fenced block consumes to EOF, and an unterminated inline span
 *  consumes only its opening run (mirroring CommonMark leniency). */
function skipBackticks(markdown: string, i: number, N: number): number {
  const lineStart = markdown.lastIndexOf("\n", i - 1) + 1;
  const prefix = markdown.slice(lineStart, i);
  let n = 0;
  while (i + n < N && markdown[i + n] === "`") n++;

  const isFenceStart = n >= 3 && /^ {0,3}$/.test(prefix);
  if (isFenceStart) {
    let j = markdown.indexOf("\n", i + n);
    if (j === -1) return N;
    j += 1;
    while (j < N) {
      const eol = markdown.indexOf("\n", j);
      const lineEnd = eol === -1 ? N : eol;
      const line = markdown.slice(j, lineEnd);
      const close = RE_FENCE_CLOSE_LINE.exec(line);
      if (close && (close[1]?.length ?? 0) >= n) {
        return eol === -1 ? N : eol + 1;
      }
      if (eol === -1) return N;
      j = eol + 1;
    }
    return N;
  }

  // Inline code span — find a matching run of exactly n backticks.
  let j = i + n;
  while (j < N) {
    if (markdown[j] === "`") {
      let m = 0;
      while (j + m < N && markdown[j + m] === "`") m++;
      if (m === n) return j + m;
      j += m;
    } else {
      j++;
    }
  }
  return i + n;
}

/** Find the closing `</agentique>` that matches the opener at openEnd.
 *
 *  Delimitation strategy — balanced depth counting over `<agentique ...>` /
 *  `</agentique>` tokens, skipping markdown code regions (fenced blocks and
 *  inline spans). Depth counting lets a body legitimately nest a complete
 *  `<agentique ...>...</agentique>` example without the inner close ending the
 *  outer block, while a body that merely *mentions* the tag inside backticks is
 *  ignored because code regions are skipped. For two adjacent top-level blocks,
 *  the first block's own close brings depth to 0 before the next opener is seen,
 *  so they never bleed together.
 *
 *  Returns the matching position, or null when no balanced close exists
 *  (genuinely unclosed, or a malformed/mismatched closer). The caller decides
 *  whether to recover via findRecoveryBoundary.
 *
 *  Known limitation: an *unbalanced, unescaped* literal `</agentique>` in prose
 *  (more closers than openers, outside any code span) closes the block early.
 *  The authoring guidance instructs models to escape such meta-mentions or place
 *  them in code; combined with code-region skipping this covers the realistic
 *  meta-prompt case. */
function findMatchingAgentiqueClose(markdown: string, openEnd: number): number | null {
  let depth = 1;
  let i = openEnd;
  const N = markdown.length;

  while (i < N) {
    const ch = markdown[i];
    if (ch === "`") {
      i = skipBackticks(markdown, i, N);
      continue;
    }
    if (ch === "<") {
      const rest = markdown.slice(i);
      const openM = RE_AGENTIQUE_OPEN_ANCHORED.exec(rest);
      if (openM) {
        depth++;
        i += openM[0].length;
        continue;
      }
      const closeM = RE_AGENTIQUE_CLOSE_ANCHORED.exec(rest);
      if (closeM) {
        depth--;
        if (depth === 0) return i;
        i += closeM[0].length;
        continue;
      }
    }
    i++;
  }

  return null;
}

/** Find the next top-level `<agentique …>` opener at or after `from`, skipping
 *  matches inside markdown code regions. Tag syntax shown as documentation — a
 *  fenced or inline code example — must never be turned into a card, so the
 *  top-level scan honors code regions exactly like the matcher does. Returns the
 *  opener's absolute index and the anchored match, or null when none remains. */
function findNextAgentiqueOpen(
  markdown: string,
  from: number,
): { index: number; match: RegExpExecArray } | null {
  let i = from;
  const N = markdown.length;
  while (i < N) {
    const ch = markdown[i];
    if (ch === "`") {
      i = skipBackticks(markdown, i, N);
      continue;
    }
    if (ch === "<") {
      const m = RE_AGENTIQUE_OPEN_ANCHORED.exec(markdown.slice(i));
      if (m) return { index: i, match: m };
    }
    i++;
  }
  return null;
}

interface RecoveryBoundary {
  /** End of the recovered body (exclusive). */
  bodyEnd: number;
  /** Position where preprocessing should resume after this block. */
  consumeEnd: number;
  reason: "wrong-closer" | "next-opener" | "eof";
}

/** Locate the most plausible end of a block whose proper `</agentique>` closer is
 *  missing or mistyped. Scans from openEnd with the SAME code-skipping +
 *  agentique-depth tracking as the matcher, so a *balanced* nested block stays
 *  part of the body rather than being mistaken for a sibling.
 *
 *  The "an opener appears inside the body" case is inherently ambiguous (it could
 *  be a nested example, or a sibling block whose predecessor forgot its closer).
 *  It is resolved by what follows:
 *    - a close-like tag (`</…>`, e.g. the mistyped `</parameter>`) at the OUTER
 *      depth wins as the boundary — any openers before it were nested, so the
 *      outer block simply had a wrong closer (incident #1, possibly with nesting);
 *    - otherwise the FIRST outer-depth `<agentique …>` opener is treated as a
 *      sibling boundary, so two adjacent blocks where the first forgot its closer
 *      split into two cards instead of merging;
 *    - otherwise the boundary is end of message.
 *  Either way the recovered block renders as a clickable card with a warning. */
function findRecoveryBoundary(markdown: string, openEnd: number): RecoveryBoundary {
  let i = openEnd;
  const N = markdown.length;
  let depth = 0; // depth of nested agentique blocks below the (unclosed) outer one
  let firstOpener = -1; // first opener seen at the outer depth (sibling candidate)

  while (i < N) {
    const ch = markdown[i];
    if (ch === "`") {
      i = skipBackticks(markdown, i, N);
      continue;
    }
    if (ch === "<") {
      const rest = markdown.slice(i);
      const openM = RE_AGENTIQUE_OPEN_ANCHORED.exec(rest);
      if (openM) {
        if (depth === 0 && firstOpener === -1) firstOpener = i;
        depth++;
        i += openM[0].length;
        continue;
      }
      const aCloseM = RE_AGENTIQUE_CLOSE_ANCHORED.exec(rest);
      if (aCloseM) {
        if (depth > 0) {
          depth--;
          i += aCloseM[0].length;
          continue;
        }
        // Defensive: a balanced outer </agentique> would have been found by the
        // matcher, so this depth-0 case is unreachable in normal flow; treat a
        // stray one as the boundary rather than scanning past it.
        return { bodyEnd: i, consumeEnd: i + aCloseM[0].length, reason: "wrong-closer" };
      }
      // A non-agentique close-like tag only ends the OUTER block at outer depth;
      // inside a nested block it is just body text.
      if (depth === 0) {
        const closeM = RE_CLOSE_LIKE_ANCHORED.exec(rest);
        if (closeM) {
          return { bodyEnd: i, consumeEnd: i + closeM[0].length, reason: "wrong-closer" };
        }
      }
    }
    i++;
  }

  if (firstOpener !== -1) {
    return { bodyEnd: firstOpener, consumeEnd: firstOpener, reason: "next-opener" };
  }
  return { bodyEnd: N, consumeEnd: N, reason: "eof" };
}

const RECOVERY_WARNINGS: Record<RecoveryBoundary["reason"], string> = {
  "wrong-closer": "Malformed close tag — auto-recovered; expected the agentique closing tag.",
  "next-opener": "Missing close tag — auto-recovered at the start of the next prompt block.",
  eof: "Missing close tag — auto-recovered at end of message.",
};

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

function buildFencedPrompt(
  attrs: AgentiqueAttrs,
  body: string,
  closed: boolean,
  warning?: string,
): string {
  const fenceLen = Math.max(3, maxFenceInBody(body) + 1);
  const fence = "`".repeat(fenceLen);
  const lines: string[] = [`${fence}prompt`];
  if (attrs.title) lines.push(`# ${attrs.title}`);
  if (warning) lines.push(`warning: ${warning}`);
  if (attrs.project) lines.push(`project: ${attrs.project}`);
  const trimmed = body.replace(/^\n+|\n+$/g, "");
  if (trimmed) lines.push(trimmed);
  const opener = lines.join("\n");
  return closed ? `${opener}\n${fence}` : opener;
}

/** Pre-process `<agentique type="prompt" ...>` tags into ```prompt fenced
 *  blocks so the existing prompt-block pipeline can handle them. Other
 *  `<agentique type="...">` values are left untouched for future features.
 *
 *  A well-formed (balanced) block is rewritten to a closed ```prompt block. A
 *  block with a malformed/missing closer is recovered (see findRecoveryBoundary)
 *  and rewritten to a closed block carrying a `warning:` meta line — so it still
 *  renders as a clickable card instead of leaking as plain text. Only a block
 *  that is genuinely unclosed at EOF *and* not final (still streaming) is left as
 *  an open opener for the pending-card path. */
export function preprocessAgentiqueTags(markdown: string, isFinal = false): string {
  let result = "";
  let cursor = 0;

  while (cursor < markdown.length) {
    const found = findNextAgentiqueOpen(markdown, cursor);
    if (!found) {
      result += markdown.slice(cursor);
      break;
    }

    const openStart = found.index;
    const openMatch = found.match;
    const openEnd = openStart + openMatch[0].length;
    const attrs = parseAttrs(openMatch[1] ?? "");

    // Only handle type="prompt" for now; pass others through unchanged.
    if (attrs.type !== "prompt") {
      result += markdown.slice(cursor, openEnd);
      cursor = openEnd;
      continue;
    }

    result += markdown.slice(cursor, openStart);
    const leadingPrefix =
      result.endsWith("\n\n") || result.length === 0 ? "" : result.endsWith("\n") ? "\n" : "\n\n";

    const closeStart = findMatchingAgentiqueClose(markdown, openEnd);
    if (closeStart !== null) {
      // Well-formed, balanced close.
      const body = markdown.slice(openEnd, closeStart);
      result += `${leadingPrefix}${buildFencedPrompt(attrs, body, true)}\n`;
      cursor = closeStart + AGENTIQUE_CLOSE.length;
      continue;
    }

    // No balanced `</agentique>`. Either a malformed/missing closer to recover,
    // or a still-streaming block. A pure end-of-message boundary is recovered
    // only when the message is final; a wrong closer or a following opener gives
    // a concrete boundary and is recovered regardless of stream state.
    const recovery = findRecoveryBoundary(markdown, openEnd);
    if (recovery.reason !== "eof" || isFinal) {
      const body = markdown.slice(openEnd, recovery.bodyEnd);
      result += `${leadingPrefix}${buildFencedPrompt(attrs, body, true, RECOVERY_WARNINGS[recovery.reason])}\n`;
      cursor = recovery.consumeEnd;
      continue;
    }

    // Streaming — emit an unclosed ```prompt opener so findPendingPromptBlock
    // detects it during pending render.
    const body = markdown.slice(openEnd);
    result += `${leadingPrefix}${buildFencedPrompt(attrs, body, false)}`;
    cursor = markdown.length;
    break;
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
 *  and the fenced form is kept as a legacy/fallback.
 *
 *  Pass `{ isFinal: true }` for completed (non-streaming) messages so a block
 *  whose closer is missing entirely is recovered into a card instead of a
 *  perpetual pending placeholder. */
export function splitByPromptBlocks(
  rawMarkdown: string,
  opts: SplitOptions = {},
): ContentSegment[] {
  const markdown = preprocessAgentiqueTags(rawMarkdown, opts.isFinal ?? false);
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
