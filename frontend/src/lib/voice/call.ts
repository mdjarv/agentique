import { MicCapture } from "./capture";
import { PlaybackQueue } from "./playback";
import {
  parseServerMessage,
  type VoiceDispatched,
  type VoiceNotice,
  type VoiceReportMessage,
  type VoiceTranscript,
} from "./protocol";

export type VoiceCallState = "idle" | "connecting" | "live" | "closed" | "failed";

export interface VoiceCallHandlers {
  onState: (state: VoiceCallState, detail?: string) => void;
  onTranscript?: (t: VoiceTranscript) => void;
  /** A progress report from the followed session (agent-written, quoted). */
  onReport?: (r: VoiceReportMessage) => void;
  /** A runtime fact about the followed session. */
  onNotice?: (n: VoiceNotice) => void;
  /** The prompt the voice agent handed over, so it is visible as well as spoken. */
  onDispatched?: (d: VoiceDispatched) => void;
}

/**
 * Turns a getUserMedia rejection into something the reader can act on.
 *
 * The distinctions matter: "denied" means change a permission, "no microphone"
 * means plug one in, and "in use" means close the other app. A single generic
 * message sends the reader looking in the wrong place.
 */
function micFailureMessage(err: unknown): string {
  const name = err instanceof DOMException ? err.name : "";
  switch (name) {
    case "NotAllowedError":
      return "microphone access was denied — allow it for this site and try again";
    case "NotFoundError":
      return "no microphone was found";
    case "NotReadableError":
      return "the microphone is in use by another application";
    case "SecurityError":
      return "the microphone needs a secure context (https or localhost)";
    default:
      return `could not start the microphone: ${String(err)}`;
  }
}

/**
 * URL of the voice socket on the machine serving this page.
 *
 * sessionId names the session the call hands work to. Without it the call can
 * still converse, but `run_prompt` has nothing to dispatch to and says so.
 */
export function primaryVoiceUrl(sessionId?: string): string {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const query = sessionId ? `?sessionId=${encodeURIComponent(sessionId)}` : "";
  return `${protocol}//${window.location.host}/api/voice/live${query}`;
}

/**
 * One live voice call: a socket, a microphone, and a playback queue.
 *
 * The socket is separate from the app's main one on purpose. That one is JSON
 * both ways, so a binary audio frame arriving on it would close it for every
 * other subscription riding it.
 */
export class VoiceCall {
  private ws: WebSocket | null = null;
  private mic = new MicCapture();
  private playback: PlaybackQueue | null = null;
  private handlers: VoiceCallHandlers;
  private state: VoiceCallState = "idle";

  /** Guards against a late close handler reopening or re-reporting a call. */
  private generation = 0;

  constructor(handlers: VoiceCallHandlers) {
    this.handlers = handlers;
  }

  async start(url: string = primaryVoiceUrl()): Promise<void> {
    if (this.state === "connecting" || this.state === "live") return;
    const generation = ++this.generation;
    this.setState("connecting");

    let ws: WebSocket;
    try {
      ws = new WebSocket(url);
    } catch (err) {
      this.fail(`could not open the voice connection: ${String(err)}`);
      return;
    }
    ws.binaryType = "arraybuffer";
    this.ws = ws;

    ws.onmessage = (event) => {
      if (generation !== this.generation) return;
      if (typeof event.data === "string") {
        this.handleControl(event.data);
        return;
      }
      this.playback?.enqueue(event.data as ArrayBuffer);
    };

    ws.onerror = () => {
      if (generation !== this.generation) return;
      // onerror carries no detail by design; onclose follows and reports.
      this.handlers.onState("failed", "the voice connection failed");
    };

    ws.onclose = () => {
      if (generation !== this.generation) return;
      void this.teardown();
      this.setState("closed");
    };
  }

  /** Ends the call, telling the server first so it can close cleanly. */
  async stop(): Promise<void> {
    this.generation++;
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({ type: "stop" }));
    }
    await this.teardown();
    this.setState("idle");
  }

  private handleControl(raw: string): void {
    const msg = parseServerMessage(raw);
    if (!msg) return;

    switch (msg.type) {
      case "ready":
        void this.goLive(msg.outputSampleRate);
        return;

      case "turn_complete":
        // Flush on both outcomes. An interruption that leaves queued audio
        // playing talks straight over the person who interrupted.
        this.playback?.flush();
        return;

      case "transcript":
        this.handlers.onTranscript?.(msg);
        return;

      case "report":
        this.handlers.onReport?.(msg);
        return;

      case "notice":
        this.handlers.onNotice?.(msg);
        return;

      case "dispatched":
        this.handlers.onDispatched?.(msg);
        return;

      case "error":
        this.handlers.onState("failed", msg.message ?? "the voice engine reported a problem");
        return;

      case "closed":
        // The server is hanging up; its close frame drives teardown.
        this.handlers.onState("closed", msg.reason);
        return;

      default: {
        // Exhaustive: a new control type must be handled above, not ignored here.
        const unexpected: never = msg;
        void unexpected;
        return;
      }
    }
  }

  /**
   * Opens the microphone and playback once the server has announced its rates.
   *
   * Capture starts only after `ready`, so the recording indicator never lights
   * for a connection that turned out to be refused.
   */
  private async goLive(outputSampleRate?: number): Promise<void> {
    const generation = this.generation;
    try {
      // The rate comes off the wire rather than a constant: the echo engine
      // answers at the input rate and a speech model at its own.
      this.playback = new PlaybackQueue(outputSampleRate ?? 24000);
      await this.playback.ready();

      await this.mic.start({
        onFrame: (frame) => {
          if (generation !== this.generation) return;
          if (this.ws?.readyState !== WebSocket.OPEN) return;
          this.ws.send(frame);
        },
        onEnded: () => {
          if (generation !== this.generation) return;
          this.handlers.onState("failed", "the microphone was disconnected");
          void this.stop();
        },
      });
    } catch (err) {
      this.fail(micFailureMessage(err));
      return;
    }

    if (generation !== this.generation) {
      await this.teardown();
      return;
    }
    this.setState("live");
  }

  private fail(detail: string): void {
    void this.teardown();
    this.setState("failed", detail);
  }

  private async teardown(): Promise<void> {
    const ws = this.ws;
    this.ws = null;
    if (ws) {
      ws.onmessage = null;
      ws.onerror = null;
      ws.onclose = null;
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
        ws.close();
      }
    }

    const playback = this.playback;
    this.playback = null;

    const errors: unknown[] = [];
    try {
      await this.mic.stop();
    } catch (err) {
      errors.push(err);
    }
    try {
      await playback?.close();
    } catch (err) {
      errors.push(err);
    }
    // Teardown failures are logged, never rethrown: the caller is already
    // ending the call and has nothing useful to do with them.
    if (errors.length > 0) console.warn("[voice] teardown", errors);
  }

  private setState(state: VoiceCallState, detail?: string): void {
    this.state = state;
    this.handlers.onState(state, detail);
  }
}
