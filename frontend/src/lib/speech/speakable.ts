/**
 * Markdown to something worth listening to.
 *
 * Handing raw markdown to a speech synthesiser reads the punctuation out loud —
 * "star star important star star", "backtick use state backtick". The syntax is
 * for eyes; strip it and keep the words.
 *
 * Code is the harder case. A fenced block read aloud is unlistenable and there
 * is no useful way to speak it, so it is announced and skipped: the listener
 * learns something was there and can go look, which is strictly better than
 * either silence or two minutes of punctuation.
 */

/** Spoken in place of a fenced code block. */
function codeBlockMarker(language: string, lines: number): string {
  const lang = language.trim();
  const what = lang ? `${lang} code block` : "code block";
  return lines > 1 ? `[${what}, ${lines} lines]` : `[${what}]`;
}

export function toSpeakableText(markdown: string): string {
  let text = markdown;

  // Fenced code first, before any inline rule can chew on its contents.
  text = text.replace(/```([^\n`]*)\n([\s\S]*?)```/g, (_m, lang: string, body: string) => {
    const lines = body.replace(/\n$/, "").split("\n").length;
    return `\n${codeBlockMarker(lang, lines)}\n`;
  });
  // An unterminated fence — common mid-stream.
  text = text.replace(
    /```([^\n`]*)\n([\s\S]*)$/g,
    (_m, lang: string) => `\n${codeBlockMarker(lang, 1)}\n`,
  );

  // Indented code blocks would be read as prose; they are rare enough in agent
  // output that leaving them is the lesser risk than a greedy indent rule.

  // Images before links: the alt text is the only speakable part.
  text = text.replace(/!\[([^\]]*)\]\([^)]*\)/g, (_m, alt: string) =>
    alt ? `[image: ${alt}]` : "[image]",
  );
  // Links keep their label and drop the URL — nobody wants a URL spelled out.
  text = text.replace(/\[([^\]]+)\]\([^)]*\)/g, "$1");

  // Inline code keeps its content; the backticks were never words.
  text = text.replace(/`([^`\n]+)`/g, "$1");

  // Headings become plain lines. The trailing period gives the synthesiser a
  // pause it would otherwise run straight through.
  text = text.replace(/^\s{0,3}#{1,6}\s+(.*)$/gm, (_m, heading: string) =>
    /[.!?:]$/.test(heading.trim()) ? heading.trim() : `${heading.trim()}.`,
  );

  // Horizontal rules carry nothing spoken.
  text = text.replace(/^\s{0,3}([-*_])\s*(?:\1\s*){2,}$/gm, "");

  // Table separator rows are pure syntax; the cells themselves survive below.
  text = text.replace(/^\s*\|?[\s:|-]*\|[\s:|-]*$/gm, "");
  text = text.replace(/\|/g, " ");

  // Blockquote and list markers.
  text = text.replace(/^\s{0,3}>\s?/gm, "");
  text = text.replace(/^(\s*)[-*+]\s+/gm, "$1");
  text = text.replace(/^(\s*)\d+[.)]\s+/gm, "$1");

  // Emphasis. Bold before italic so ** is consumed as a pair.
  text = text.replace(/\*\*([^*]+)\*\*/g, "$1");
  text = text.replace(/__([^_]+)__/g, "$1");
  text = text.replace(/\*([^*\n]+)\*/g, "$1");
  text = text.replace(/(^|\s)_([^_\n]+)_(?=\s|$)/g, "$1$2");
  text = text.replace(/~~([^~]+)~~/g, "$1");

  // Any raw HTML that made it through.
  text = text.replace(/<[^>]+>/g, " ");

  // Collapse the blank lines the strips left behind.
  text = text.replace(/[ \t]+/g, " ");
  text = text.replace(/\n{3,}/g, "\n\n");

  return text.trim();
}

/**
 * Splits text into utterance-sized chunks.
 *
 * Browsers cut long utterances off — the limit varies and is not documented —
 * so a whole answer handed over in one piece stops partway through. Splitting
 * on sentence boundaries and queueing the pieces is what makes a real answer
 * play to the end.
 */
export function toUtteranceChunks(text: string, maxChars = 200): string[] {
  if (!text) return [];

  const chunks: string[] = [];
  // Split on sentence ends and on blank lines, keeping the terminator.
  const pieces = text.split(/(?<=[.!?:])\s+|\n{2,}/).filter((p) => p.trim() !== "");

  let current = "";
  for (const piece of pieces) {
    const part = piece.trim();
    if (part.length > maxChars) {
      if (current) {
        chunks.push(current);
        current = "";
      }
      // A single over-long sentence still has to be broken somewhere; prefer
      // commas, then whitespace, so the break lands where a reader would pause.
      chunks.push(...hardSplit(part, maxChars));
      continue;
    }
    if (!current) {
      current = part;
    } else if (current.length + 1 + part.length <= maxChars) {
      current += ` ${part}`;
    } else {
      chunks.push(current);
      current = part;
    }
  }
  if (current) chunks.push(current);
  return chunks;
}

function hardSplit(text: string, maxChars: number): string[] {
  const out: string[] = [];
  let rest = text;
  while (rest.length > maxChars) {
    const window = rest.slice(0, maxChars);
    const at = Math.max(
      window.lastIndexOf(", "),
      window.lastIndexOf("; "),
      window.lastIndexOf(" "),
    );
    const cut = at > maxChars * 0.5 ? at : maxChars;
    out.push(rest.slice(0, cut).trim());
    rest = rest.slice(cut).trim();
  }
  if (rest) out.push(rest);
  return out;
}
