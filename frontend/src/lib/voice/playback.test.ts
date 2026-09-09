import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  FakeAudioContext,
  FakeAudioWorkletNode,
  installFakeAudio,
  settleWorklets,
} from "./__tests__/fake-audio";
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
    vi.spyOn(console, "warn").mockImplementation(() => {});
  });

  // The context is created by the constructor and nothing else, so a caller
  // still inside a gesture gets one inside it. Everything the old code waited
  // for — the engine's rate above all — has been moved off this path.
  it("creates its context without waiting to learn the engine's rate", () => {
    const queue = new PlaybackQueue();

    expect(FakeAudioContext.created).toHaveLength(1);
    expect(FakeAudioContext.last.options?.sampleRate).toBeUndefined();
    expect(queue.isRunning).toBe(false);
  });

  it("loads the playback worklet onto its context and wires one node", async () => {
    new PlaybackQueue();
    await settleWorklets();

    const ctx = FakeAudioContext.last;
    expect(ctx.modules).toHaveLength(1);
    expect(ctx.modules[0]).toMatch(/playback-worklet/);
    const node = FakeAudioWorkletNode.last;
    expect(node.name).toBe("playback-processor");
    expect(node.options?.numberOfInputs).toBe(0);
    expect(node.connected).toContain(ctx.gains[0]);
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

  // The rate the server announced rides every frame, and the worklet converts
  // from it. This is what lets the context exist before the engine has said
  // anything — and what removes the per-buffer resampling that buzzed.
  it("posts each frame to the worklet at the announced source rate, transferred", async () => {
    const queue = new PlaybackQueue();
    await settleWorklets();

    const frame = pcm(0, 1000, -1000, 32767);
    queue.enqueue(frame, 24000);

    const port = FakeAudioWorkletNode.last.port;
    expect(port.posted).toEqual([{ type: "frame", rate: 24000, pcm: frame }]);
    expect(port.transfers[0]).toContain(frame);
    expect(FakeAudioContext.last.buffers).toHaveLength(0);
    expect(queue.isPlaying).toBe(true);
  });

  it("falls back to the context's own rate rather than dropping a frame", async () => {
    const queue = new PlaybackQueue();
    await settleWorklets();

    queue.enqueue(pcm(1, 2), 0);

    expect(FakeAudioWorkletNode.last.port.posted[0]).toMatchObject({ rate: 48000 });
  });

  // The module load is a fetch and a parse, and a reply's first frames can
  // beat it. They wait, in order, rather than being lost.
  it("holds frames that arrive before the worklet has loaded, in order", async () => {
    const queue = new PlaybackQueue();
    const first = pcm(1, 1);
    const second = pcm(2, 2);
    queue.enqueue(first, 24000);
    queue.enqueue(second, 24000);
    expect(FakeAudioWorkletNode.created).toHaveLength(0);

    await settleWorklets();

    const posted = FakeAudioWorkletNode.last.port.posted;
    expect(posted).toHaveLength(2);
    expect(posted[0]).toMatchObject({ pcm: first });
    expect(posted[1]).toMatchObject({ pcm: second });
  });

  it("clears the worklet and forgets waiting frames on a barge-in", async () => {
    const queue = new PlaybackQueue();
    queue.enqueue(pcm(1, 2), 24000);
    queue.flush();
    await settleWorklets();
    // Flushed before the node existed: nothing reaches it.
    expect(FakeAudioWorkletNode.last.port.posted).toHaveLength(0);

    queue.enqueue(pcm(3, 4), 24000);
    queue.flush();

    expect(FakeAudioWorkletNode.last.port.posted.at(-1)).toEqual({ type: "clear" });
    expect(queue.isPlaying).toBe(false);
  });

  // "Playing" is whether the ring holds samples, and only the worklet knows
  // when it ran out.
  it("follows the worklet's word on whether audio is sounding", async () => {
    const queue = new PlaybackQueue();
    await settleWorklets();

    queue.enqueue(pcm(1, 2), 24000);
    expect(queue.isPlaying).toBe(true);

    FakeAudioWorkletNode.last.port.receive({ type: "drained" });
    expect(queue.isPlaying).toBe(false);
  });

  it("counts the underruns the worklet reports", async () => {
    const queue = new PlaybackQueue();
    await settleWorklets();
    expect(queue.underruns).toBe(0);

    FakeAudioWorkletNode.last.port.receive({ type: "underrun", count: 3 });

    expect(queue.underruns).toBe(3);
  });

  // A blocked module — the CSP case — is said in the console and does not
  // throw into the call. The watchdog reports the silence it causes.
  it("survives a worklet the browser will not load", async () => {
    FakeAudioContext.moduleBehaviour = "reject";
    const queue = new PlaybackQueue();
    await settleWorklets();

    expect(() => queue.enqueue(pcm(1, 2), 24000)).not.toThrow();
    expect(FakeAudioWorkletNode.created).toHaveLength(0);
    expect(console.warn).toHaveBeenCalled();
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
    await settleWorklets();
    await queue.close();

    expect(queue.tone(() => {})).toBe(false);
    expect(await queue.ready()).toBe(false);
    const before = FakeAudioWorkletNode.last.port.posted.length;
    queue.enqueue(pcm(1, 2), 24000);
    expect(FakeAudioWorkletNode.last.port.posted).toHaveLength(before);
    expect(FakeAudioWorkletNode.last.port.closed).toBe(true);
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
    await settleWorklets();
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

    // And the queue plays into the context it actually has now: a worklet on
    // the new one, and the frame reaches that node.
    await settleWorklets();
    const frame = pcm(1, 2, 3, 4);
    queue.enqueue(frame, 24000);
    const node = FakeAudioWorkletNode.last;
    expect(node.context).toBe(FakeAudioContext.last);
    expect(node.port.posted.at(-1)).toMatchObject({ pcm: frame });
    expect(FakeAudioContext.created[0]?.closeCalls).toBe(1);
  });
});
