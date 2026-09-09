import { type AudioDevice, type AudioRoute, listInputs, NO_ROUTE } from "./audio-route";
import { type CaptureRoute, MicCapture } from "./capture";
import {
  type AudioHealthSample,
  assessAudioHealth,
  HEALTH_MESSAGE,
  MIC_SILENCE_FLOOR,
  type VoiceAudioHealth,
} from "./health";
import { readMicLevel } from "./level";
import {
  judgeMicRoute,
  MIC_ROUTE_MESSAGE,
  MIC_ROUTE_RETRIES,
  MIC_ROUTE_RETRY_PAUSE_MS,
} from "./mic-route";
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

/**
 * How long after going live the connected blip waits for the greeting.
 *
 * The server injects the pickup greeting the instant the call goes live, so
 * its audio and the blip used to land on the same moment — a beep under the
 * first word. The greeting is the better acknowledgement when it comes, so
 * the blip yields to it: if PCM arrives inside this window the greeting says
 * "connected" and the blip is skipped; if nothing arrives, the blip still says
 * it, because until the first word "still connecting" and "connected, waiting
 * for you" look identical.
 */
export const CONNECTED_BLIP_WAIT_MS = 1000;

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
 * Everything a call knows about its own audio, at one instant.
 *
 * Assembled rather than logged, because the question it answers is asked after
 * the drive: not "what happened" but "where was this going". A health verdict
 * is in here too, since a report saying the path looked healthy is exactly the
 * finding when the car was silent.
 */
export interface CallAudioReport {
  /** The playback route in the gesture that placed the call. */
  placed: AudioRoute;
  /** The same route once the microphone was open. `state: "none"` until then. */
  live: AudioRoute;
  /** And now. */
  now: AudioRoute;
  capture: CaptureRoute;
  /** The rate the server announced its audio at. */
  outputSampleRate: number;
  /** Bytes of PCM received: the difference between no audio and unplayed audio. */
  pcmBytes: number;
  /** Times playback ran dry mid-reply: audible stutter the watchdog cannot see. */
  underruns: number;
  /** What the watchdog currently makes of it. */
  health: VoiceAudioHealth;
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

  /** The connected blip, while it waits to see whether the greeting beats it. */
  private blipTimer: ReturnType<typeof setTimeout> | null = null;

  /** The last playback queue's underrun count, kept past its teardown. */
  private lastUnderruns = 0;

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
   * The playback route at the two moments worth comparing.
   *
   * Kept rather than read on demand because the interesting fact is a
   * *difference*: the standing suspicion in this subsystem is that opening the
   * microphone moves the output route, and by the time anyone reads a report
   * the move has already happened. One reading before the microphone exists
   * and one immediately after is the smallest thing that can show it.
   */
  private placedRoute: AudioRoute = NO_ROUTE;
  private liveRoute: AudioRoute = NO_ROUTE;

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

  /** Where this call's audio has been going, for someone reading afterwards. */
  audioReport(): CallAudioReport {
    return {
      placed: this.placedRoute,
      live: this.liveRoute,
      now: this.playback?.describe() ?? NO_ROUTE,
      capture: this.mic.describe(),
      outputSampleRate: this.outputSampleRate,
      pcmBytes: this.pcmBytes,
      underruns: this.playback?.underruns ?? this.lastUnderruns,
      health: this.health,
    };
  }

