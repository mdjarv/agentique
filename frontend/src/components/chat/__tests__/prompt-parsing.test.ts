import { describe, expect, it } from "vitest";
import {
  findRawPromptBlocks,
  normalizePromptFences,
  parsePromptBlocks,
} from "~/components/chat/PromptCard";

// ---------------------------------------------------------------------------
// findRawPromptBlocks
// ---------------------------------------------------------------------------

describe("findRawPromptBlocks", () => {
  it("finds a simple prompt block with no inner fences", () => {
    const md = "```prompt\n# Title\nDo the thing.\n```";
    const blocks = findRawPromptBlocks(md);
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.content).toBe("# Title\nDo the thing.");
    expect(blocks[0]?.fenceLen).toBe(3);
    expect(blocks[0]?.maxInnerFence).toBe(0);
  });

  it("finds a prompt block containing an inner code fence", () => {
    const md = [
      "```prompt",
      "# Fix API",
      "```go",
      "func main() {}",
      "```",
      "Run tests.",
      "```",
    ].join("\n");
    const blocks = findRawPromptBlocks(md);
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.content).toBe("# Fix API\n```go\nfunc main() {}\n```\nRun tests.");
    expect(blocks[0]?.maxInnerFence).toBe(3);
  });

  it("finds a prompt block with multiple inner code blocks", () => {
    const md = [
      "```prompt",
      "# Multi",
      "```python",
      "print('hi')",
      "```",
      "then",
      "```js",
      "console.log('hi')",
      "```",
      "done",
      "```",
    ].join("\n");
    const blocks = findRawPromptBlocks(md);
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.maxInnerFence).toBe(3);
    expect(blocks[0]?.content).toContain("```python");
    expect(blocks[0]?.content).toContain("```js");
    expect(blocks[0]?.content).toContain("done");
  });

  it("handles two consecutive prompt blocks", () => {
    const md = [
      "```prompt",
      "# First",
      "Do A.",
      "```",
      "text between",
      "```prompt",
      "# Second",
      "Do B.",
      "```",
    ].join("\n");
    const blocks = findRawPromptBlocks(md);
    expect(blocks).toHaveLength(2);
    expect(blocks[0]?.content).toBe("# First\nDo A.");
    expect(blocks[1]?.content).toBe("# Second\nDo B.");
  });

  it("skips unclosed prompt blocks", () => {
    const md = "```prompt\n# Broken\nNo closing fence.";
    const blocks = findRawPromptBlocks(md);
    expect(blocks).toHaveLength(0);
  });

  it("ignores non-prompt code blocks", () => {
    const md = "```python\nprint('hi')\n```";
    const blocks = findRawPromptBlocks(md);
    expect(blocks).toHaveLength(0);
  });

  it("handles opening fence with more than 3 backticks", () => {
    const md = "````prompt\n# Title\nContent.\n````";
    const blocks = findRawPromptBlocks(md);
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.fenceLen).toBe(4);
  });

  it("tracks inner fence backtick count correctly", () => {
    const md = ["```prompt", "# Title", "````python", "code", "````", "```"].join("\n");
    const blocks = findRawPromptBlocks(md);
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.maxInnerFence).toBe(4);
  });

  it("handles a bare inner code block (no language tag)", () => {
    const md = [
      "```prompt",
      "# Title",
      "Format:",
      "```",
      "[Message from agent]",
      "```",
      "Done.",
      "```",
    ].join("\n");
    const blocks = findRawPromptBlocks(md);
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.content).toContain("[Message from agent]");
    expect(blocks[0]?.content).toContain("Done.");
    expect(blocks[0]?.maxInnerFence).toBe(3);
  });

  it("handles a bare inner block followed by an info-string block", () => {
    const md = [
      "```prompt",
      "# Title",
      "```",
      "plain code",
      "```",
      "```go",
      "func main() {}",
      "```",
      "```",
    ].join("\n");
    const blocks = findRawPromptBlocks(md);
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.content).toContain("plain code");
    expect(blocks[0]?.content).toContain("```go");
    expect(blocks[0]?.content).toContain("func main() {}");
  });

  it("handles multiple consecutive bare inner blocks", () => {
    const md = ["```prompt", "# Title", "```", "block1", "```", "```", "block2", "```", "```"].join(
      "\n",
    );
    const blocks = findRawPromptBlocks(md);
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.content).toContain("block1");
    expect(blocks[0]?.content).toContain("block2");
  });

  it("handles indented inner code fences (CommonMark allows 0-3 spaces)", () => {
    const md = [
      "```prompt",
      "# Backend tests",
      "Extract interfaces:",
      "   ```go",
      "   type Foo interface {}",
      "   ```",
      "Run tests.",
      "```",
    ].join("\n");
    const blocks = findRawPromptBlocks(md);
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.content).toContain("```go");
    expect(blocks[0]?.content).toContain("Run tests.");
    expect(blocks[0]?.maxInnerFence).toBe(3);
  });

  it("tracks maxInnerFence from info fence openers", () => {
    const md = ["```prompt", "# Title", "````go", "code", "````", "```"].join("\n");
    const blocks = findRawPromptBlocks(md);
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.maxInnerFence).toBe(4);
  });
});

// ---------------------------------------------------------------------------
// normalizePromptFences
// ---------------------------------------------------------------------------

