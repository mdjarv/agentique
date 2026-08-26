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
import { HANGUP_TONE_SECONDS, playConnectedTone, playDialTone, playHangupTone } from "./tones";

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
 * Playback rate assumed until the server announces its own in `ready`.
 *
 * A fallback rather than a guess that matters: the announced rate is what every
 * buffer is actually built at, and this only covers audio arriving before the
 * announcement, which the protocol does not produce.
 */
const FALLBACK_OUTPUT_RATE = 24000;

/**
 * What the operator is told when the browser will not let the call make a
 * sound. Written as the gesture that fixes it, because there is one.
 */
const AUDIO_BLOCKED_LABEL = "Sound is blocked — tap the call to enable it";

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
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

  /** The rate the server said its audio is at. Every buffer is built at it. */
  private outputSampleRate = FALLBACK_OUTPUT_RATE;

  /** Whether the browser let the playback context run, settled during start. */
  private audioReady: Promise<boolean> | null = null;

  /** Guards against a late close handler reopening or re-reporting a call. */
  private generation = 0;

  constructor(handlers: VoiceCallHandlers) {
    this.handlers = handlers;
  }

  async start(url: string = primaryVoiceUrl()): Promise<void> {
    if (this.state === "connecting" || this.state === "live") return;
    const generation = ++this.generation;

    // Playback is built HERE, in the gesture that placed the call, and NOT when
    // `ready` arrives.
    //
    // An AudioContext created outside a user gesture starts suspended and stays
    // suspended: `resume()` resolves without the context ever running, control
    // frames keep rendering, and nothing is ever heard. `ready` is a socket, a
    // briefing and a speech-model handshake later, by which time the activation
    // has lapsed — which is exactly the call that transcribed the operator and
    // answered in silence. The engine's rate is not known yet and no longer has
    // to be; see PlaybackQueue.
    //
    // Nothing here may be awaited before `resume()` is invoked, or the gesture
    // is over by the time the browser is asked.
    this.startPlayback();
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
      this.playback?.enqueue(event.data as ArrayBuffer, this.outputSampleRate);
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
   * Creates the playback context and asks the browser to run it.
   *
   * Split out only so its one constraint is visible: it must be reachable
   * synchronously from the click. The dial tone rides the same context, so a
   * caller who hears it has been shown the audio path works.
   */
  private startPlayback(): void {
    try {
      const playback = new PlaybackQueue();
      this.playback = playback;
      this.audioReady = playback.ready();
      playback.tone(playDialTone);
    } catch (err) {
      // No AudioContext at all is a browser that cannot do this. The call is
      // still worth opening — transcripts and dispatch do not need one.
      console.warn("[voice] playback unavailable", err);
      this.playback = null;
      this.audioReady = null;
    }
  }

  /**
   * Opens the microphone once the server has announced its rates.
   *
   * Capture starts only after `ready`, so the recording indicator never lights
   * for a connection that turned out to be refused. Playback is already open by
   * then — it was built in the gesture, several seconds earlier.
   */
  private async goLive(outputSampleRate?: number): Promise<void> {
    const generation = this.generation;
    // The rate comes off the wire rather than a constant: the echo engine
    // answers at the input rate and a speech model at its own. It reaches each
    // buffer rather than the context, so learning it late costs nothing.
    if (outputSampleRate && outputSampleRate > 0) this.outputSampleRate = outputSampleRate;

    try {
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
    this.playback?.tone(playConnectedTone);
    void this.reportBlockedAudio(generation);
  }

  /**
   * Says so when the browser will not let the call be heard.
   *
   * Loud rather than mute: a suspended context renders every transcript and
   * plays nothing, which reads as a broken server. It is reported as a status
   * line rather than a failed call because the call is not failed — it hears,
   * it drafts, it dispatches — and because a gesture fixes it, so the line
   * clears itself when one does.
   */
  private async reportBlockedAudio(generation: number): Promise<void> {
    const playback = this.playback;
    if (!playback) return;

    const running = await (this.audioReady ?? playback.ready());
    if (generation !== this.generation || running) return;

    console.warn("[voice] playback is suspended; audio will not be heard");
    this.handlers.onActivity?.({ type: "activity", label: AUDIO_BLOCKED_LABEL });
    playback.resumeOnNextGesture(() => {
      if (generation !== this.generation) return;
      this.handlers.onActivity?.({ type: "activity", label: "" });
    });
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
    this.audioReady = null;

    // Every ending sounds, whoever ended it: the operator, the idle guard, a
    // broken engine. The one the operator cannot otherwise explain is the
    // server hanging up mid-silence, and that arrives here like any other.
    const sounded = playback?.tone(playHangupTone) ?? false;

    const errors: unknown[] = [];
    try {
      await this.mic.stop();
    } catch (err) {
      errors.push(err);
    }
    // Closing the context cancels anything scheduled on it, so the tone gets
    // its moment first. A quarter of a second, and only when there is a tone.
    if (sounded) await sleep(HANGUP_TONE_SECONDS * 1000);
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