  async start(url: string = primaryVoiceUrl()): Promise<void> {
    if (this.state === "connecting" || this.state === "live") return;
    const generation = ++this.generation;
    this.resetHealth();
    // A fresh capture per call, so its open count describes this call alone.
    this.mic = new MicCapture();
    this.setState("connecting");

    // The microphone opens FIRST, from inside the gesture, and playback is
    // built only once it has — the reverse of what this used to do, and the
    // order is the fix for the car.
    //
    // `getUserMedia` with echo cancellation is what makes Android enter
    // communication mode and bring the Bluetooth hands-free link (HFP/SCO) up.
    // With an output stream already open on the media profile — the dial tone
    // and the ring, on A2DP — the handset has to suspend A2DP and raise SCO
    // underneath a running context, and head units do that unreliably: when
    // SCO fails the request quietly resolves to the handset's own microphone
    // and nothing the driver says is heard. Opening the microphone before any
    // output exists lets the route settle first, and the playback context is
    // then born on the route SCO chose — its rate reads 16 or 8 kHz on HFP,
    // which is the confirmation.
    //
    // `getUserMedia` is invoked synchronously here, so it is inside the
    // activation. The context built when it resolves is still inside the
    // activation window when the permission was remembered, and a permission
    // prompt's Allow is itself a gesture. `ready()` still reports whether the
    // context actually runs and the cannot-play recovery still applies. The
    // cost is that the recording indicator lights before `ready`, so it lights
    // for a call the server then refuses; accepted.
    let settled: boolean;
    try {
      settled = await this.settleAudio(generation);
    } catch (err) {
      if (generation !== this.generation) return;
      this.fail(micFailureMessage(err));
      return;
    }
    // Torn down while the microphone was opening. settleAudio has released
    // what it built; there is nothing to open a socket for.
    if (!settled) return;

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
   * Opens the microphone, checks it is the car's, and only then builds
   * playback and makes the first sound.
   *
   * Resolves true once the route is settled and the socket may open; false
   * when the call was torn down underneath it, in which case everything it
   * built has been released. Throws only for a microphone that cannot be
   * opened at all — the one failure worth ending the call over.
   *
   * The loop is the retry from the car: after each open the device list is
   * read again (labels appear once permission is granted, so the second look
   * can name a device the first could not) and [judgeMicRoute] decides
   * whether the track is the Bluetooth input on the hands-free profile. If
   * not, the microphone and the playback context are both released — so no
   * output stream is open while the platform decides the route again — and
   * the open is repeated asking for that device by id, up to
   * [MIC_ROUTE_RETRIES] times with a pause between. The dial tone and the
   * ring wait for the settled route, so the first sound the operator hears is
   * on the route the call will actually use.
   */
  private async settleAudio(generation: number): Promise<boolean> {
    const mic = this.mic;
    let device: AudioDevice | undefined;
    let announced = false;

    for (let attempt = 0; ; attempt++) {
      await mic.start({
        device,
        onFrame: (frame) => {
          // Only a live call uploads. The server drains the socket only once
          // the engine is up, so frames sent while it is still gathering would
          // arrive as one stale burst the moment it starts listening.
          if (generation !== this.generation || this.state !== "live") return;
          if (this.ws?.readyState !== WebSocket.OPEN) return;
          this.ws.send(frame);
        },
        onEnded: () => {
          if (generation !== this.generation) return;
          this.handlers.onState("failed", "the microphone was disconnected");
          void this.stop();
        },
      });
      if (generation !== this.generation) {
        await mic.stop();
        return false;
      }

      const playback = this.buildPlayback();
      if (!playback) return true;
      await (this.audioReady ?? playback.ready());
      if (generation !== this.generation) {
        this.playback = null;
        await Promise.all([playback.close(), mic.stop()]);
        return false;
      }

      const listed = await listInputs();
      const verdict = judgeMicRoute({
        devices: listed.devices,
        capture: mic.describe(),
        playback: playback.describe(),
      });
      if (verdict.action === "ok" || attempt >= MIC_ROUTE_RETRIES) {
        if (verdict.action !== "ok") {
          console.warn("[voice] microphone route unsettled, proceeding", mic.describe());
        }
        this.sound(playback);
        if (announced) this.handlers.onActivity?.({ type: "activity", label: "" });
        return true;
      }

      console.info("[voice] re-opening microphone", verdict.reason, verdict.device.label);
      if (!announced) {
        announced = true;
        this.handlers.onActivity?.({ type: "activity", label: MIC_ROUTE_MESSAGE });
      }
      this.playback = null;
      this.audioReady = null;
      await Promise.all([playback.close(), mic.stop()]);
      await sleep(MIC_ROUTE_RETRY_PAUSE_MS);
      device = verdict.device;
    }
  }

  /**
   * Creates the playback context and asks the browser to run it.
   *
   * Nothing sounds yet: the route may still be judged wrong and the context
   * thrown away, and a dial tone on a route the call then leaves is a false
   * proof. Null when there is no AudioContext at all, which is a browser that
   * cannot do this — the call is still worth opening, since transcripts and
   * dispatch do not need one.
   */
  private buildPlayback(): PlaybackQueue | null {
    try {
      const playback = new PlaybackQueue();
      this.playback = playback;
      this.audioReady = playback.ready();
      return playback;
    } catch (err) {
      console.warn("[voice] playback unavailable", err);
      this.playback = null;
      this.audioReady = null;
      return null;
    }
  }

  /**
   * The first sounds, on the settled route: the dial tone, then the ringback.
   *
   * The dial tone's second job is proof — a caller who hears it has been shown
   * the audio path works on the route the call will use. The ringback follows
   * for the same reason held down: connecting is audible without looking at
   * the phone, and the ring keeps proving the output path until `ready`.
   */
  private sound(playback: PlaybackQueue): void {
    playback.tone(playDialTone);
    this.ring = playback.ring(startRingback);
    // The route the call was placed on: read once the microphone has settled
    // it, so the reading at `ready` can be compared against it.
    this.placedRoute = playback.describe();
  }

  /**
   * Goes live once the server has announced its rates.
   *
   * The microphone is already open — it was the first thing the call did —
   * and playback is already built on the route it settled, so all that is
   * learned here is the engine's output rate.
   */
  private async goLive(outputSampleRate?: number): Promise<void> {
    const generation = this.generation;
    // The rate comes off the wire rather than a constant: the echo engine
    // answers at the input rate and a speech model at its own. It reaches each
    // frame rather than the context, so learning it late costs nothing.
    if (outputSampleRate && outputSampleRate > 0) this.outputSampleRate = outputSampleRate;

    this.liveSince = Date.now();
    // The second reading, and the reason there are two: a route that moves
    // between the microphone settling and the engine answering is a route
    // this app cannot hold, and one reading cannot show a move.
    this.liveRoute = this.playback?.describe() ?? NO_ROUTE;
    // Live: the ring stops here, because setState is the one door out of
    // connecting.
    this.setState("live");
    this.armConnectedBlip(generation);
    void this.startWatchdog(generation);
  }

  /**
   * Sounds the connected blip unless the greeting gets there first.
   *
   * The server injects the pickup greeting the moment the call goes live, and
   * a blip played at that same moment landed under its first word. So the blip
   * waits [CONNECTED_BLIP_WAIT_MS]: PCM arriving inside the window is the
   * greeting, which is the better acknowledgement, and the blip is skipped; a
   * silent window means the fact still needs saying.
   */
  private armConnectedBlip(generation: number): void {
    this.clearConnectedBlip();
    this.blipTimer = setTimeout(() => {
      this.blipTimer = null;
      if (generation !== this.generation) return;
      if (this.audioFrameAt >= this.liveSince && this.audioFrameAt > 0) return;
      this.playback?.tone(playConnectedTone);
    }, CONNECTED_BLIP_WAIT_MS);
  }

  private clearConnectedBlip(): void {
    if (this.blipTimer === null) return;
    clearTimeout(this.blipTimer);
    this.blipTimer = null;
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
    this.lastUnderruns = 0;
    this.health = "ok";
    this.serverActivity = "";
    this.placedRoute = NO_ROUTE;
    this.liveRoute = NO_ROUTE;
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
    this.clearConnectedBlip();

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
    if (playback) this.lastUnderruns = playback.underruns;

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