describe("normalizePromptFences", () => {
  it("returns input unchanged when no prompt blocks exist", () => {
    const md = "# Hello\n\nSome text.\n\n```python\ncode\n```";
    expect(normalizePromptFences(md)).toBe(md);
  });

  it("returns input unchanged when no inner fences conflict", () => {
    const md = "```prompt\n# Title\nNo code here.\n```";
    expect(normalizePromptFences(md)).toBe(md);
  });

  it("lengthens outer fence when inner has 3 backticks", () => {
    const md = [
      "```prompt",
      "# Fix API",
      "```go",
      "func main() {}",
      "```",
      "Run tests.",
      "```",
    ].join("\n");
    const result = normalizePromptFences(md);
    expect(result).toBe(
      ["````prompt", "# Fix API", "```go", "func main() {}", "```", "Run tests.", "````"].join(
        "\n",
      ),
    );
  });

  it("lengthens to maxInnerFence + 1", () => {
    const md = ["```prompt", "# Title", "````python", "code", "````", "```"].join("\n");
    const result = normalizePromptFences(md);
    expect(result).toBe(
      ["`````prompt", "# Title", "````python", "code", "````", "`````"].join("\n"),
    );
  });

  it("preserves surrounding content", () => {
    const md = [
      "Before text.",
      "",
      "```prompt",
      "# Title",
      "```go",
      "code",
      "```",
      "```",
      "",
      "After text.",
    ].join("\n");
    const result = normalizePromptFences(md);
    expect(result).toBe(
      [
        "Before text.",
        "",
        "````prompt",
        "# Title",
        "```go",
        "code",
        "```",
        "````",
        "",
        "After text.",
      ].join("\n"),
    );
  });

  it("lengthens outer fence when bare inner block has same backtick count", () => {
    const md = ["```prompt", "# Title", "```", "plain code", "```", "Done.", "```"].join("\n");
    const result = normalizePromptFences(md);
    expect(result).toBe(
      ["````prompt", "# Title", "```", "plain code", "```", "Done.", "````"].join("\n"),
    );
  });

  it("lengthens outer fence when indented inner fence conflicts", () => {
    const md = [
      "```prompt",
      "# Backend tests",
      "Extract interfaces:",
      "   ```go",
      "   type Foo interface {}",
      "   ```",
      "Run tests.",
      "```",
    ].join("\n");
    const result = normalizePromptFences(md);
    expect(result).toBe(
      [
        "````prompt",
        "# Backend tests",
        "Extract interfaces:",
        "   ```go",
        "   type Foo interface {}",
        "   ```",
        "Run tests.",
        "````",
      ].join("\n"),
    );
  });

  it("normalizes only blocks that need it", () => {
    const md = [
      "```prompt",
      "# Simple",
      "No fences.",
      "```",
      "```prompt",
      "# Complex",
      "```python",
      "code",
      "```",
      "```",
    ].join("\n");
    const result = normalizePromptFences(md);
    expect(result).toBe(
      [
        "```prompt",
        "# Simple",
        "No fences.",
        "```",
        "````prompt",
        "# Complex",
        "```python",
        "code",
        "```",
        "````",
      ].join("\n"),
    );
  });
});

// ---------------------------------------------------------------------------
// parsePromptBlocks (regression — now uses state machine internally)
// ---------------------------------------------------------------------------

describe("parsePromptBlocks", () => {
  it("parses a simple prompt block", () => {
    const md = "```prompt\n# My Task\nDo the thing.\n```";
    const blocks = parsePromptBlocks(md);
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.title).toBe("My Task");
    expect(blocks[0]?.prompt).toBe("Do the thing.");
    expect(blocks[0]?.projectSlug).toBeUndefined();
  });

  it("parses project metadata", () => {
    const md = "```prompt\n# Task\nproject: my-proj\nDo it.\n```";
    const blocks = parsePromptBlocks(md);
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.projectSlug).toBe("my-proj");
    expect(blocks[0]?.prompt).toBe("Do it.");
  });

  it("extracts full prompt through nested fences", () => {
    const md = [
      "```prompt",
      "# Fix API",
      "Refactor:",
      "```go",
      "func main() {}",
      "```",
      "Make sure tests pass.",
      "```",
    ].join("\n");
    const blocks = parsePromptBlocks(md);
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.title).toBe("Fix API");
    expect(blocks[0]?.prompt).toContain("```go");
    expect(blocks[0]?.prompt).toContain("Make sure tests pass.");
  });

  it("extracts full prompt through bare inner fences", () => {
    const md = [
      "```prompt",
      "# Fix API",
      "Format:",
      "```",
      "[Message from agent]",
      "```",
      "Make sure tests pass.",
      "```",
    ].join("\n");
    const blocks = parsePromptBlocks(md);
    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.title).toBe("Fix API");
    expect(blocks[0]?.prompt).toContain("[Message from agent]");
    expect(blocks[0]?.prompt).toContain("Make sure tests pass.");
  });

  it("finds multiple prompt blocks", () => {
    const md = [
      "```prompt",
      "# First",
      "Do A.",
      "```",
      "```prompt",
      "# Second",
      "Do B.",
      "```",
    ].join("\n");
    const blocks = parsePromptBlocks(md);
    expect(blocks).toHaveLength(2);
    expect(blocks[0]?.title).toBe("First");
    expect(blocks[1]?.title).toBe("Second");
  });
});
