import { describe, expect, it } from "vitest";
import { toSpeakableText, toUtteranceChunks } from "./speakable";

describe("toSpeakableText", () => {
  it("drops emphasis markers but keeps the words", () => {
    expect(toSpeakableText("this is **important** and *this* too")).toBe(
      "this is important and this too",
    );
    expect(toSpeakableText("__bold__ and _italic_ words")).toBe("bold and italic words");
    expect(toSpeakableText("~~struck~~ out")).toBe("struck out");
  });

  it("keeps inline code content without the backticks", () => {
    expect(toSpeakableText("call `useState` first")).toBe("call useState first");
  });

  // A fenced block read aloud is unlistenable, and silence hides that anything
  // was there. Announce and skip.
  it("announces a fenced code block instead of reading it", () => {
    const got = toSpeakableText("Before\n\n```go\nfunc main() {\n\tx := 1\n}\n```\n\nAfter");
    expect(got).toContain("go code block");
    expect(got).toContain("3 lines");
    expect(got).not.toContain("func main");
    expect(got).toContain("Before");
    expect(got).toContain("After");
  });

  it("handles a fence with no language", () => {
    expect(toSpeakableText("```\nplain\n```")).toContain("code block");
  });

  // Mid-stream content routinely has an unclosed fence.
  it("handles an unterminated fence", () => {
    const got = toSpeakableText("Here:\n\n```ts\nconst x = 1");
    expect(got).toContain("ts code block");
    expect(got).not.toContain("const x");
  });

  it("keeps link text and drops the url", () => {
    expect(toSpeakableText("see [the docs](https://example.com/a/b) for more")).toBe(
      "see the docs for more",
    );
  });

  it("speaks image alt text rather than the source", () => {
    expect(toSpeakableText("![a chart](/api/x.png)")).toBe("[image: a chart]");
    expect(toSpeakableText("![](/api/x.png)")).toBe("[image]");
  });

  it("strips heading hashes and gives the heading a pause", () => {
    expect(toSpeakableText("## The plan")).toBe("The plan.");
    // An existing terminator is not doubled.
    expect(toSpeakableText("## Ready?")).toBe("Ready?");
  });

  it("strips list and quote markers", () => {
    expect(toSpeakableText("- one\n- two")).toBe("one\ntwo");
    expect(toSpeakableText("1. first\n2. second")).toBe("first\nsecond");
    expect(toSpeakableText("> quoted")).toBe("quoted");
  });

  it("drops horizontal rules and table separators", () => {
    expect(toSpeakableText("a\n\n---\n\nb")).toBe("a\n\nb");
    expect(toSpeakableText("| a | b |\n| --- | --- |\n| 1 | 2 |")).toContain("a b");
  });

  it("strips raw html", () => {
    expect(toSpeakableText("<span>hello</span> there")).toBe("hello there");
  });

  it("returns empty for empty input", () => {
    expect(toSpeakableText("")).toBe("");
    expect(toSpeakableText("   \n\n  ")).toBe("");
  });
});

describe("toUtteranceChunks", () => {
  it("returns nothing for empty text", () => {
    expect(toUtteranceChunks("")).toEqual([]);
  });

  it("keeps a short message as one utterance", () => {
    expect(toUtteranceChunks("Just this.")).toEqual(["Just this."]);
  });

  // Browsers cut long utterances off, so a whole answer handed over in one
  // piece stops partway through.
  it("splits long text into bounded chunks", () => {
    const sentence = "This is a sentence of some length that goes on a while. ";
    const chunks = toUtteranceChunks(sentence.repeat(20), 200);
    expect(chunks.length).toBeGreaterThan(1);
    for (const chunk of chunks) {
      expect(chunk.length).toBeLessThanOrEqual(200);
    }
  });

  it("splits on sentence boundaries rather than mid-word", () => {
    const chunks = toUtteranceChunks("First one here. Second one here. Third one here.", 20);
    for (const chunk of chunks) {
      expect(chunk).not.toMatch(/\s$/);
      expect(chunk.length).toBeGreaterThan(0);
    }
    expect(chunks.join(" ")).toContain("First one here.");
  });

  it("breaks a single over-long sentence at a pause", () => {
    const long = `${"word ".repeat(60)}, and then a clause${" more".repeat(40)}`;
    const chunks = toUtteranceChunks(long, 100);
    for (const chunk of chunks) {
      expect(chunk.length).toBeLessThanOrEqual(100);
    }
  });

  it("loses no words", () => {
    const text = "Alpha beta. Gamma delta epsilon. Zeta eta theta iota kappa.";
    const chunks = toUtteranceChunks(text, 25);
    const rejoined = chunks.join(" ").replace(/\s+/g, " ");
    for (const word of ["Alpha", "beta", "Gamma", "epsilon", "kappa"]) {
      expect(rejoined).toContain(word);
    }
  });
});
