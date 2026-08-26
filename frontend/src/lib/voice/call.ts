import { MicCapture } from "./capture";
import { PlaybackQueue } from "./playback";
import {
  parseServerMessage,
  type VoiceActivity,
  type VoiceClientMessage,
  type VoiceDispatched,
  type VoiceFocus,
  type VoiceNotice,
  type VoiceReportMessage,
  type VoiceSummary,
  type VoiceTranscript,
  type VoiceWorldSession,
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
  /** The call moved its focus — the screen is expected to follow. */
  onFocus?: (f: VoiceFocus) => void;
  /** The call started or finished something slow. Empty label means finished. */
  onActivity?: (a: VoiceActivity) => void;
  /** A session summary, on screen before it is spoken. */
  onSummary?: (s: VoiceSummary) => void;
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
    this.send({ type: "stop" });
    await this.teardown();
    this.setState("idle");
  }

  /**
   * Tells the call which sessions exist, across every machine this client can
   * reach. The server has no way to ask — only the browser holds the merged
   * picture — so it arrives as a snapshot rather than a query.
   */
  sendWorld(sessions: VoiceWorldSession[]): void {
    this.send({ type: "world", sessions });
  }

  /**
   * Reports that the operator navigated themselves. An empty id means they
   * left the session view.
   *
   * It is a report, never a retarget: what the call does about it is the
   * server's decision.
   */
  sendViewing(sessionId: string): void {
    this.send({ type: "viewing", sessionId });
  }

  /**
   * Control frames are dropped when the socket is not open.
   *
   * Everything sent from here is a snapshot of something the client still
   * holds — the world, where the operator is looking — so a dropped frame is
   * superseded by the next one rather than lost.
   */
  private send(msg: VoiceClientMessage): void {
    if (this.ws?.readyState !== WebSocket.OPEN) return;
    this.ws.send(JSON.stringify(msg));
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

      case "focus":
        this.handlers.onFocus?.(msg);
        return;

      case "activity":
        this.handlers.onActivity?.(msg);
        return;

      case "summary":
        this.handlers.onSummary?.(msg);
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
