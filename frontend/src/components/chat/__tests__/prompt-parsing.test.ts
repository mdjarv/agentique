import { describe, expect, it } from "vitest";
import {
  findRawPromptBlocks,
  parsePromptBlocks,
  preprocessAgentiqueTags,
  repairNestedFences,
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

  it("returns pending_prompt for unclosed prompt block", () => {
    const md = "Some text.\n\n```prompt\n# My Task\nPartial content";
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(2);
    expect(segments[0]?.type).toBe("markdown");
    expect(segments[1]?.type).toBe("pending_prompt");
    if (segments[1]?.type === "pending_prompt") {
      expect(segments[1].title).toBe("My Task");
      expect(segments[1].content).toContain("Partial content");
    }
  });

  it("returns pending_prompt with no title when only opener is present", () => {
    const md = "```prompt\n";
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(1);
    expect(segments[0]?.type).toBe("pending_prompt");
    if (segments[0]?.type === "pending_prompt") {
      expect(segments[0].title).toBeUndefined();
    }
  });

  it("handles completed block followed by incomplete block", () => {
    const md = "```prompt\n# Done\nDo it.\n```\n\n```prompt\n# WIP\nPartial";
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(2);
    expect(segments[0]?.type).toBe("prompt");
    expect(segments[1]?.type).toBe("pending_prompt");
    if (segments[1]?.type === "pending_prompt") {
      expect(segments[1].title).toBe("WIP");
    }
  });

  it("does not produce pending_prompt for non-prompt code blocks", () => {
    const md = "Some text.\n\n```python\ncode";
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(1);
    expect(segments[0]?.type).toBe("markdown");
  });
});

// ---------------------------------------------------------------------------
// repairNestedFences
// ---------------------------------------------------------------------------

describe("repairNestedFences", () => {
  it("leaves a simple code block alone", () => {
    const md = "```python\nprint('hi')\n```";
    expect(repairNestedFences(md)).toBe(md);
  });

  it("leaves consecutive code blocks alone", () => {
    const md = ["```python", "code1", "```", "```js", "code2", "```"].join("\n");
    expect(repairNestedFences(md)).toBe(md);
  });

  it("leaves regular prose alone", () => {
    const md = "Just some prose with `inline code` and no fences.";
    expect(repairNestedFences(md)).toBe(md);
  });

  it("upgrades a bare ``` wrapper containing a ```yaml block", () => {
    const md = ["```", "Some content", "```yaml", "key: value", "```", "More content", "```"].join(
      "\n",
    );
    const repaired = repairNestedFences(md);
    // Outer fence should be upgraded to 4 backticks
    expect(repaired).toBe(
      ["````", "Some content", "```yaml", "key: value", "```", "More content", "````"].join("\n"),
    );
  });

  it("reproduces the failing chat message: prose-quoted prompt wrapping ```yaml", () => {
    const md = [
      "Here's a self-contained prompt you can paste.",
      "",
      "```",
      "# meta-spec design review",
      "",
      "Some context paragraph.",
      "",
      "```yaml",
      "repositories:",
      "  - name: psp-api",
      "```",
      "",
      "Everything else stays prose.",
      "```",
      "",
      "Two notes for the back-and-forth.",
    ].join("\n");
    const repaired = repairNestedFences(md);
    // Outer wrapper fences should be upgraded to 4 backticks
    const lines = repaired.split("\n");
    expect(lines[2]).toBe("````");
    expect(lines[13]).toBe("````");
    // Inner ```yaml fences stay at 3 backticks
    expect(lines[7]).toBe("```yaml");
    expect(lines[10]).toBe("```");
    // Trailing prose still intact
    expect(lines[15]).toBe("Two notes for the back-and-forth.");
  });

  it("upgrades to one more than the maximum inner fence length", () => {
    const md = ["```", "outer", "````go", "code", "````", "back", "```"].join("\n");
    const repaired = repairNestedFences(md);
    const lines = repaired.split("\n");
    expect(lines[0]).toBe("`````"); // 5 backticks (maxInner=4 + 1)
    expect(lines[6]).toBe("`````");
  });

  it("preserves indent on opener and closer", () => {
    const md = ["   ```", "content", "   ```yaml", "yaml", "   ```", "x", "   ```"].join("\n");
    const repaired = repairNestedFences(md);
    const lines = repaired.split("\n");
    expect(lines[0]).toBe("   ````");
    expect(lines[6]).toBe("   ````");
  });

  it("preserves info string when upgrading an info-string opener", () => {
    const md = ["```js", "intro", "```", "code", "```", "outro", "```"].join("\n");
    const repaired = repairNestedFences(md);
    const lines = repaired.split("\n");
    expect(lines[0]).toBe("````js");
    expect(lines[6]).toBe("````");
  });

  it("leaves a wrapper alone when outer is already longer than inner", () => {
    const md = ["````", "content", "```yaml", "yaml", "```", "more", "````"].join("\n");
    expect(repairNestedFences(md)).toBe(md);
  });

  it("leaves a wrapper alone if it has no nested fences", () => {
    const md = "```\nplain text\n```";
    expect(repairNestedFences(md)).toBe(md);
  });

  it("handles a wrapper with multiple nested ```yaml blocks", () => {
    const md = ["```", "a", "```yaml", "1", "```", "b", "```python", "2", "```", "c", "```"].join(
      "\n",
    );
    const repaired = repairNestedFences(md);
    const lines = repaired.split("\n");
    expect(lines[0]).toBe("````");
    expect(lines[10]).toBe("````");
    expect(lines[2]).toBe("```yaml");
    expect(lines[6]).toBe("```python");
  });

  it("does not break ``` blocks that have no close", () => {
    const md = "```\ncontent without close";
    expect(repairNestedFences(md)).toBe(md);
  });
});

