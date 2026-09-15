import { describe, expect, it } from "vitest";
import { hasSteps, mergeStep, runningStep, stepDuration, stepsTitle } from "~/lib/assistant/steps";
import type { AssistantStep } from "~/lib/assistant/wire";

const recallRunning: AssistantStep = { seq: 2, kind: "verb", verb: "recall", status: "running" };
const recallDone: AssistantStep = { ...recallRunning, status: "done", outcome: "3 facts" };
const thought: AssistantStep = { seq: 1, kind: "thought", status: "done", encrypted: true };

describe("mergeStep", () => {
  it("places steps by seq whatever order they arrive in", () => {
    const merged = mergeStep(mergeStep([], recallRunning), thought);
    expect(merged.map((s) => s.seq)).toEqual([1, 2]);
  });

  it("lets a settled verb replace its running self", () => {
    const merged = mergeStep([thought, recallRunning], recallDone);
    expect(merged).toHaveLength(2);
    expect(merged[1]?.status).toBe("done");
  });

  it("keeps the held reference for a push it already has", () => {
    const held = [thought, recallDone];
    expect(mergeStep(held, { ...recallDone })).toBe(held);
  });

  it("ignores a step it cannot place", () => {
    const held = [thought];
    expect(mergeStep(held, { kind: "verb", verb: "recall" })).toBe(held);
  });
});

describe("stepsTitle", () => {
  it("counts verbs and thoughts the way the session line does", () => {
    expect(stepsTitle([thought, recallDone])).toBe("1 step, 1 thought");
    expect(stepsTitle([recallDone, { ...recallDone, seq: 3 }])).toBe("2 steps");
  });

  it("counts the steps the server did not keep", () => {
    expect(stepsTitle([recallDone], 5)).toBe("6 steps");
  });
});

describe("runningStep", () => {
  it("is the newest verb still waiting, never a thought", () => {
    expect(runningStep([thought, recallRunning])?.seq).toBe(2);
    expect(runningStep([thought, recallDone])).toBeUndefined();
  });
});

describe("hasSteps", () => {
  it("is true for kept or omitted steps and false for a message with neither", () => {
    expect(hasSteps({ steps: [thought] })).toBe(true);
    expect(hasSteps({ stepsOmitted: 2 })).toBe(true);
    expect(hasSteps({})).toBe(false);
  });
});

describe("stepDuration", () => {
  it("says nothing for an instant verb and reads milliseconds and seconds", () => {
    expect(stepDuration(0)).toBe("");
    expect(stepDuration(140)).toBe("140ms");
    expect(stepDuration(2400)).toBe("2.4s");
    expect(stepDuration(42_000)).toBe("42s");
  });
});
