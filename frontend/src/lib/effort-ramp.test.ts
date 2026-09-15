import { describe, expect, it } from "vitest";
import { rampLevelAt, stepRampLevel } from "~/lib/effort-ramp";

describe("rampLevelAt", () => {
  it("snaps a position to the nearest of the five stops", () => {
    expect(rampLevelAt(0)).toBe("low");
    expect(rampLevelAt(0.12)).toBe("low");
    expect(rampLevelAt(0.13)).toBe("medium");
    expect(rampLevelAt(0.5)).toBe("high");
    expect(rampLevelAt(0.8)).toBe("xhigh");
    expect(rampLevelAt(1)).toBe("max");
  });

  it("holds a drag past either end at that end", () => {
    expect(rampLevelAt(-0.4)).toBe("low");
    expect(rampLevelAt(1.7)).toBe("max");
    expect(rampLevelAt(Number.NaN)).toBe("low");
  });
});

describe("stepRampLevel", () => {
  it("moves one stop and stops at the ends", () => {
    expect(stepRampLevel("high", 1)).toBe("xhigh");
    expect(stepRampLevel("high", -1)).toBe("medium");
    expect(stepRampLevel("max", 1)).toBe("max");
    expect(stepRampLevel("low", -1)).toBe("low");
  });

  it("starts an unset level on the first stop, whichever way", () => {
    expect(stepRampLevel("", 1)).toBe("low");
    expect(stepRampLevel("", -1)).toBe("low");
  });
});
