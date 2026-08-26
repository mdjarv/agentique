import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FakeAudioContext, installFakeAudio } from "./__tests__/fake-audio";
import { VoiceCall, type VoiceCallHandlers, type VoiceCallState } from "./call";
import { publishMicLevel, resetMicLevel } from "./level";

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

/**
 * The ringback, whose whole job is to be audible when nobody can look at the
 * screen — and whose one invariant is that it stops.
 *
 * 480 Hz is the ring's alone: no other tone in the call uses it, so counting
 * those notes counts bursts without reaching inside the generator.
 */
function ringNotes(): number {
  return FakeAudioContext.last.oscillators.filter((o) => o.frequency.value === 480).length;
}

/**
 * Lets timers and microtasks run together, on the fake clock.
 *
 * The audio context's clock is moved with them: a running context's clock
 * advances with real time, and a fake one that never moved would look exactly
 * like the wedged context the watchdog is watching for.
 */
async function advance(ms: number): Promise<void> {
  const ctx = FakeAudioContext.created.at(-1);
  if (ctx) ctx.currentTime += ms / 1000;
  await vi.advanceTimersByTimeAsync(ms);
}

/** Advances time with the microphone alive, as a talking caller keeps it. */
async function advanceSpeaking(ms: number): Promise<void> {
  for (let elapsed = 0; elapsed < ms; elapsed += 200) {
    publishMicLevel(0.4);
    await advance(200);
  }
}

describe("VoiceCall ringback", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    installFakeAudio();
    FakeWebSocket.instances = [];
    (globalThis as { WebSocket?: unknown }).WebSocket = FakeWebSocket;
    vi.spyOn(console, "warn").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("rings while the call is connecting", async () => {
    const { call } = newCall();

    await call.start("ws://test/voice");
    await advance(3000);

    expect(ringNotes()).toBeGreaterThan(0);
  });

  // The invariant, path by path. A ring over a live call talks over the
  // assistant's first sentence, and a ring that outlives the call object rings
  // in an empty room.
  it("stops ringing when the call goes live", async () => {
    const { call, states } = newCall();
    await call.start("ws://test/voice");
    await advance(2500);
    const rung = ringNotes();
    expect(rung).toBeGreaterThan(0);

    FakeWebSocket.last.onmessage?.(ready());
    await advance(5000);

    expect(states.at(-1)?.state).toBe("live");
    expect(ringNotes()).toBe(rung);
  });

  it("stops ringing when the server refuses the call", async () => {
    const { call, states } = newCall();
    await call.start("ws://test/voice");
    await advance(2500);
    const rung = ringNotes();

    FakeWebSocket.last.onmessage?.({
      data: JSON.stringify({ type: "error", message: "the voice backend is unavailable" }),
    });
    await advance(5000);

    expect(states.at(-1)?.state).toBe("failed");
    expect(ringNotes()).toBe(rung);
  });

  it("stops ringing when the socket closes under it", async () => {
    const { call } = newCall();
    await call.start("ws://test/voice");
    await advance(2500);
    const rung = ringNotes();

    FakeWebSocket.last.onclose?.();
    await advance(5000);

    expect(ringNotes()).toBe(rung);
  });

  it("stops ringing when the operator hangs up mid-connect", async () => {
    const { call } = newCall();
    await call.start("ws://test/voice");
    await advance(2500);
    const rung = ringNotes();

    const stopped = call.stop();
    await advance(5000);
    await stopped;

    expect(ringNotes()).toBe(rung);
  });

  // A call refused before it ever rang must not ring afterwards: the sound
  // would arrive after the failure it is supposed to precede.
  it("never starts ringing when the call fails immediately", async () => {
    (globalThis as { WebSocket?: unknown }).WebSocket = class {
      constructor() {
        throw new Error("blocked");
      }
    };
    const { call, states } = newCall();

    await call.start("ws://test/voice");
    await advance(30_000);

    expect(states.at(-1)?.state).toBe("failed");
    expect(ringNotes()).toBe(0);
  });
});

describe("VoiceCall audio health", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetMicLevel();
    installFakeAudio();
    FakeWebSocket.instances = [];
    (globalThis as { WebSocket?: unknown }).WebSocket = FakeWebSocket;
    vi.spyOn(console, "warn").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  async function live(): Promise<Recorded> {
    const recorded = newCall();
    await recorded.call.start("ws://test/voice");
    FakeWebSocket.last.onmessage?.(ready());
    await advance(10);
    return recorded;
  }

  // The car case: the operator talks, the model never hears a word, and the
  // silence that follows looks exactly like a dead assistant.
  it("says the microphone is picking up nothing", async () => {
    const { activity } = await live();

    await advance(7000);

    expect(activity.at(-1)).toMatch(/microphone is picking up nothing/i);
  });

  it("says so when the assistant replies and no audio follows", async () => {
    const { activity } = await live();

    FakeWebSocket.last.onmessage?.({
      data: JSON.stringify({ type: "transcript", source: "engine", text: "On it.", final: true }),
    });
    await advanceSpeaking(6400);

    expect(activity.at(-1)).toMatch(/no audio is arriving/i);
  });

  it("clears the line when the audio starts arriving", async () => {
    const { activity } = await live();
    FakeWebSocket.last.onmessage?.({
      data: JSON.stringify({ type: "transcript", source: "engine", text: "On it.", final: true }),
    });
    await advanceSpeaking(6400);
    expect(activity.at(-1)).toMatch(/no audio/i);

    FakeWebSocket.last.onmessage?.({ data: new ArrayBuffer(320) });
    await advanceSpeaking(1200);

    expect(activity.at(-1)).toBe("");
  });

  it("counts the audio it received, so a silent call can be told apart later", async () => {
    const { call } = await live();
    expect(call.audioBytesReceived).toBe(0);

    FakeWebSocket.last.onmessage?.({ data: new ArrayBuffer(320) });
    FakeWebSocket.last.onmessage?.({ data: new ArrayBuffer(640) });

    expect(call.audioBytesReceived).toBe(960);
  });

  // One line, one message: a health fault borrows the status line and gives it
  // back, rather than deleting what the call was working on.
  it("gives the status line back to the call when the fault clears", async () => {
    const { call, activity } = await live();
    FakeWebSocket.last.onmessage?.({
      data: JSON.stringify({ type: "activity", label: "Summarizing the session" }),
    });
    await advance(7000);
    expect(activity.at(-1)).toMatch(/microphone/i);

    // The microphone comes back — a route that settled, a car that reconnected.
    FakeWebSocket.last.onmessage?.({ data: new ArrayBuffer(320) });
    await advanceSpeaking(1200);

    expect(activity.at(-1)).toBe("Summarizing the session");
    expect(call.audioBytesReceived).toBe(320);
  });
});
