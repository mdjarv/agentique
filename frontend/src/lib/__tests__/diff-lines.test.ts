import { describe, expect, it } from "vitest";
import {
  diffNote,
  hasRenderableDiff,
  lineContent,
  lineNumber,
  parseDiffLines,
} from "~/lib/diff-lines";
import { appendQuote, buildDiffQuote, quoteLabel } from "~/lib/diff-quote";

const PATCH = `diff --git a/lib/agent-runs.ts b/lib/agent-runs.ts
index 1111111..2222222 100644
--- a/lib/agent-runs.ts
+++ b/lib/agent-runs.ts
@@ -160,4 +160,5 @@ export function collectAgentRuns(
   const tasks = tasksByToolUse.get(toolUseId) ?? [];
   const spawn = spawns.get(toolUseId);
-  const input = readInput(spawn?.use.toolInput);
+  if (!isSubagentRun(kind, spawn?.use.toolName)) continue;
+  const input = readInput(spawn?.use.toolInput);

`;

describe("parseDiffLines", () => {
  it("numbers each side from the hunk header", () => {
    const lines = parseDiffLines(PATCH);
    const hunk = lines.findIndex((l) => l.kind === "hunk");

    expect(lines[hunk]?.text).toContain("@@ -160,4 +160,5 @@");
    expect(lines[hunk + 1]).toMatchObject({ kind: "context", oldNo: 160, newNo: 160 });
    expect(lines[hunk + 2]).toMatchObject({ kind: "context", oldNo: 161, newNo: 161 });
    // A deletion consumes an old line and no new one.
    expect(lines[hunk + 3]).toEqual({
      kind: "del",
      oldNo: 162,
      text: "-  const input = readInput(spawn?.use.toolInput);",
    });
    // Both additions land on new lines 162 and 163 — the old side stands still.
    expect(lines[hunk + 4]).toMatchObject({ kind: "add", newNo: 162 });
    expect(lines[hunk + 4]).not.toHaveProperty("oldNo");
    expect(lines[hunk + 5]).toMatchObject({ kind: "add", newNo: 163 });
    expect(lines[hunk + 6]).toMatchObject({ kind: "context", oldNo: 163, newNo: 164 });
  });

  it("keeps everything before the first hunk as meta", () => {
    const lines = parseDiffLines(PATCH);
    const before = lines.slice(
      0,
      lines.findIndex((l) => l.kind === "hunk"),
    );

    expect(before.every((l) => l.kind === "meta")).toBe(true);
    expect(before).toHaveLength(4);
  });

  it("treats the no-newline marker as meta, numbering nothing", () => {
    const lines = parseDiffLines("@@ -1,1 +1,1 @@\n-a\n+b\n\\ No newline at end of file\n");

    expect(lines.at(-1)).toEqual({ kind: "meta", text: "\\ No newline at end of file" });
    expect(lines.filter((l) => l.kind === "add")).toHaveLength(1);
  });

  it("drops only git's trailing newline, not a real blank line", () => {
    const lines = parseDiffLines("@@ -1,2 +1,2 @@\n a\n\n");

    expect(lines.map((l) => l.kind)).toEqual(["hunk", "context", "context"]);
    expect(lineContent(lines[2] as never)).toBe("");
  });

  it("reports a binary file as having nothing to render, and says why", () => {
    const binary =
      "diff --git a/logo.png b/logo.png\nindex 111..222 100644\nBinary files a/logo.png and b/logo.png differ\n";
    const lines = parseDiffLines(binary);

    expect(hasRenderableDiff(lines)).toBe(false);
    expect(diffNote(lines)).toBe("Binary files a/logo.png and b/logo.png differ");
  });

  it("strips git's marker from a line's content", () => {
    const lines = parseDiffLines("@@ -1,1 +1,2 @@\n-old\n+new\n context\n");

    expect(lines.map(lineContent)).toEqual(["@@ -1,1 +1,2 @@", "old", "new", "context"]);
  });

  it("numbers a deletion by the old file, since it has no new line", () => {
    const lines = parseDiffLines("@@ -7,1 +7,0 @@\n-gone\n");

    expect(lineNumber(lines[1] as never)).toBe(7);
  });
});

describe("buildDiffQuote", () => {
  const lines = parseDiffLines(PATCH);
  const hunk = lines.findIndex((l) => l.kind === "hunk");

  it("labels one line and a range differently", () => {
    expect(quoteLabel("a.ts", [lines[hunk + 1] as never])).toBe("a.ts:160");
    expect(quoteLabel("a.ts", lines.slice(hunk + 1, hunk + 3) as never)).toBe("a.ts:160-161");
  });

  it("fences the selection and carries the enclosing hunk header in", () => {
    const quote = buildDiffQuote("lib/agent-runs.ts", lines, hunk + 3, hunk + 4);

    expect(quote).toBe(
      "lib/agent-runs.ts:162\n\n```diff\n" +
        "@@ -160,4 +160,5 @@ export function collectAgentRuns(\n" +
        "-  const input = readInput(spawn?.use.toolInput);\n" +
        "+  if (!isSubagentRun(kind, spawn?.use.toolName)) continue;\n" +
        "```\n",
    );
  });

  it("does not repeat the hunk header when it is already selected", () => {
    const quote = buildDiffQuote("a.ts", lines, hunk, hunk + 1);

    expect(quote.match(/@@ -160,4/gu)).toHaveLength(1);
  });

  it("reads a selection made bottom-up the same as one made top-down", () => {
    expect(buildDiffQuote("a.ts", lines, hunk + 4, hunk + 2)).toBe(
      buildDiffQuote("a.ts", lines, hunk + 2, hunk + 4),
    );
  });

  it("returns nothing for a range outside the diff", () => {
    expect(buildDiffQuote("a.ts", lines, 500, 600)).toBe("");
  });
});

describe("appendQuote", () => {
  it("is the whole text when the composer is empty", () => {
    expect(appendQuote("", "a.ts:1\n")).toBe("a.ts:1\n");
    expect(appendQuote("   \n", "a.ts:1\n")).toBe("a.ts:1\n");
  });

  it("leaves what the reader typed alone, one blank line above the quote", () => {
    expect(appendQuote("why is this here?", "a.ts:1\n")).toBe("why is this here?\n\na.ts:1\n");
  });

  it("does not stack blank lines when the composer already ends in one", () => {
    expect(appendQuote("first\n\n", "a.ts:1\n")).toBe("first\n\na.ts:1\n");
  });
});
