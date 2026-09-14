import { describe, expect, it } from "vitest";
import { UTTERANCE_PAUSE_MS, UtteranceGate } from "./utterance-gate";

/** Feeds 32ms frames at a level for a duration, collecting edges. */
function run(gate: UtteranceGate, clock: { now: number }, level: number, ms: number): string[] {
  const edges: string[] = [];
  for (let t = 0; t < ms; t += 32) {
    clock.now += 32;
    const e = gate.feed(level, clock.now);
    if (e) edges.push(e);
  }
  return edges;
}

describe("UtteranceGate", () => {
  it("opens on speech and closes after a pause", () => {
    const gate = new UtteranceGate();
    const clock = { now: 0 };
    expect(run(gate, clock, 0.02, 500)).toEqual([]);
    expect(run(gate, clock, 0.5, 1500)).toEqual(["start"]);
    expect(run(gate, clock, 0.02, UTTERANCE_PAUSE_MS - 100)).toEqual([]);
    expect(run(gate, clock, 0.02, 300)).toEqual(["end"]);
  });

  it("keeps one utterance across the gaps between words", () => {
    const gate = new UtteranceGate();
    const clock = { now: 0 };
    const edges = [
      ...run(gate, clock, 0.5, 400),
      ...run(gate, clock, 0.03, 300), // a gap between words
      ...run(gate, clock, 0.5, 400),
      ...run(gate, clock, 0.03, 400), // a breath
      ...run(gate, clock, 0.5, 400),
    ];
    expect(edges).toEqual(["start"]);
  });

  it("does not open on a noisy room", () => {
    const gate = new UtteranceGate();
    const clock = { now: 0 };
    // A fan at a level above the fixed threshold: the floor learns it.
    expect(run(gate, clock, 0.1, 2000)).toEqual([]);
    expect(run(gate, clock, 0.16, 500)).toEqual([]);
    expect(run(gate, clock, 0.4, 200)).toEqual(["start"]);
  });

  it("flushes an open utterance on stop, and only then", () => {
    const gate = new UtteranceGate();
    const clock = { now: 0 };
    expect(gate.flush()).toBeNull();
    run(gate, clock, 0.5, 200);
    expect(gate.flush()).toBe("end");
    expect(gate.isOpen).toBe(false);
  });
});