// ---------------------------------------------------------------------------
// splitByPromptBlocks integration with fence repair
// ---------------------------------------------------------------------------

describe("splitByPromptBlocks + repair", () => {
  it("repairs nested fences in markdown segments around prompt blocks", () => {
    const md = [
      "```",
      "quoted",
      "```yaml",
      "x: 1",
      "```",
      "end quote",
      "```",
      "",
      "```prompt",
      "# Title",
      "Do it.",
      "```",
    ].join("\n");
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(2);
    expect(segments[0]?.type).toBe("markdown");
    if (segments[0]?.type === "markdown") {
      // The bare ``` wrapper should have been upgraded to ````
      expect(segments[0].content.split("\n")[0]).toBe("````");
    }
    expect(segments[1]?.type).toBe("prompt");
  });
});

// ---------------------------------------------------------------------------
// preprocessAgentiqueTags + splitByPromptBlocks integration
// ---------------------------------------------------------------------------

describe("preprocessAgentiqueTags", () => {
  it('converts a closed <agentique type="prompt"> tag to a fenced block', () => {
    const md = '<agentique type="prompt" title="Foo">\nDo the thing.\n</agentique>';
    const out = preprocessAgentiqueTags(md);
    expect(out).toContain("```prompt");
    expect(out).toContain("# Foo");
    expect(out).toContain("Do the thing.");
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(1);
    expect(segments[0]).toEqual({
      type: "prompt",
      block: { title: "Foo", prompt: "Do the thing." },
    });
  });

  it("carries the project attribute through", () => {
    const md = '<agentique type="prompt" title="Fix" project="agentkit">Do it.</agentique>';
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(1);
    if (segments[0]?.type === "prompt") {
      expect(segments[0].block.projectSlug).toBe("agentkit");
      expect(segments[0].block.title).toBe("Fix");
      expect(segments[0].block.prompt).toBe("Do it.");
    } else {
      expect.fail("expected prompt segment");
    }
  });

  it("preserves code blocks inside the prompt body (upgrades outer fence)", () => {
    const md = [
      '<agentique type="prompt" title="Fix">',
      "Edit this:",
      "```python",
      "print('hi')",
      "```",
      "Run tests.",
      "</agentique>",
    ].join("\n");
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(1);
    if (segments[0]?.type === "prompt") {
      expect(segments[0].block.title).toBe("Fix");
      expect(segments[0].block.prompt).toContain("```python");
      expect(segments[0].block.prompt).toContain("Run tests.");
    } else {
      expect.fail("expected prompt segment");
    }
  });

  it("treats an unclosed tag as a pending_prompt (streaming)", () => {
    const md = '<agentique type="prompt" title="Streaming">\npartial body';
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(1);
    expect(segments[0]?.type).toBe("pending_prompt");
    if (segments[0]?.type === "pending_prompt") {
      expect(segments[0].title).toBe("Streaming");
      expect(segments[0].content).toContain("partial body");
    }
  });

  it("depth-tracks nested <agentique> tags so the body's `</agentique>` mention doesn't close the outer", () => {
    const md = [
      '<agentique type="prompt" title="Outer">',
      "Body mentions a nested example:",
      '<agentique type="example">inner stuff</agentique>',
      "more outer body.",
      "</agentique>",
    ].join("\n");
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(1);
    if (segments[0]?.type === "prompt") {
      expect(segments[0].block.title).toBe("Outer");
      expect(segments[0].block.prompt).toContain("more outer body.");
      expect(segments[0].block.prompt).toContain("inner stuff");
    } else {
      expect.fail("expected prompt segment");
    }
  });

  it('passes through <agentique type="other"> tags unchanged (future feature types)', () => {
    const md = '<agentique type="diff">some diff content</agentique>';
    const out = preprocessAgentiqueTags(md);
    expect(out).toBe(md);
  });

  it("co-exists with legacy ```prompt fenced blocks in the same message", () => {
    const md = [
      "Two tasks:",
      "",
      '<agentique type="prompt" title="A">Task A.</agentique>',
      "",
      "```prompt",
      "# B",
      "Task B.",
      "```",
    ].join("\n");
    const segments = splitByPromptBlocks(md);
    const prompts = segments.filter((s) => s.type === "prompt");
    expect(prompts).toHaveLength(2);
  });

  it("preserves surrounding prose around the tag", () => {
    const md = [
      "Some intro text.",
      "",
      '<agentique type="prompt" title="T">do it</agentique>',
      "",
      "Some outro text.",
    ].join("\n");
    const segments = splitByPromptBlocks(md);
    expect(segments).toHaveLength(3);
    expect(segments[0]?.type).toBe("markdown");
    expect(segments[1]?.type).toBe("prompt");
    expect(segments[2]?.type).toBe("markdown");
    if (segments[0]?.type === "markdown") {
      expect(segments[0].content).toContain("Some intro text.");
    }
    if (segments[2]?.type === "markdown") {
      expect(segments[2].content).toContain("Some outro text.");
    }
  });

  it("ignores agentique tags missing the type attribute", () => {
    const md = '<agentique title="X">body</agentique>';
    const out = preprocessAgentiqueTags(md);
    expect(out).toBe(md);
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

  it("parses project metadata after blank line", () => {
    const md = "```prompt\n# Task\n\nproject: my-proj\n\nDo it.\n```";
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
