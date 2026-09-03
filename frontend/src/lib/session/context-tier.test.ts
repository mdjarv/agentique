import { describe, expect, it } from "vitest";
import { contextEscalated, contextPercent, contextTier } from "./context-tier";

describe("contextPercent", () => {
  it("prefers usedTokens, the only signal that survives a compaction", () => {
    expect(
      contextPercent({ contextWindow: 1000, inputTokens: 900, outputTokens: 50, usedTokens: 140 }),
    ).toBe(14);
  });

  it("falls back to input + output for usage restored from older history", () => {
    expect(contextPercent({ contextWindow: 1000, inputTokens: 400, outputTokens: 100 })).toBe(50);
  });

  it("clamps over-limit usage rather than drawing past the end", () => {
    expect(
      contextPercent({ contextWindow: 100, inputTokens: 0, outputTokens: 0, usedTokens: 250 }),
    ).toBe(100);
  });

  it("answers 0 for a window that has not been reported yet", () => {
    expect(contextPercent({ contextWindow: 0, inputTokens: 10, outputTokens: 0 })).toBe(0);
  });
});

describe("contextTier", () => {
  it("is quiet below 60 and never neutral — a grey 2px line reads as a divider", () => {
    const calm = contextTier(14);
    expect(calm.tier).toBe("calm");
    expect(calm.label).toBe("");
    expect(calm.bar).toContain("emerald");
  });

  it("escalates on the boundaries, not near them", () => {
    expect(contextTier(59).tier).toBe("calm");
    expect(contextTier(60).tier).toBe("watch");
    expect(contextTier(79).tier).toBe("watch");
    expect(contextTier(80).tier).toBe("high");
    expect(contextTier(94).tier).toBe("high");
    expect(contextTier(95).tier).toBe("critical");
  });

  it("earns words only once it has escalated", () => {
    expect(contextTier(60).label).toBe("");
    expect(contextTier(80).label).not.toBe("");
    expect(contextTier(95).label).not.toBe("");
  });

  it("spells the Progress indicator class out, because Tailwind scans source", () => {
    for (const pct of [10, 70, 85, 99]) {
      expect(contextTier(pct).indicator).toMatch(/^\[&>\[data-slot=progress-indicator\]\]:bg-/);
    }
  });
});

describe("contextEscalated", () => {
  it("agrees with the tier that owns the words", () => {
    expect(contextEscalated(79)).toBe(false);
    expect(contextEscalated(80)).toBe(true);
    expect(contextEscalated(100)).toBe(true);
  });
});
