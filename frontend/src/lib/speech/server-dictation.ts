/**
 * Dictation through the server, for browsers whose own recognizer cannot.
 *
 * The microphone is the call's (`MicCapture`, 16 kHz PCM from a worklet); the
 * socket is `/api/voice/dictation`; where an utterance begins and ends is
 * `UtteranceGate`'s call. Text comes back a sentence at a time, about 0.3s after
 * each pause, so this reports *phases* as well as text: there is a real wait
 * between speaking and seeing words, and a person has to be able to tell it
 * from nothing happening.
 *
 * Audio leaves only inside an utterance, plus a short pre-roll so the first
 * syllable is not clipped by the gate's reaction time.
 */
import { frameLevel } from "~/lib/voice/level";
import { UtteranceGate } from "./utterance-gate";

export type DictationPhase = "connecting" | "listening" | "hearing" | "writing";

/** Why a server dictation ended. Absent means the person stopped it. */
export type DictationEndReason =
  | "mic-denied"
  | "no-microphone"
  | "unavailable"
  | "time-limit"
  | "idle"
  | "lost";

export interface ServerDictationHandlers {
  onPhase: (phase: DictationPhase) => void;
  /** One chunk of text; `newUtterance` marks where a space belongs before it. */
  onText: (text: string, newUtterance: boolean) => void;
  /** The dictation is over. Called exactly once per start. */
  onEnd: (reason?: DictationEndReason) => void;
}

/** The capture half, as this module uses it. */
export interface DictationMic {
  start(opts: { onFrame: (frame: ArrayBuffer) => void; onEnded?: () => void }): Promise<void>;
  stop(): Promise<void>;
}

export interface ServerDictationDeps {
  mic: () => DictationMic;
  socket: (url: string) => WebSocket;
  url: string;
  now?: () => number;
}

/** Frames kept before an utterance opens: ~320ms at 32ms a frame. */
const PREROLL_FRAMES = 10;

/**
 * How long "writing" may wait for text. A noise the gate took for speech
 * produces no transcript at all, and the phase must not stick on it.
 */
export const WRITING_TIMEOUT_MS = 2500;

/** How long a stop waits for the last utterance's words before closing. */
export const STOP_FLUSH_MS = 1500;

