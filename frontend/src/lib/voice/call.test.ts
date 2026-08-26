import { beforeEach, describe, expect, it, vi } from "vitest";
import { FakeAudioContext, installFakeAudio } from "./__tests__/fake-audio";
import { VoiceCall, type VoiceCallHandlers, type VoiceCallState } from "./call";

// The microphone is not what these tests are about, and jsdom has none.
vi.mock("./capture", () => ({
  MicCapture: class {
    async start(): Promise<void> {}
    async stop(): Promise<void> {}
  },
}));

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 3;

  readyState = FakeWebSocket.OPEN;
  binaryType = "blob";
  sent: unknown[] = [];
  onmessage: ((e: { data: unknown }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: (() => void) | null = null;

  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this);
  }

  send(data: unknown): void {
    this.sent.push(data);
  }

  close(): void {
    this.readyState = FakeWebSocket.CLOSED;
  }

  static get last(): FakeWebSocket {
    const ws = FakeWebSocket.instances.at(-1);
    if (!ws) throw new Error("no socket was opened");
    return ws;
  }
}

/** Lets every pending microtask and zero-delay timer run. */
function settle(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function ready(outputSampleRate = 24000): { data: string } {
  return { data: JSON.stringify({ type: "ready", inputSampleRate: 16000, outputSampleRate }) };
}

interface Recorded {
  call: VoiceCall;
  states: { state: VoiceCallState; detail?: string }[];
  activity: (string | undefined)[];
}

function newCall(): Recorded {
  const states: { state: VoiceCallState; detail?: string }[] = [];
  const activity: (string | undefined)[] = [];
  const handlers: VoiceCallHandlers = {
    onState: (state, detail) => states.push({ state, detail }),
    onActivity: (a) => activity.push(a.label),
  };
  return { call: new VoiceCall(handlers), states, activity };
}

describe("VoiceCall audio", () => {
  beforeEach(() => {
    installFakeAudio();
    FakeWebSocket.instances = [];
    (globalThis as { WebSocket?: unknown }).WebSocket = FakeWebSocket;
    vi.spyOn(console, "warn").mockImplementation(() => {});
  });

  // The bug this exists for: the context used to be created when `ready`
  // arrived, seconds after the click, by which time the browser's user
  // activation had lapsed and the context could never be resumed.
  it("creates and resumes the playback context inside start(), not on ready", async () => {
    const { call } = newCall();

    await call.start("ws://test/voice");

    expect(FakeAudioContext.created).toHaveLength(1);
    expect(FakeAudioContext.last.resumeCalls).toBe(1);
    // And nothing about the engine's rate was needed to do it.
    expect(FakeAudioContext.last.options?.sampleRate).toBeUndefined();
  });

  it("sounds the dial tone from the gesture, exactly once", async () => {
    const { call } = newCall();

    await call.start("ws://test/voice");

    // Two rising notes, and no other sound yet.
    expect(FakeAudioContext.last.oscillators).toHaveLength(2);
    const freqs = FakeAudioContext.last.oscillators.map((o) => o.frequency.value);
    expect(freqs[1]).toBeGreaterThan(freqs[0] as number);
  });

  it("acknowledges going live with one more note", async () => {
    const { call, states } = newCall();
    await call.start("ws://test/voice");

    FakeWebSocket.last.onmessage?.(ready());
    await settle();

    expect(states.at(-1)?.state).toBe("live");
    expect(FakeAudioContext.last.oscillators).toHaveLength(3);
  });

  it("builds playback buffers at the rate the server announced", async () => {
    const { call } = newCall();
    await call.start("ws://test/voice");
    FakeWebSocket.last.onmessage?.(ready(24000));
    await settle();

    const pcm = new ArrayBuffer(8);
    FakeWebSocket.last.onmessage?.({ data: pcm });

    const ctx = FakeAudioContext.last;
    expect(ctx.sampleRate).toBe(48000);
    expect(ctx.buffers[0]?.sampleRate).toBe(24000);
  });

  // Whoever ended it — the operator, the idle guard, a broken engine — the line
  // going down sounds the same, and it sounds once.
  it("sounds the hangup tone when the server closes the call, exactly once", async () => {
    const { call } = newCall();
    await call.start("ws://test/voice");
    FakeWebSocket.last.onmessage?.(ready());
    await settle();

    const before = FakeAudioContext.last.oscillators.length;
    FakeWebSocket.last.onclose?.();
    await settle();

    expect(FakeAudioContext.last.oscillators).toHaveLength(before + 2);
    const [first, second] = FakeAudioContext.last.oscillators.slice(-2);
    expect(second?.frequency.value).toBeLessThan(first?.frequency.value as number);

    // A second close is the same ending, not another one.
    FakeWebSocket.last.onclose?.();
    await settle();
    expect(FakeAudioContext.last.oscillators).toHaveLength(before + 2);
  });

  it("sounds the hangup tone when the operator hangs up", async () => {
    const { call } = newCall();
    await call.start("ws://test/voice");
    FakeWebSocket.last.onmessage?.(ready());
    await settle();

    const before = FakeAudioContext.last.oscillators.length;
    await call.stop();

    expect(FakeAudioContext.last.oscillators).toHaveLength(before + 2);
  });

  // Loud rather than mute. A suspended context renders every transcript and
  // plays nothing, which reads as a broken server.
  it("says so when the browser will not let the call be heard", async () => {
    FakeAudioContext.resumeBehaviour = "stay";
    const { call, activity, states } = newCall();

    await call.start("ws://test/voice");
    FakeWebSocket.last.onmessage?.(ready());
    await settle();

    expect(activity.at(-1)).toMatch(/blocked/i);
    // The call is not failed: it hears, it drafts, it dispatches.
    expect(states.at(-1)?.state).toBe("live");
  });

  it("clears the warning once a gesture unblocks the sound", async () => {
    FakeAudioContext.resumeBehaviour = "stay";
    const { call, activity } = newCall();
    await call.start("ws://test/voice");
    FakeWebSocket.last.onmessage?.(ready());
    await settle();
    expect(activity.at(-1)).toMatch(/blocked/i);

    FakeAudioContext.resumeBehaviour = "run";
    window.dispatchEvent(new Event("pointerdown"));
    await settle();

    expect(activity.at(-1)).toBe("");
  });

  it("says nothing about sound when the context runs", async () => {
    const { call, activity } = newCall();

    await call.start("ws://test/voice");
    FakeWebSocket.last.onmessage?.(ready());
    await settle();

    expect(activity).toHaveLength(0);
  });
});
