import { describe, expect, it } from "vitest";
import {
  findRawPromptBlocks,
  parsePromptBlocks,
  splitByPromptBlocks,
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
// splitByPromptBlocks
// ---------------------------------------------------------------------------

describe("splitByPromptBlocks", () => {
  it("returns single markdown segment when no prompt blocks exist", () => {
    const md = "# Hello\n\nSome text.\n\n```python\ncode\n```";
    const segments = splitByPromptBlocks(md);
    expect(segments).toEqual([{ type: "markdown", content: md }]);
  });

  it("splits text before, prompt, and text after", () => {
    const md = [
      "Before text.",
      "",
      "```prompt",
      "# My Task",
      "Do the thing.",
      "```",
      "",
      "After text.",
    ].join("\n");
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(3);
    expect(segments[0]).toEqual({ type: "markdown", content: "Before text.\n" });
    expect(segments[1]).toEqual({
      type: "prompt",
      block: { title: "My Task", prompt: "Do the thing." },
    });
    expect(segments[2]).toEqual({ type: "markdown", content: "\nAfter text." });
  });

  it("handles prompt-only content (no surrounding text)", () => {
    const md = "```prompt\n# Title\nDo it.\n```";
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(1);
    expect(segments[0]?.type).toBe("prompt");
  });

  it("handles multiple prompts with text between", () => {
    const md = [
      "Here are 2 tasks:",
      "",
      "```prompt",
      "# First",
      "Do A.",
      "```",
      "",
      "And also:",
      "",
      "```prompt",
      "# Second",
      "Do B.",
      "```",
    ].join("\n");
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(4);
    expect(segments[0]?.type).toBe("markdown");
    expect(segments[1]).toEqual({ type: "prompt", block: { title: "First", prompt: "Do A." } });
    expect(segments[2]?.type).toBe("markdown");
    expect(segments[3]).toEqual({ type: "prompt", block: { title: "Second", prompt: "Do B." } });
  });

  it("handles prompts with nested code fences", () => {
    const md = [
      "```prompt",
      "# Fix API",
      "```go",
      "func main() {}",
      "```",
      "Run tests.",
      "```",
    ].join("\n");
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(1);
    expect(segments[0]?.type).toBe("prompt");
    if (segments[0]?.type === "prompt") {
      expect(segments[0].block.prompt).toContain("```go");
      expect(segments[0].block.prompt).toContain("Run tests.");
    }
  });

  it("handles prompts with indented inner fences", () => {
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
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(1);
    expect(segments[0]?.type).toBe("prompt");
    if (segments[0]?.type === "prompt") {
      expect(segments[0].block.prompt).toContain("Run tests.");
    }
  });

  it("skips empty text segments between consecutive prompts", () => {
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
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(2);
    expect(segments[0]?.type).toBe("prompt");
    expect(segments[1]?.type).toBe("prompt");
  });

  it("includes project slug from metadata", () => {
    const md = "```prompt\n# Task\nproject: my-proj\nDo it.\n```";
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(1);
    if (segments[0]?.type === "prompt") {
      expect(segments[0].block.projectSlug).toBe("my-proj");
    }
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
