import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { asAudioContext, FakeAudioContext } from "./__tests__/fake-audio";
import { playConnectedTone, playDialTone, playHangupTone, startRingback } from "./tones";

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

describe("ringback", () => {
  let fake: FakeAudioContext;

  beforeEach(() => {
    vi.useFakeTimers();
    FakeAudioContext.reset();
    fake = new FakeAudioContext();
    fake.currentTime = 5;
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("does not sound over the dial tone it follows", () => {
    startRingback(asAudioContext(fake));

    expect(fake.oscillators).toHaveLength(0);
  });

  it("rings in bursts of two notes, with a gap between them", () => {
    startRingback(asAudioContext(fake));

    vi.advanceTimersByTime(400);
    expect(fake.oscillators).toHaveLength(2);

    // Mid-gap: still two. The gap is silence, not a quieter ring.
    vi.advanceTimersByTime(1000);
    expect(fake.oscillators).toHaveLength(2);

    vi.advanceTimersByTime(1500);
    expect(fake.oscillators).toHaveLength(4);
  });

  it("keeps ringing for as long as nobody answers", () => {
    startRingback(asAudioContext(fake));

    vi.advanceTimersByTime(30_000);

    expect(fake.oscillators.length).toBeGreaterThan(8);
  });

  // The invariant: stop means stop, not "after this burst". A ring that
  // outlives the connecting state plays over the assistant's first sentence.
  it("silences a burst that is still sounding", () => {
    const ring = startRingback(asAudioContext(fake));
    vi.advanceTimersByTime(400);
    const sounding = fake.oscillators.slice();
    expect(sounding).toHaveLength(2);

    ring.stop();

    for (const osc of sounding) {
      // Cut short: stopped at the clock rather than at the end of the burst.
      expect(osc.stoppedAt as number).toBeLessThan(fake.currentTime + 0.4);
    }
    for (const gain of fake.gains) {
      expect(gain.gain.cancels).not.toHaveLength(0);
      expect(gain.gain.ramps.at(-1)?.value).toBe(0);
    }
  });

  it("schedules nothing more once stopped", () => {
    const ring = startRingback(asAudioContext(fake));
    vi.advanceTimersByTime(400);
    const before = fake.oscillators.length;

    ring.stop();
    vi.advanceTimersByTime(30_000);

    expect(fake.oscillators).toHaveLength(before);
  });

  it("stops cleanly before it has ever sounded", () => {
    const ring = startRingback(asAudioContext(fake));

    ring.stop();
    vi.advanceTimersByTime(30_000);

    expect(fake.oscillators).toHaveLength(0);
  });

  it("is idempotent, because every exit from connecting stops it", () => {
    const ring = startRingback(asAudioContext(fake));
    vi.advanceTimersByTime(400);

    ring.stop();
    ring.stop();

    const stops = fake.oscillators.map((o) => o.stoppedAt);
    expect(stops.every((at) => at !== null)).toBe(true);
  });
});
