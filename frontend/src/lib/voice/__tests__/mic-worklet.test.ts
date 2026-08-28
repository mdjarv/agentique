/**
 * The capture worklet's rate conversion.
 *
 * This is the code that stood between a working microphone and a dead one on
 * every media route: the context is no longer built at the socket's rate, so
 * whatever the hardware gave has to be converted here. The worklet is a real
 * bundled file with no imports (the CSP forbids the blob: trick, and Vite
 * serves it by URL), so it is exercised the way the audio thread runs it —
 * globals installed, module imported, `process` called quantum by quantum.
 */
import { afterEach, describe, expect, it, vi } from "vitest";

/** One render quantum, as the Web Audio spec fixes it. */
const QUANTUM = 128;

interface Processor {
  process(inputs: Float32Array[][]): boolean;
}

interface ProcessorOptions {
  processorOptions?: { targetSampleRate?: number };
}

/**
 * Loads the worklet under a given hardware rate and returns a processor plus
 * the Int16 samples it posts.
 *
 * `sampleRate` is a global in `AudioWorkletGlobalScope` and the worklet reads
 * it at construction, so the module has to be re-imported per rate.
 */
async function loadProcessor(
  hardwareRate: number,
  options: ProcessorOptions = { processorOptions: { targetSampleRate: 16000 } },
): Promise<{ processor: Processor; sent: number[] }> {
  const sent: number[] = [];
  const port = {
    postMessage(buffer: ArrayBuffer) {
      for (const sample of new Int16Array(buffer)) sent.push(sample);
    },
  };

  let registered: (new (opts?: ProcessorOptions) => Processor) | null = null;
  const scope = globalThis as Record<string, unknown>;
  scope.sampleRate = hardwareRate;
  scope.AudioWorkletProcessor = class {
    port = port;
  };
  scope.registerProcessor = (_name: string, cls: new (opts?: ProcessorOptions) => Processor) => {
    registered = cls;
  };

  vi.resetModules();
  // The worklet is deliberately an untyped plain-JS audio-thread module: it can
  // import nothing from the app bundle, so there is nothing a declaration file
  // could usefully describe. The import is for its side effect, which is the
  // registerProcessor call the shim above captures.
  // @ts-expect-error -- untyped audio-thread module, imported for its side effect
  await import("../mic-worklet.js");
  if (!registered) throw new Error("the worklet registered no processor");

  return {
    processor: new (registered as new (opts?: ProcessorOptions) => Processor)(options),
    sent,
  };
}

/** Feeds `quanta` render quanta of a sine at `hz`, sampled at `rate`. */
function feed(processor: Processor, quanta: number, rate: number, hz: number): void {
  let n = 0;
  for (let q = 0; q < quanta; q++) {
    const block = new Float32Array(QUANTUM);
    for (let i = 0; i < QUANTUM; i++, n++) block[i] = Math.sin((2 * Math.PI * hz * n) / rate);
    processor.process([[block]]);
  }
}

afterEach(() => {
  const scope = globalThis as Record<string, unknown>;
  scope.sampleRate = undefined;
  scope.AudioWorkletProcessor = undefined;
  scope.registerProcessor = undefined;
});

describe("mic-worklet rate conversion", () => {
  // The whole bug in one assertion. A 48 kHz device used to post 48 kHz samples
  // that the socket labelled 16 kHz — three times too fast, and unintelligible
  // to the speech model. The count is what says the rate is right.
  it("emits one sample per three on a 48 kHz media route", async () => {
    const { processor, sent } = await loadProcessor(48000);
    // 96 quanta is 12,288 input samples: 4,096 output samples, eight full
    // frames, so nothing is left sitting in the part-filled buffer.
    feed(processor, 96, 48000, 440);
    expect(sent).toHaveLength(12288 / 3);
  });

  it("emits one sample per sample on a wideband hands-free route", async () => {
    const { processor, sent } = await loadProcessor(16000);
    feed(processor, 32, 16000, 440);
    // 4,096 samples in, 4,096 out — the route that always worked still does,
    // and it takes no special case to do it.
    expect(sent).toHaveLength(4096);
  });

  it("holds the target rate on a 44.1 kHz device, where the ratio is fractional", async () => {
    const { processor, sent } = await loadProcessor(44100);
    const inputs = 128 * QUANTUM;
    feed(processor, 128, 44100, 440);
    const expected = Math.round(inputs * (16000 / 44100));
    // Within one frame of exact: only whole 512-sample frames are posted, so
    // the remainder is waiting in the buffer rather than lost.
    expect(expected - sent.length).toBeGreaterThanOrEqual(0);
    expect(expected - sent.length).toBeLessThan(512);
  });

  it("interpolates up from a narrowband hands-free route", async () => {
    const { processor, sent } = await loadProcessor(8000);
    feed(processor, 32, 8000, 440);
    // 4,096 samples in at 8 kHz is 8,192 out at 16 kHz.
    expect(sent).toHaveLength(8192);
  });

  // Speech, not silence. A conversion that produced the right number of
  // all-zero samples would pass every count above and still be a dead
  // microphone, which is the failure this whole change is about.
  it("carries the signal, not zeros", async () => {
    const { processor, sent } = await loadProcessor(48000);
    feed(processor, 96, 48000, 440);
    const peak = sent.reduce((max, s) => Math.max(max, Math.abs(s)), 0);
    // A full-scale sine, box-averaged over three samples at 48 kHz, keeps
    // essentially all of its amplitude at speech frequencies.
    expect(peak).toBeGreaterThan(30000);
  });

  // The window and the read position carry across quanta. If either reset at a
  // quantum boundary the stream would carry a 125 Hz artefact — audible, and
  // exactly the kind of fault that reads as "the microphone is noisy".
  it("keeps conversion state across render quanta", async () => {
    const { processor, sent } = await loadProcessor(44100);
    feed(processor, 64, 44100, 220);
    // A DC-free input stays DC-free: a periodic reset would bias the windows
    // that straddle a boundary and pull the mean off zero.
    const mean = sent.reduce((sum, s) => sum + s, 0) / sent.length;
    expect(Math.abs(mean)).toBeLessThan(200);
  });

  it("survives a quantum with no input and resumes", async () => {
    const { processor, sent } = await loadProcessor(48000);
    expect(processor.process([[]])).toBe(true);
    expect(processor.process([])).toBe(true);
    feed(processor, 96, 48000, 440);
    expect(sent.length).toBeGreaterThan(0);
  });

  it("falls back to the socket's rate when the node names none", async () => {
    const { processor, sent } = await loadProcessor(48000, {});
    feed(processor, 96, 48000, 440);
    expect(sent).toHaveLength(12288 / 3);
  });

  // A microphone at the rail must not wrap to the opposite sign, which is a
  // click rather than a loud sample.
  it("clamps rather than wrapping", async () => {
    const { processor, sent } = await loadProcessor(16000);
    for (let q = 0; q < 32; q++) {
      processor.process([[new Float32Array(QUANTUM).fill(2)]]);
    }
    expect(sent.length).toBeGreaterThan(0);
    expect(Math.min(...sent)).toBeGreaterThan(0);
    expect(Math.max(...sent)).toBeLessThanOrEqual(32767);
  });
});
