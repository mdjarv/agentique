import { beforeEach, describe, expect, it } from "vitest";
import { asAudioContext, FakeAudioContext } from "./__tests__/fake-audio";
import { playConnectedTone, playDialTone, playHangupTone } from "./tones";

describe("call tones", () => {
  let fake: FakeAudioContext;

  beforeEach(() => {
    FakeAudioContext.reset();
    fake = new FakeAudioContext();
    fake.currentTime = 5;
  });

  it("places the call with two rising notes", () => {
    playDialTone(asAudioContext(fake));

    const freqs = fake.oscillators.map((o) => o.frequency.value);
    expect(freqs).toHaveLength(2);
    expect(freqs[1]).toBeGreaterThan(freqs[0] as number);
  });

  it("ends the call with two falling notes", () => {
    playHangupTone(asAudioContext(fake));

    const freqs = fake.oscillators.map((o) => o.frequency.value);
    expect(freqs).toHaveLength(2);
    expect(freqs[1]).toBeLessThan(freqs[0] as number);
  });

  it("acknowledges connection with one note, quieter than placing the call", () => {
    playDialTone(asAudioContext(fake));
    const dialPeak = Math.max(...fake.gains.flatMap((g) => g.gain.ramps.map((r) => r.value)));

    const second = new FakeAudioContext();
    playConnectedTone(asAudioContext(second));
    const connectedPeak = Math.max(
      ...second.gains.flatMap((g) => g.gain.ramps.map((r) => r.value)),
    );

    expect(second.oscillators).toHaveLength(1);
    expect(connectedPeak).toBeLessThan(dialPeak);
  });

  it("schedules every note against the context clock and stops it", () => {
    playDialTone(asAudioContext(fake));

    for (const osc of fake.oscillators) {
      expect(osc.startedAt).not.toBeNull();
      expect(osc.stoppedAt).not.toBeNull();
      // Scheduled ahead of the clock the context is actually on, never at zero.
      expect(osc.startedAt as number).toBeGreaterThanOrEqual(fake.currentTime);
      expect(osc.stoppedAt as number).toBeGreaterThan(osc.startedAt as number);
    }
    // The second note follows the first rather than playing over it.
    const [first, second] = fake.oscillators;
    expect(second?.startedAt as number).toBeGreaterThanOrEqual(first?.stoppedAt as number);
  });

  it("ramps every note in and out rather than stepping, which would click", () => {
    playHangupTone(asAudioContext(fake));

    for (const gain of fake.gains) {
      expect(gain.gain.ramps).toHaveLength(2);
      const [up, down] = gain.gain.ramps;
      expect(up?.value).toBeGreaterThan(0);
      expect(down?.value).toBe(0);
      expect(down?.at as number).toBeGreaterThan(up?.at as number);
      // It starts from silence, and holds the peak until the fade begins.
      expect(gain.gain.setValues[0]?.value).toBe(0);
    }
  });

  it("plays into the destination, not into nothing", () => {
    playConnectedTone(asAudioContext(fake));

    const gain = fake.gains[0];
    expect(fake.oscillators[0]?.connected).toContain(gain);
    expect(gain?.connected).toContain(fake.destination);
  });

  it("releases its nodes when a note ends", () => {
    playConnectedTone(asAudioContext(fake));

    const osc = fake.oscillators[0];
    const gain = fake.gains[0];
    osc?.onended?.();
    expect(osc?.disconnects).toBe(1);
    expect(gain?.disconnects).toBe(1);
  });
});
