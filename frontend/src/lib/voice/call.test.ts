import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  FakeAudioContext,
  FakeAudioWorkletNode,
  installFakeAudio,
  settleWorklets,
} from "./__tests__/fake-audio";
import type { AudioDevice } from "./audio-route";
import {
  CONNECTED_BLIP_WAIT_MS,
  VoiceCall,
  type VoiceCallHandlers,
  type VoiceCallState,
} from "./call";
import type { CaptureRoute, MicCaptureOptions } from "./capture";
import { publishMicLevel, resetMicLevel } from "./level";
import { MIC_ROUTE_MESSAGE } from "./mic-route";

/**
 * A scripted microphone. jsdom has none, and what these tests care about is
 * the order the call does things in and what it does with what it is given:
 * each open records the device asked for and how many contexts existed at the
 * time, and answers with the next scripted route.
 */
const mic = vi.hoisted(() => ({
  /** Routes handed back by successive opens; the last one repeats. */
  routes: [] as Partial<CaptureRoute>[],
  /** One record per open: the device asked for, and the contexts built before it. */
  starts: [] as {
    device?: AudioDevice;
    contextsBefore: number;
    onFrame: (f: ArrayBuffer) => void;
  }[],
  /** What `listInputs` enumerates. */
  inputs: [] as AudioDevice[],
  reset() {
    this.routes = [];
    this.starts = [];
    this.inputs = [];
  },
}));

vi.mock("./capture", () => {
  const NO_CAPTURE: CaptureRoute = {
    active: false,
    device: "",
    deviceId: "",
    requestedDevice: "",
    bluetoothListed: false,
    opens: 0,
    contextSampleRate: 0,
    trackSampleRate: 0,
    uploadSampleRate: 0,
    echoCancellation: false,
  };
  return {
    NO_CAPTURE,
    MicCapture: class {
      private route: CaptureRoute = NO_CAPTURE;
      private opens = 0;
      async start(opts: MicCaptureOptions): Promise<void> {
        this.opens++;
        mic.starts.push({
          device: opts.device,
          contextsBefore: FakeAudioContext.created.length,
          onFrame: opts.onFrame,
        });
        const scripted = mic.routes[Math.min(this.opens - 1, mic.routes.length - 1)] ?? {};
        this.route = { ...NO_CAPTURE, opens: this.opens, ...scripted };
      }
      describe(): CaptureRoute {
        return this.route;
      }
      async stop(): Promise<void> {
        this.route = { ...NO_CAPTURE, opens: this.opens };
      }
    },
  };
});

vi.mock("./audio-route", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./audio-route")>()),
  listInputs: async () => ({ supported: true, devices: mic.inputs }),
}));

const DEFAULT_INPUT: AudioDevice = { id: "default", label: "Default" };
const BLUETOOTH_INPUT: AudioDevice = { id: "bt", label: "Bluetooth headset" };

/** A track on the handset's own microphone, on a media route. */
const HANDSET_TRACK: Partial<CaptureRoute> = {
  active: true,
  device: "Default",
  deviceId: "default",
  contextSampleRate: 48000,
  uploadSampleRate: 16000,
};

