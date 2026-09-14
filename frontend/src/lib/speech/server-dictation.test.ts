import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  type DictationEndReason,
  type DictationMic,
  type DictationPhase,
  ServerDictation,
  STOP_FLUSH_MS,
  WRITING_TIMEOUT_MS,
} from "./server-dictation";
import { UTTERANCE_PAUSE_MS } from "./utterance-gate";

class FakeSocket {
  static OPEN = 1;
  readyState = 0;
  binaryType = "";
  sent: (string | ArrayBuffer)[] = [];
  onmessage: ((e: { data: unknown }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  send(data: string | ArrayBuffer) {
    this.sent.push(data);
  }
  close() {
    this.readyState = 3;
  }
  // server side
  open() {
    this.readyState = 1;
  }
  push(msg: object) {
    this.onmessage?.({ data: JSON.stringify(msg) });
  }
  controls(): string[] {
    return this.sent
      .filter((d): d is string => typeof d === "string")
      .map((d) => JSON.parse(d).type);
  }
  frames(): number {
    return this.sent.filter((d) => typeof d !== "string").length;
  }
}

class FakeMic implements DictationMic {
  onFrame: ((f: ArrayBuffer) => void) | null = null;
  stopped = false;
  fail: Error | null = null;
  async start(opts: { onFrame: (f: ArrayBuffer) => void }) {
    if (this.fail) throw this.fail;
    this.onFrame = opts.onFrame;
  }
  async stop() {
    this.stopped = true;
  }
}

/** A 32ms frame whose peak gives the level asked for (level = sqrt(peak)). */
function frame(level: number): ArrayBuffer {
  const samples = new Int16Array(512);
  samples[0] = Math.round(level * level * 32767);
  return samples.buffer;
}

function setup() {
  const sock = new FakeSocket();
  const mic = new FakeMic();
  const clock = { now: 0 };
  const phases: DictationPhase[] = [];
  const texts: [string, boolean][] = [];
  const ends: (DictationEndReason | undefined)[] = [];
  const d = new ServerDictation({
    mic: () => mic,
    socket: () => sock as unknown as WebSocket,
    url: "ws://x/api/voice/dictation",
    now: () => clock.now,
  });
  const handlers = {
    onPhase: (p: DictationPhase) => phases.push(p),
    onText: (t: string, n: boolean) => texts.push([t, n]),
    onEnd: (r?: DictationEndReason) => ends.push(r),
  };
  const speak = (level: number, ms: number) => {
    for (let t = 0; t < ms; t += 32) {
      clock.now += 32;
      mic.onFrame?.(frame(level));
    }
  };
  return { d, sock, mic, phases, texts, ends, speak, handlers };
}

beforeEach(() => {
  vi.stubGlobal("WebSocket", FakeSocket);
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("ServerDictation", () => {
  it("walks connecting → listening → hearing → writing → listening", async () => {
    const s = setup();
    await s.d.start(s.handlers);
    s.sock.open();
    s.sock.push({ type: "ready" });
    s.speak(0.02, 500);
    s.speak(0.6, 1000);
    s.speak(0.02, UTTERANCE_PAUSE_MS + 100);
    expect(s.sock.controls()).toEqual(["utterance_start", "utterance_end"]);

    s.sock.push({ type: "transcript", text: "Refactor the reconnect logic.", newUtterance: true });
    expect(s.phases).toEqual(["connecting", "listening", "hearing", "writing", "listening"]);
    expect(s.texts).toEqual([["Refactor the reconnect logic.", true]]);
  });

  it("sends the pre-roll so the first syllable survives the gate", async () => {
    const s = setup();
    await s.d.start(s.handlers);
    s.sock.open();
    s.sock.push({ type: "ready" });
    s.speak(0.02, 1000);
    expect(s.sock.frames()).toBe(0);
    s.speak(0.6, 32);
    // Ten held frames of room, then the frame that opened the utterance.
    expect(s.sock.frames()).toBe(11);
  });

  it("does not stick on writing when a noise produced no words", async () => {
    const s = setup();
    await s.d.start(s.handlers);
    s.sock.open();
    s.sock.push({ type: "ready" });
    s.speak(0.6, 200);
    s.speak(0.02, UTTERANCE_PAUSE_MS + 100);
    expect(s.phases.at(-1)).toBe("writing");
    vi.advanceTimersByTime(WRITING_TIMEOUT_MS);
    expect(s.phases.at(-1)).toBe("listening");
  });

  it("stop closes the open utterance and waits for its words", async () => {
    const s = setup();
    await s.d.start(s.handlers);
    s.sock.open();
    s.sock.push({ type: "ready" });
    s.speak(0.6, 600);
    s.d.stop();
    expect(s.sock.controls()).toEqual(["utterance_start", "stop"]);
    expect(s.ends).toEqual([]);

    s.sock.push({ type: "transcript", text: "last words", newUtterance: true });
    expect(s.ends).toEqual([undefined]);
    expect(s.texts).toEqual([["last words", true]]);
    expect(s.mic.stopped).toBe(true);
  });

  it("stop gives up waiting after the flush window", async () => {
    const s = setup();
    await s.d.start(s.handlers);
    s.sock.open();
    s.sock.push({ type: "ready" });
    s.speak(0.6, 600);
    s.d.stop();
    vi.advanceTimersByTime(STOP_FLUSH_MS);
    expect(s.ends).toEqual([undefined]);
  });

  it("names a refused microphone", async () => {
    const s = setup();
    s.mic.fail = new DOMException("no", "NotAllowedError");
    await s.d.start(s.handlers);
    expect(s.ends).toEqual(["mic-denied"]);
  });

  it("names why the server ended it", async () => {
    const s = setup();
    await s.d.start(s.handlers);
    s.sock.open();
    s.sock.push({ type: "ready" });
    s.sock.push({ type: "error", message: "the dictation reached its time limit" });
    s.sock.push({ type: "closed", reason: "ended" });
    s.sock.onclose?.();
    expect(s.ends).toEqual(["time-limit"]);

    const r = setup();
    await r.d.start(r.handlers);
    r.sock.push({ type: "error", message: "the speech service is unavailable" });
    r.sock.onclose?.();
    expect(r.ends).toEqual(["unavailable"]);
  });

  it("releases a microphone that finished opening after a stop", async () => {
    const s = setup();
    let release: () => void = () => {};
    s.mic.start = (opts) =>
      new Promise<void>((resolve) => {
        s.mic.onFrame = opts.onFrame;
        release = resolve;
      });
    const started = s.d.start(s.handlers);
    s.d.stop();
    s.mic.stopped = false;
    release();
    await started;
    expect(s.mic.stopped).toBe(true);
    expect(s.ends).toEqual([undefined]);
  });
});