export function dictationUrl(): string {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${window.location.host}/api/voice/dictation`;
}

interface ServerMessage {
  type?: string;
  text?: string;
  newUtterance?: boolean;
  message?: string;
  reason?: string;
}

export class ServerDictation {
  private readonly deps: ServerDictationDeps;
  private handlers: ServerDictationHandlers | null = null;
  private mic: DictationMic | null = null;
  private ws: WebSocket | null = null;
  private gate = new UtteranceGate();
  private preroll: ArrayBuffer[] = [];
  private ready = false;
  private phase: DictationPhase | null = null;
  private writingTimer: ReturnType<typeof setTimeout> | null = null;
  private stopTimer: ReturnType<typeof setTimeout> | null = null;
  /** Utterances closed whose text has not arrived yet. */
  private awaiting = 0;
  private stopping = false;
  private ended = false;
  private endReason: DictationEndReason | undefined;

  constructor(deps: ServerDictationDeps) {
    this.deps = deps;
  }

  /**
   * Opens the microphone first — inside the gesture, which is what keeps the
   * permission prompt attached to the press — then the socket.
   */
  async start(handlers: ServerDictationHandlers): Promise<void> {
    this.handlers = handlers;
    this.setPhase("connecting");

    const mic = this.deps.mic();
    this.mic = mic;
    try {
      await mic.start({
        onFrame: (frame) => this.onFrame(frame),
        onEnded: () => this.finish("lost"),
      });
    } catch (err) {
      const name = err instanceof DOMException ? err.name : "";
      this.finish(name === "NotFoundError" ? "no-microphone" : "mic-denied");
      return;
    }
    if (this.ended) {
      // Stopped while the microphone was opening: finish() released a capture
      // that did not exist yet, so the one that just opened is released here.
      await mic.stop();
      return;
    }

    let ws: WebSocket;
    try {
      ws = this.deps.socket(this.deps.url);
    } catch {
      this.finish("unavailable");
      return;
    }
    ws.binaryType = "arraybuffer";
    this.ws = ws;
    ws.onmessage = (event) => {
      if (typeof event.data === "string") this.onControl(event.data);
    };
    ws.onclose = () => this.finish(this.endReason ?? (this.stopping ? undefined : "lost"));
    ws.onerror = () => {
      // onclose follows and reports.
    };
  }

  /**
   * Ends the dictation cooperatively: the open utterance is closed so its
   * words still come back, and the socket closes once they have or after
   * [STOP_FLUSH_MS].
   */
  stop(): void {
    if (this.ended || this.stopping) return;
    this.stopping = true;
    if (!this.ready || !this.ws) {
      this.finish();
      return;
    }
    if (this.gate.flush() === "end") this.awaiting++;
    this.send({ type: "stop" });
    if (this.awaiting === 0) {
      this.finish();
      return;
    }
    this.setPhase("writing");
    this.stopTimer = setTimeout(() => this.finish(), STOP_FLUSH_MS);
  }

  /** Unconditional teardown: no waiting for words. */
  abort(): void {
    this.stopping = true;
    this.finish();
  }

  private onFrame(frame: ArrayBuffer): void {
    if (this.ended || this.stopping) return;
    if (!this.ready) return;
    const now = this.deps.now?.() ?? Date.now();
    const edge = this.gate.feed(frameLevel(frame), now);

    if (edge === "start") {
      this.send({ type: "utterance_start" });
      for (const held of this.preroll) this.ws?.send(held);
      this.preroll = [];
      this.clearWritingTimer();
      this.setPhase("hearing");
    }

    if (this.gate.isOpen || edge === "end") {
      this.ws?.send(frame);
    } else {
      this.preroll.push(frame);
      if (this.preroll.length > PREROLL_FRAMES) this.preroll.shift();
    }

    if (edge === "end") {
      this.send({ type: "utterance_end" });
      this.awaiting++;
      this.setPhase("writing");
      this.armWritingTimer();
    }
  }

  private onControl(raw: string): void {
    let msg: ServerMessage;
    try {
      msg = JSON.parse(raw) as ServerMessage;
    } catch {
      return;
    }
    switch (msg.type) {
      case "ready":
        this.ready = true;
        if (!this.stopping) this.setPhase("listening");
        return;
      case "transcript":
        if (!msg.text) return;
        this.handlers?.onText(msg.text, msg.newUtterance === true);
        if (msg.newUtterance && this.awaiting > 0) this.awaiting--;
        if (this.stopping) {
          if (this.awaiting === 0) this.finish();
          return;
        }
        if (!this.gate.isOpen && this.awaiting === 0) {
          this.clearWritingTimer();
          this.setPhase("listening");
        }
        return;
      case "error":
        this.endReason = /time limit/i.test(msg.message ?? "") ? "time-limit" : "unavailable";
        return;
      case "closed":
        if (msg.reason === "idle") this.endReason = "idle";
        return;
    }
  }

  private armWritingTimer(): void {
    this.clearWritingTimer();
    this.writingTimer = setTimeout(() => {
      this.writingTimer = null;
      this.awaiting = 0;
      if (!this.gate.isOpen && !this.stopping) this.setPhase("listening");
    }, WRITING_TIMEOUT_MS);
  }

  private clearWritingTimer(): void {
    if (this.writingTimer) clearTimeout(this.writingTimer);
    this.writingTimer = null;
  }

  private send(msg: { type: string }): void {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(msg));
  }

  private setPhase(phase: DictationPhase): void {
    if (this.phase === phase || this.ended) return;
    this.phase = phase;
    this.handlers?.onPhase(phase);
  }

  private finish(reason?: DictationEndReason): void {
    if (this.ended) return;
    this.ended = true;
    this.clearWritingTimer();
    if (this.stopTimer) clearTimeout(this.stopTimer);
    const ws = this.ws;
    this.ws = null;
    if (ws) {
      ws.onmessage = null;
      ws.onclose = null;
      ws.onerror = null;
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) ws.close();
    }
    void this.mic?.stop();
    this.mic = null;
    this.handlers?.onEnd(reason);
  }
}