/** The car's microphone, with the hands-free link up. */
const CAR_TRACK: Partial<CaptureRoute> = {
  active: true,
  device: "Bluetooth headset",
  deviceId: "bt",
  contextSampleRate: 16000,
  uploadSampleRate: 16000,
};

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
    mic.reset();
    FakeWebSocket.instances = [];
    (globalThis as { WebSocket?: unknown }).WebSocket = FakeWebSocket;
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(console, "info").mockImplementation(() => {});
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

  // The car fix: opening the microphone is what makes Android bring up the
  // hands-free link, and an output stream already open on the media profile
  // is what made that unreliable. So no context exists until the mic does,
  // and the socket waits for both.
  it("opens the microphone before any output stream exists, and the socket after", async () => {
    const { call } = newCall();

    await call.start("ws://test/voice");

    expect(mic.starts).toHaveLength(1);
    expect(mic.starts[0]?.contextsBefore).toBe(0);
    expect(FakeAudioContext.created).toHaveLength(1);
    expect(FakeWebSocket.instances).toHaveLength(1);
  });

  it("sounds the dial tone once the route has settled, exactly once", async () => {
    const { call } = newCall();

    await call.start("ws://test/voice");

    // Two rising notes, and no other sound yet.
    expect(FakeAudioContext.last.oscillators).toHaveLength(2);
    const freqs = FakeAudioContext.last.oscillators.map((o) => o.frequency.value);
    expect(freqs[1]).toBeGreaterThan(freqs[0] as number);
  });

  // The server drains the socket only once the engine is up, so anything
  // uploaded before `ready` would arrive as one stale burst.
  it("uploads microphone frames only once the call is live", async () => {
    const { call } = newCall();
    await call.start("ws://test/voice");
    const frame = new ArrayBuffer(1024);

    mic.starts[0]?.onFrame(frame);
    expect(FakeWebSocket.last.sent).toHaveLength(0);

    FakeWebSocket.last.onmessage?.(ready());
    await settle();
    mic.starts[0]?.onFrame(frame);
    expect(FakeWebSocket.last.sent).toEqual([frame]);
  });

  it("hands the server's audio to the worklet at the rate it announced", async () => {
    const { call } = newCall();
    await call.start("ws://test/voice");
    FakeWebSocket.last.onmessage?.(ready(24000));
    await settle();
    await settleWorklets();

    const pcm = new ArrayBuffer(8);
    FakeWebSocket.last.onmessage?.({ data: pcm });

    expect(FakeAudioContext.last.sampleRate).toBe(48000);
    expect(FakeAudioWorkletNode.last.port.posted).toEqual([{ type: "frame", rate: 24000, pcm }]);
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

describe("VoiceCall connected blip", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    installFakeAudio();
    mic.reset();
    FakeWebSocket.instances = [];
    (globalThis as { WebSocket?: unknown }).WebSocket = FakeWebSocket;
    vi.spyOn(console, "warn").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  // Until the first word, "still connecting" and "connected, waiting for you"
  // look identical. A silent window after going live still gets the blip.
  it("acknowledges going live with one more note when no greeting arrives", async () => {
    const { call, states } = newCall();
    await call.start("ws://test/voice");

    FakeWebSocket.last.onmessage?.(ready());
    await advance(10);
    expect(states.at(-1)?.state).toBe("live");
    expect(FakeAudioContext.last.oscillators).toHaveLength(2);

    await advance(CONNECTED_BLIP_WAIT_MS);
    expect(FakeAudioContext.last.oscillators).toHaveLength(3);
  });

  // The greeting is injected the instant the call goes live, and a blip at
  // the same instant landed under its first word. The greeting is the better
  // acknowledgement, so it wins.
  it("skips the blip when the greeting's audio arrives first", async () => {
    const { call } = newCall();
    await call.start("ws://test/voice");

    FakeWebSocket.last.onmessage?.(ready());
    await advance(300);
    FakeWebSocket.last.onmessage?.({ data: new ArrayBuffer(320) });
    await advance(CONNECTED_BLIP_WAIT_MS + 100);

    expect(FakeAudioContext.last.oscillators).toHaveLength(2);
  });
});

describe("VoiceCall microphone route", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    installFakeAudio();
    mic.reset();
    FakeWebSocket.instances = [];
    (globalThis as { WebSocket?: unknown }).WebSocket = FakeWebSocket;
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(console, "info").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  /** Runs start() to completion, letting the retry pauses elapse. */
  async function started(call: VoiceCall): Promise<void> {
    const pending = call.start("ws://test/voice");
    await advance(5000);
    await pending;
  }

  /** The dial tone's two notes on a context, told apart from the ring by pitch. */
  function dialNotes(ctx: FakeAudioContext): number {
    return ctx.oscillators.filter((o) => o.frequency.value === 660 || o.frequency.value === 880)
      .length;
  }

  // The intermittent car fault, and the retry that answers it: default
  // resolution opened the handset while a Bluetooth input was listed, so the
  // call releases everything, says so, and asks for that device by id.
  it("re-opens with the Bluetooth input when the handset's microphone was opened instead", async () => {
    mic.inputs = [DEFAULT_INPUT, BLUETOOTH_INPUT];
    mic.routes = [HANDSET_TRACK, CAR_TRACK];
    const { call, activity, states } = newCall();

    await started(call);

    expect(mic.starts).toHaveLength(2);
    expect(mic.starts[1]?.device).toEqual(BLUETOOTH_INPUT);
    // Said, then cleared once the route settled.
    expect(activity).toEqual([MIC_ROUTE_MESSAGE, ""]);
    // No output stream survived into the retry: the first context was closed
    // and the second one is the one that sounds — one dial tone, on it.
    expect(FakeAudioContext.created).toHaveLength(2);
    expect(FakeAudioContext.created[0]?.closeCalls).toBe(1);
    expect(FakeAudioContext.created[0]?.oscillators).toHaveLength(0);
    expect(dialNotes(FakeAudioContext.last)).toBe(2);
    expect(states.at(-1)?.state).toBe("connecting");
    expect(FakeWebSocket.instances).toHaveLength(1);
  });

  it("takes what it has after two retries rather than refusing the call", async () => {
    mic.inputs = [DEFAULT_INPUT, BLUETOOTH_INPUT];
    mic.routes = [HANDSET_TRACK];
    const { call, states } = newCall();

    await started(call);

    expect(mic.starts).toHaveLength(3);
    expect(states.at(-1)?.state).toBe("connecting");
    expect(FakeWebSocket.instances).toHaveLength(1);
    expect(dialNotes(FakeAudioContext.last)).toBe(2);
    expect(call.audioReport().capture.opens).toBe(3);
  });

  it("opens once when the car's microphone comes up first time", async () => {
    mic.inputs = [DEFAULT_INPUT, BLUETOOTH_INPUT];
    mic.routes = [CAR_TRACK];
    const { call, activity } = newCall();

    await started(call);

    expect(mic.starts).toHaveLength(1);
    expect(activity).toHaveLength(0);
    expect(FakeAudioContext.created).toHaveLength(1);
  });

  it("opens once on a handset with no Bluetooth input to ask for", async () => {
    mic.inputs = [DEFAULT_INPUT];
    mic.routes = [HANDSET_TRACK];
    const { call, activity } = newCall();

    await started(call);

    expect(mic.starts).toHaveLength(1);
    expect(activity).toHaveLength(0);
  });
});

describe("VoiceCall ringback", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    installFakeAudio();
    mic.reset();
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
    mic.reset();
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
