import { beforeEach, describe, expect, it } from "vitest";
import { FakeAudioContext, installFakeAudio } from "./__tests__/fake-audio";
import { PlaybackQueue } from "./playback";

/** One frame of Int16 little-endian mono PCM. */
function pcm(...samples: number[]): ArrayBuffer {
  const buf = new ArrayBuffer(samples.length * 2);
  const view = new DataView(buf);
  for (const [i, s] of samples.entries()) view.setInt16(i * 2, s, true);
  return buf;
}

describe("PlaybackQueue", () => {
  beforeEach(() => {
    installFakeAudio();
  });

  // The context is created by the constructor and nothing else, so a caller in
  // a click handler gets one inside the gesture. Everything the old code waited
  // for — the engine's rate above all — has been moved off this path.
  it("creates its context without waiting to learn the engine's rate", () => {
    const queue = new PlaybackQueue();

    expect(FakeAudioContext.created).toHaveLength(1);
    expect(FakeAudioContext.last.options?.sampleRate).toBeUndefined();
    expect(queue.isRunning).toBe(false);
  });

  it("resumes the context and reports that it is running", async () => {
    const queue = new PlaybackQueue();

    await expect(queue.ready()).resolves.toBe(true);
    expect(FakeAudioContext.last.resumeCalls).toBe(1);
    expect(queue.isRunning).toBe(true);
  });

  // A browser can resolve resume() and leave the context suspended. That is the
  // whole failure: every control frame renders and nothing is ever heard, so it
  // has to be reported rather than assumed away.
  it("reports a context the browser refuses to run", async () => {
    FakeAudioContext.resumeBehaviour = "stay";
    const queue = new PlaybackQueue();

    await expect(queue.ready()).resolves.toBe(false);
    expect(queue.isRunning).toBe(false);
  });

  it("reports a resume that rejects outright, rather than throwing", async () => {
    FakeAudioContext.resumeBehaviour = "reject";
    const queue = new PlaybackQueue();

    await expect(queue.ready()).resolves.toBe(false);
  });

  // The rate the server announced, not the rate the hardware runs at. This is
  // what lets the context exist before the engine has said anything.
  it("builds each buffer at the announced source rate", async () => {
    const queue = new PlaybackQueue();
    await queue.ready();

    queue.enqueue(pcm(0, 1000, -1000, 32767), 24000);

    const ctx = FakeAudioContext.last;
    expect(ctx.sampleRate).toBe(48000);
    expect(ctx.buffers).toHaveLength(1);
    expect(ctx.buffers[0]?.sampleRate).toBe(24000);
    expect(ctx.buffers[0]?.length).toBe(4);
    expect(ctx.sources[0]?.startedAt).not.toBeNull();
  });

  it("falls back to the context's own rate rather than dropping a frame", async () => {
    const queue = new PlaybackQueue();
    await queue.ready();

    queue.enqueue(pcm(1, 2), 0);

    expect(FakeAudioContext.last.buffers[0]?.sampleRate).toBe(48000);
  });

  it("schedules frames back to back and flushes every one on a barge-in", async () => {
    const queue = new PlaybackQueue();
    await queue.ready();

    queue.enqueue(pcm(...new Array(2400).fill(0)), 24000);
    queue.enqueue(pcm(...new Array(2400).fill(0)), 24000);

    const ctx = FakeAudioContext.last;
    const [first, second] = ctx.sources;
    expect(second?.startedAt as number).toBeGreaterThan(first?.startedAt as number);
    expect(queue.isPlaying).toBe(true);

    queue.flush();
    expect(ctx.sources.every((s) => s.stopped)).toBe(true);
    expect(queue.isPlaying).toBe(false);
  });

  it("plays a tone through its own context and says that it did", () => {
    const queue = new PlaybackQueue();

    const played = queue.tone((ctx) => {
      ctx.createOscillator();
    });

    expect(played).toBe(true);
    expect(FakeAudioContext.last.oscillators).toHaveLength(1);
  });

  // A sound is never worth aborting a call, or a hangup, over.
  it("swallows a tone that throws", () => {
    const queue = new PlaybackQueue();

    expect(
      queue.tone(() => {
        throw new Error("no oscillators today");
      }),
    ).toBe(false);
  });

  it("plays nothing once closed", async () => {
    const queue = new PlaybackQueue();
    await queue.close();

    expect(queue.tone(() => {})).toBe(false);
    expect(await queue.ready()).toBe(false);
    queue.enqueue(pcm(1, 2), 24000);
    expect(FakeAudioContext.last.buffers).toHaveLength(0);
  });

  // Belt and braces for mobile: the operator touching anything is a fresh
  // activation, and the callback is what clears the "sound is blocked" line.
  it("retries on the next gesture and calls back once it works", async () => {
    FakeAudioContext.resumeBehaviour = "stay";
    const queue = new PlaybackQueue();
    await queue.ready();

    let resumed = 0;
    queue.resumeOnNextGesture(() => {
      resumed++;
    });

    window.dispatchEvent(new Event("pointerdown"));
    await Promise.resolve();
    expect(resumed).toBe(0);

    FakeAudioContext.resumeBehaviour = "run";
    window.dispatchEvent(new Event("pointerdown"));
    await new Promise((r) => setTimeout(r, 0));
    expect(resumed).toBe(1);

    // And it stops listening once it has worked.
    window.dispatchEvent(new Event("pointerdown"));
    await new Promise((r) => setTimeout(r, 0));
    expect(resumed).toBe(1);
  });

  // A context resume() will not revive is not suspended, it is wedged — which
  // is what an audio route changing underneath it does, and no amount of
  // resuming fixes that. A fresh context on a gesture is the reliable recovery.
  it("rebuilds the context when resuming it twice was not enough", async () => {
    FakeAudioContext.resumeBehaviour = "stay";
    const queue = new PlaybackQueue();
    await queue.ready();
    expect(FakeAudioContext.created).toHaveLength(1);

    let resumed = 0;
    queue.resumeOnNextGesture(() => {
      resumed++;
    });

    // The first gesture tries the cheap thing, and only that.
    window.dispatchEvent(new Event("pointerdown"));
    await new Promise((r) => setTimeout(r, 0));
    expect(FakeAudioContext.created).toHaveLength(1);
    expect(resumed).toBe(0);

    // The second builds a new one, inside the gesture, where it can run.
    FakeAudioContext.resumeBehaviour = "run";
    window.dispatchEvent(new Event("pointerdown"));
    await new Promise((r) => setTimeout(r, 0));
    expect(FakeAudioContext.created).toHaveLength(2);
    expect(resumed).toBe(1);

    // And the queue plays into the context it actually has now.
    queue.enqueue(new Int16Array([1, 2, 3, 4]).buffer, 24000);
    expect(FakeAudioContext.last.sources).toHaveLength(1);
    expect(FakeAudioContext.created[0]?.closeCalls).toBe(1);
  });
});
