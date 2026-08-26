import { MicCapture } from "./capture";
import {
  type AudioHealthSample,
  assessAudioHealth,
  HEALTH_MESSAGE,
  MIC_SILENCE_FLOOR,
  type VoiceAudioHealth,
} from "./health";
import { readMicLevel } from "./level";
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
import {
  HANGUP_TONE_SECONDS,
  playConnectedTone,
  playDialTone,
  playHangupTone,
  type Ringback,
  startRingback,
} from "./tones";

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
 * How often the call checks that it can still be heard and heard from.
 *
 * Coarse on purpose. Every threshold it feeds is measured in seconds, and the
 * thing being watched — an audio route surviving a Bluetooth profile switch —
 * does not need sub-second resolution to be caught.
 */
const HEALTH_TICK_MS = 1000;

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

  /**
   * The ringback, while the call is connecting. One owner, and the only one:
   * every exit from connecting goes through [stopRinging].
   */
  private ring: Ringback | null = null;

  /** The audio-health watchdog's timer, while the call is live. */
  private watchdog: ReturnType<typeof setInterval> | null = null;

  /** When the call went live, in epoch ms. 0 until it does. */
  private liveSince = 0;

  /** Last time the microphone was above the silence floor. */
  private micSoundAt = 0;

  /** Last time the engine's own transcript arrived — the assistant spoke. */
  private engineSpokeAt = 0;

  /** Last time a PCM frame arrived from the server. */
  private audioFrameAt = 0;

  /** The context clock at the previous health check, for detecting a wedge. */
  private lastClock = -1;

  /**
   * Total PCM received this call, in bytes.
   *
   * The one number that separates "the audio never came" from "the audio came
   * and could not be played", which are the same silence to the operator and
   * two entirely different bugs to whoever reads the report afterwards.
   */
  private pcmBytes = 0;

  /** What the watchdog last decided. Changes are what reach the status line. */
  private health: VoiceAudioHealth = "ok";

  /**
   * The server's own activity label, held so a health line can be lifted off
   * the status line without erasing what the call was actually working on.
   */
  private serverActivity = "";

  constructor(handlers: VoiceCallHandlers) {
    this.handlers = handlers;
  }

  /** Total PCM bytes received from the server this call. */
  get audioBytesReceived(): number {
    return this.pcmBytes;
  }

  async start(url: string = primaryVoiceUrl()): Promise<void> {
    if (this.state === "connecting" || this.state === "live") return;
    const generation = ++this.generation;
    this.resetHealth();

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
      const pcm = event.data as ArrayBuffer;
      this.audioFrameAt = Date.now();
      this.pcmBytes += pcm.byteLength;
      this.playback?.enqueue(pcm, this.outputSampleRate);
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
        // The assistant speaking is what makes its audio *expected*. Caller
        // transcripts say nothing about whether a reply is coming.
        if (msg.source && msg.source !== "caller") this.engineSpokeAt = Date.now();
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
        // Held rather than forwarded blindly: the status line carries one
        // message, and a fault in the audio path outranks what the call is
        // busy with. The label is restored when the fault clears.
        this.serverActivity = (msg.label ?? "").trim();
        if (this.health === "ok") this.handlers.onActivity?.(msg);
        return;

      case "summary":
        this.handlers.onSummary?.(msg);
        return;

      case "error":
        // The ring stops here rather than waiting for the socket to close
        // behind the refusal. A refusal is an answer, and ringing over it says
        // the opposite of what happened — the one thing an eyes-free operator
        // would act on.
        this.stopRinging();
        this.handlers.onState("failed", msg.message ?? "the voice engine reported a problem");
        return;

      case "closed":
        // The server is hanging up; its close frame drives teardown.
        this.stopRinging();
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
   *
   * The ringback follows it immediately, and for the same two reasons doubled:
   * connecting is audible without looking at the phone, and the ring keeps
   * proving the output path right through the moment the microphone opens —
   * which on Bluetooth hands-free is when the route changes underneath it.
   */
  private startPlayback(): void {
    try {
      const playback = new PlaybackQueue();
      this.playback = playback;
      this.audioReady = playback.ready();
      playback.tone(playDialTone);
      this.ring = playback.ring(startRingback);
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
    this.liveSince = Date.now();
    // Live: the ring stops here, before the blip, because setState is the one
    // door out of connecting.
    this.setState("live");
    this.playback?.tone(playConnectedTone);
    void this.startWatchdog(generation);
  }

  /**
   * Silences the ringback, whatever ended the connecting state.
   *
   * The invariant lives here and only here: the ring must never sound over a
   * live call and must never outlive the call object, so every exit — live,
   * error, closed, hangup, teardown — passes through this, and calling it twice
   * is the normal case rather than a bug.
   */
  private stopRinging(): void {
    this.ring?.stop();
    this.ring = null;
  }

  /**
   * Watches, once a second, whether the call can still be heard and heard from.
   *
   * A call that has gone quiet is three faults wearing one face, and in a car
   * none of them can be seen. [assessAudioHealth] names which one; this
   * supplies it with evidence and puts the answer on the status line.
   *
   * The first verdict waits for the playback context to settle: `ready()` is
   * what decides whether the browser let this call make a sound at all, and
   * asking before it resolves would report a blocked context on every call.
   */
  private async startWatchdog(generation: number): Promise<void> {
    const playback = this.playback;
    if (!playback) return;

    await (this.audioReady ?? playback.ready());
    // A call torn down across that await has already stopped a watchdog that
    // did not exist yet; arming one now would leave it ticking forever. The
    // generation does not always move — a server hangup tears down without
    // touching it — so the queue being gone is what says so.
    if (generation !== this.generation || this.playback !== playback) return;

    this.lastClock = -1;
    this.checkHealth(generation);
    this.watchdog = setInterval(() => this.checkHealth(generation), HEALTH_TICK_MS);
  }

  private stopWatchdog(): void {
    if (this.watchdog === null) return;
    clearInterval(this.watchdog);
    this.watchdog = null;
  }

  /** One reading of the audio path, turned into at most one thing to say. */
  private checkHealth(generation: number): void {
    if (generation !== this.generation) return;
    const playback = this.playback;
    if (!playback) return;

    const now = Date.now();
    // The level is already being published by capture for the meter; reading it
    // here costs a variable rather than a second audio node.
    if (readMicLevel() > MIC_SILENCE_FLOOR) this.micSoundAt = now;

    const clock = playback.contextTime;
    // Nothing to compare the first sample against, and unknown is not broken.
    const clockAdvancing = this.lastClock < 0 || clock > this.lastClock;
    this.lastClock = clock;

    const sample: AudioHealthSample = {
      now,
      liveSince: this.liveSince,
      micSoundAt: this.micSoundAt,
      engineSpokeAt: this.engineSpokeAt,
      audioFrameAt: this.audioFrameAt,
      audioRunning: playback.isRunning,
      clockAdvancing,
    };
    this.applyHealth(assessAudioHealth(sample), generation);
  }

  /**
   * Puts a change of verdict on the status line, and nothing else there.
   *
   * Only changes are published, so the line does not rewrite itself every
   * second, and clearing restores whatever the call itself was working on
   * rather than blanking it — the health line borrowed that line, it does not
   * own it.
   */
  private applyHealth(next: VoiceAudioHealth, generation: number): void {
    if (next === this.health) return;
    this.health = next;

    if (next === "ok") {
      this.handlers.onActivity?.({ type: "activity", label: this.serverActivity });
      return;
    }

    // The byte count is the difference between "no audio came" and "audio came
    // and could not be played", which are one silence to the operator.
    console.warn("[voice] audio health", next, { pcmBytes: this.pcmBytes });
    this.handlers.onActivity?.({ type: "activity", label: HEALTH_MESSAGE[next] });

    // The only fault with a gesture that fixes it. Re-arming is free: the queue
    // ignores a second arm while one is outstanding.
    if (next === "cannot-play") {
      this.playback?.resumeOnNextGesture(() => this.checkHealth(generation));
    }
  }

  /** Forgets the previous call's evidence. A new call is diagnosed fresh. */
  private resetHealth(): void {
    this.liveSince = 0;
    this.micSoundAt = 0;
    this.engineSpokeAt = 0;
    this.audioFrameAt = 0;
    this.lastClock = -1;
    this.pcmBytes = 0;
    this.health = "ok";
    this.serverActivity = "";
  }

  private fail(detail: string): void {
    void this.teardown();
    this.setState("failed", detail);
  }

  private async teardown(): Promise<void> {
    // Both before anything else: the ring must not survive the call object, and
    // a watchdog firing against a torn-down playback queue has nothing to read.
    this.stopRinging();
    this.stopWatchdog();

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

    // Whatever the agent was mid-sentence on stops here rather than when the
    // context closes: hanging up is the one gesture that means "stop talking",
    // and the ending tone should not have to compete with a queue.
    playback?.flush();

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
    // Connecting is the only state a ring belongs to, so leaving it — for any
    // reason, including the ones that arrive by throwing — is what stops it.
    if (state !== "connecting") this.stopRinging();
    this.state = state;
    this.handlers.onState(state, detail);
  }
}
