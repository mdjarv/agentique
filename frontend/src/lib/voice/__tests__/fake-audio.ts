/**
 * A recording stand-in for the Web Audio API.
 *
 * jsdom has no AudioContext, and the things worth asserting about this
 * subsystem are all scheduling decisions — when a context is created, what rate
 * a buffer is built at, whether an oscillator was started and stopped. None of
 * them need a real audio device, and none of them are about how it sounds.
 */

export class FakeAudioParam {
  readonly setValues: { value: number; at: number }[] = [];
  readonly ramps: { value: number; at: number }[] = [];
  readonly cancels: number[] = [];

  constructor(public value = 0) {}

  setValueAtTime(value: number, at: number): this {
    this.value = value;
    this.setValues.push({ value, at });
    return this;
  }

  linearRampToValueAtTime(value: number, at: number): this {
    this.value = value;
    this.ramps.push({ value, at });
    return this;
  }

  cancelScheduledValues(at: number): this {
    this.cancels.push(at);
    return this;
  }
}

class FakeNode {
  connected: unknown[] = [];
  disconnects = 0;
  connect(target: unknown): unknown {
    this.connected.push(target);
    return target;
  }
  disconnect(): void {
    this.disconnects++;
  }
}

export class FakeGainNode extends FakeNode {
  gain = new FakeAudioParam(1);
}

export class FakeOscillatorNode extends FakeNode {
  type = "sine";
  frequency = new FakeAudioParam(440);
  startedAt: number | null = null;
  stoppedAt: number | null = null;
  onended: (() => void) | null = null;

  start(at = 0): void {
    this.startedAt = at;
  }
  stop(at = 0): void {
    this.stoppedAt = at;
  }
}

export class FakeBufferSourceNode extends FakeNode {
  buffer: FakeAudioBuffer | null = null;
  startedAt: number | null = null;
  stopped = false;
  onended: (() => void) | null = null;

  start(at = 0): void {
    this.startedAt = at;
  }
  stop(): void {
    this.stopped = true;
  }
}

export class FakeAudioBuffer {
  private data: Float32Array;
  constructor(
    readonly channels: number,
    readonly length: number,
    readonly sampleRate: number,
  ) {
    this.data = new Float32Array(length);
  }
  getChannelData(): Float32Array {
    return this.data;
  }
  get duration(): number {
    return this.length / this.sampleRate;
  }
}

/** A worklet node's port as the main thread sees it: what it posted, and a way back. */
export class FakeMessagePort {
  readonly posted: unknown[] = [];
  readonly transfers: unknown[][] = [];
  onmessage: ((event: { data: unknown }) => void) | null = null;
  closed = false;

  postMessage(message: unknown, transfer: unknown[] = []): void {
    this.posted.push(message);
    this.transfers.push(transfer);
  }

  close(): void {
    this.closed = true;
  }

  /** What the worklet would say back. */
  receive(data: unknown): void {
    this.onmessage?.({ data });
  }
}

export class FakeAudioWorkletNode extends FakeNode {
  static created: FakeAudioWorkletNode[] = [];
  readonly port = new FakeMessagePort();

  constructor(
    readonly context: FakeAudioContext,
    readonly name: string,
    readonly options?: AudioWorkletNodeOptions,
  ) {
    super();
    FakeAudioWorkletNode.created.push(this);
    context.workletNodes.push(this);
  }

  static get last(): FakeAudioWorkletNode {
    const node = FakeAudioWorkletNode.created.at(-1);
    if (!node) throw new Error("no AudioWorkletNode was created");
    return node;
  }
}

export class FakeAudioContext {
  static created: FakeAudioContext[] = [];

  /** What `audioWorklet.addModule` does: resolve, or reject as a blocked load. */
  static moduleBehaviour: "load" | "reject" = "load";

  /** What the browser would do with `resume()`: "run", "stay", or "reject". */
  static resumeBehaviour: "run" | "stay" | "reject" = "run";

  /** What state a fresh context starts in. */
  static initialState: AudioContextState = "suspended";

  state: AudioContextState;
  currentTime = 0;
  readonly sampleRate = 48000;
  readonly destination = new FakeNode();

  readonly oscillators: FakeOscillatorNode[] = [];
  readonly gains: FakeGainNode[] = [];
  readonly sources: FakeBufferSourceNode[] = [];
  readonly buffers: FakeAudioBuffer[] = [];
  readonly workletNodes: FakeAudioWorkletNode[] = [];
  readonly modules: string[] = [];
  resumeCalls = 0;
  closeCalls = 0;

  readonly audioWorklet = {
    addModule: async (url: string): Promise<void> => {
      if (FakeAudioContext.moduleBehaviour === "reject") throw new Error("blocked by CSP");
      this.modules.push(url);
    },
  };

  constructor(readonly options?: AudioContextOptions) {
    this.state = FakeAudioContext.initialState;
    FakeAudioContext.created.push(this);
  }

  static reset(): void {
    FakeAudioContext.created = [];
    FakeAudioWorkletNode.created = [];
    FakeAudioContext.resumeBehaviour = "run";
    FakeAudioContext.moduleBehaviour = "load";
    FakeAudioContext.initialState = "suspended";
  }

  static get last(): FakeAudioContext {
    const ctx = FakeAudioContext.created.at(-1);
    if (!ctx) throw new Error("no AudioContext was created");
    return ctx;
  }

  async resume(): Promise<void> {
    this.resumeCalls++;
    if (FakeAudioContext.resumeBehaviour === "reject") throw new Error("blocked");
    if (FakeAudioContext.resumeBehaviour === "run") this.state = "running";
  }

  async close(): Promise<void> {
    this.closeCalls++;
    this.state = "closed";
  }

  createGain(): FakeGainNode {
    const gain = new FakeGainNode();
    this.gains.push(gain);
    return gain;
  }

  createOscillator(): FakeOscillatorNode {
    const osc = new FakeOscillatorNode();
    this.oscillators.push(osc);
    return osc;
  }

  createBufferSource(): FakeBufferSourceNode {
    const source = new FakeBufferSourceNode();
    this.sources.push(source);
    return source;
  }

  createBuffer(channels: number, length: number, sampleRate: number): FakeAudioBuffer {
    const buffer = new FakeAudioBuffer(channels, length, sampleRate);
    this.buffers.push(buffer);
    return buffer;
  }
}

/** Installs the fake as the page's AudioContext for one test file. */
export function installFakeAudio(): void {
  FakeAudioContext.reset();
  (globalThis as { AudioContext?: unknown }).AudioContext = FakeAudioContext;
  (globalThis as { AudioWorkletNode?: unknown }).AudioWorkletNode = FakeAudioWorkletNode;
}

/** Lets the pending module loads settle, so worklet nodes exist. */
export async function settleWorklets(): Promise<void> {
  // Two hops: addModule resolves, then the `.then` that builds the node runs.
  await Promise.resolve();
  await Promise.resolve();
  await Promise.resolve();
}

/** Anything here is only ever handed straight back to the code under test. */
export function asAudioContext(ctx: FakeAudioContext): AudioContext {
  return ctx as unknown as AudioContext;
}
