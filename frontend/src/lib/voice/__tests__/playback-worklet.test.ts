/**
 * The playback worklet's resampling and its ring.
 *
 * This is the code between a clean reply and a buzz on the hands-free route:
 * every frame used to be resampled by the browser on its own, with nothing
 * carried across the boundary, so the boundaries clicked. The worklet is a
 * real bundled file with no imports, exercised the way the audio thread runs
 * it — globals installed, module imported, messages posted, `process` called
 * quantum by quantum.
 */
import { afterEach, describe, expect, it, vi } from "vitest";

/** One render quantum, as the Web Audio spec fixes it. */
const QUANTUM = 128;

interface Processor {
  process(inputs: Float32Array[][], outputs: Float32Array[][]): boolean;
  port: { onmessage: ((event: { data: unknown }) => void) | null };
}

interface Loaded {
  processor: Processor;
  /** Everything the worklet posted back. */
  sent: unknown[];
  /** Posts one message into the worklet, as the main thread would. */
  post(msg: unknown): void;
  /** Runs one quantum and returns what it rendered. */
  render(): Float32Array;
  /** Moves the audio-thread clock. */
  tick(seconds: number): void;
}

/**
 * Loads the worklet under a given hardware rate.
 *
 * `sampleRate` and `currentTime` are globals in `AudioWorkletGlobalScope`; the
 * worklet reads the first at construction, so the module is re-imported per
 * rate.
 */
async function load(hardwareRate: number): Promise<Loaded> {
  const sent: unknown[] = [];
  const scope = globalThis as Record<string, unknown>;
  scope.sampleRate = hardwareRate;
  scope.currentTime = 0;
  scope.AudioWorkletProcessor = class {
    port = {
      onmessage: null as ((event: { data: unknown }) => void) | null,
      postMessage(msg: unknown) {
        sent.push(msg);
      },
    };
  };
  let registered: (new () => Processor) | null = null;
  scope.registerProcessor = (_name: string, cls: new () => Processor) => {
    registered = cls;
  };

  vi.resetModules();
  // @ts-expect-error -- untyped audio-thread module, imported for its side effect
  await import("../playback-worklet.js");
  if (!registered) throw new Error("the worklet registered no processor");
  const processor = new (registered as new () => Processor)();

  return {
    processor,
    sent,
    post: (msg) => processor.port.onmessage?.({ data: msg }),
    render: () => {
      const out = new Float32Array(QUANTUM);
      processor.process([], [[out]]);
      return out;
    },
    tick: (seconds) => {
      scope.currentTime = (scope.currentTime as number) + seconds;
    },
  };
}

/** A frame message of `n` samples of a sine at `hz`, sampled at `rate`, from sample offset `from`. */
function sineFrame(
  rate: number,
  hz: number,
  n: number,
  from = 0,
): { type: string; rate: number; pcm: ArrayBuffer } {
  const pcm = new ArrayBuffer(n * 2);
  const view = new DataView(pcm);
  for (let i = 0; i < n; i++) {
    const s = Math.sin((2 * Math.PI * hz * (from + i)) / rate);
    view.setInt16(i * 2, Math.round(s * 32000), true);
  }
  return { type: "frame", rate, pcm };
}

/** Renders until the worklet reports the ring drained, returning everything. */
function drain(w: Loaded): number[] {
  const out: number[] = [];
  const before = w.sent.length;
  for (let guard = 0; guard < 100_000; guard++) {
    for (const s of w.render()) out.push(s);
    if (w.sent.slice(before).some((m) => (m as { type: string }).type === "drained")) break;
  }
  return out;
}

afterEach(() => {
  const scope = globalThis as Record<string, unknown>;
  scope.sampleRate = undefined;
  scope.currentTime = undefined;
  scope.AudioWorkletProcessor = undefined;
  scope.registerProcessor = undefined;
});

describe("playback-worklet", () => {
  // The car's route. 24 kHz in, 16 kHz out: two samples for every three.
  it("downsamples the engine's rate onto a hands-free context", async () => {
    const w = await load(16000);
    w.post(sineFrame(24000, 440, 2400));

    const out = drain(w);
    // 1600 real samples, then the rest of the last quantum is silence.
    const rendered = out.length - (out.length % QUANTUM);
    expect(rendered).toBe(Math.ceil(1600 / QUANTUM) * QUANTUM);
    expect(out.slice(0, 1600).some((s) => s !== 0)).toBe(true);
    expect(out.slice(1600).every((s) => s === 0)).toBe(true);
  });

  it("upsamples the engine's rate onto a media context", async () => {
    const w = await load(48000);
    w.post(sineFrame(24000, 440, 2400));

    const out = drain(w);
    const nonZero = out.filter((s) => s !== 0).length;
    // 4800 out for 2400 in, less the odd zero crossing.
    expect(nonZero).toBeGreaterThan(4700);
    expect(nonZero).toBeLessThanOrEqual(4800);
  });

  // The whole bug in one assertion. Frames are 30 ms each and the sine runs
  // straight through them; resampled with state carried across the boundary
  // the output is one continuous sine, and no sample-to-sample step is larger
  // than the sine's own slope allows. A per-frame reset puts a step at every
  // boundary — thirty a second, heard as a buzz.
  it("carries its state across frames, so boundaries do not click", async () => {
    const w = await load(16000);
    const frame = 720; // 30 ms at 24 kHz
    for (let f = 0; f < 20; f++) w.post(sineFrame(24000, 440, frame, f * frame));

    const out = drain(w).slice(0, 9500);
    const peak = out.reduce((m, s) => Math.max(m, Math.abs(s)), 0);
    expect(peak).toBeGreaterThan(0.85);

    // Largest slope of a 440 Hz sine at 16 kHz is 2π·440/16000 ≈ 0.173 of the
    // peak per sample. A boundary reset steps by up to a whole peak.
    let maxStep = 0;
    for (let i = 1; i < out.length; i++)
      maxStep = Math.max(maxStep, Math.abs((out[i] ?? 0) - (out[i - 1] ?? 0)));
    expect(maxStep).toBeLessThan(0.2);
  });

  it("carries its state across frames going up as well", async () => {
    const w = await load(48000);
    const frame = 720;
    for (let f = 0; f < 20; f++) w.post(sineFrame(24000, 440, frame, f * frame));

    const out = drain(w).slice(0, 28_000);
    let maxStep = 0;
    for (let i = 1; i < out.length; i++)
      maxStep = Math.max(maxStep, Math.abs((out[i] ?? 0) - (out[i - 1] ?? 0)));
    // 2π·440/48000 ≈ 0.058 per sample.
    expect(maxStep).toBeLessThan(0.07);
  });

  it("drops everything held on clear", async () => {
    const w = await load(16000);
    w.post(sineFrame(24000, 440, 2400));
    w.post({ type: "clear" });

    const out = w.render();
    expect(out.every((s) => s === 0)).toBe(true);
    expect(w.sent).toHaveLength(0);
  });

  // An underrun is the ring running dry and refilling within a quarter
  // second: a gap in a stream. The end of a reply is followed by seconds of
  // someone else talking, and is not one.
  it("counts a gap mid-stream as an underrun, and the end of a reply as nothing", async () => {
    const w = await load(16000);
    w.post(sineFrame(24000, 440, 240));
    drain(w);
    w.tick(0.05);
    w.post(sineFrame(24000, 440, 240));
    expect(w.sent.filter((m) => (m as { type: string }).type === "underrun")).toEqual([
      { type: "underrun", count: 1 },
    ]);

    drain(w);
    w.tick(2);
    w.post(sineFrame(24000, 440, 240));
    expect(w.sent.filter((m) => (m as { type: string }).type === "underrun")).toHaveLength(1);
  });

  // An engine can send a long reply faster than real time. The ring grows
  // rather than dropping the end of it, and keeps what it held in order.
  it("grows to hold a reply longer than its initial ring, in order", async () => {
    const w = await load(16000);
    // A ramp: 6 s at 24 kHz, in one frame, into a 2 s ring.
    const n = 24000 * 6;
    const pcm = new ArrayBuffer(n * 2);
    const view = new DataView(pcm);
    for (let i = 0; i < n; i++) view.setInt16(i * 2, Math.round((i / n) * 30000), true);
    w.post({ type: "frame", rate: 24000, pcm });

    const out = drain(w);
    const held = out.length - (out.length % QUANTUM);
    expect(held).toBeGreaterThanOrEqual(16000 * 6);
    // Monotonic: nothing reordered by the growth.
    const real = out.slice(0, 16000 * 6 - 1);
    for (let i = 1; i < real.length; i++)
      expect(real[i]).toBeGreaterThanOrEqual((real[i - 1] ?? 0) - 1e-6);
  });

  it("stays alive with nothing to play", async () => {
    const w = await load(16000);
    expect(w.processor.process([], [[new Float32Array(QUANTUM)]])).toBe(true);
    expect(w.processor.process([], [])).toBe(true);
  });
});
