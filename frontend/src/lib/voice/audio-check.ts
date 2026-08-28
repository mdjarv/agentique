/**
 * Three ways to make a sound, so a car can say which one it plays.
 *
 * A live call is a poor instrument for this. It needs a server, a microphone
 * permission, a speech backend and thirty seconds of someone's attention, and
 * at the end of it the operator has learned one bit: silence. These probes
 * strip everything else away — press, listen, read the numbers — and they vary
 * exactly one thing between them, so hearing one and not another *names the
 * fix* rather than merely confirming the fault.
 *
 * The three are not equal. [call] is the path a real call takes today, so it is
 * the control: if a car plays this, the output route is not the problem.
 * [buffered] and [element] are the two candidate fixes, and they exist here
 * rather than in `playback.ts` deliberately — a probe may guess, the call may
 * not. Nothing here changes what a call does.
 *
 * Every probe must reach `new AudioContext()` synchronously from the click that
 * asked for it. That is the same rule the call itself obeys and for the same
 * reason: a context built after an await starts suspended, resumes to nothing,
 * and would report a silence it caused.
 */
import { type AudioRoute, NO_ROUTE, readRoute } from "./audio-route";
import { PlaybackQueue } from "./playback";
import { CHECK_TONE_SECONDS, playCheckTone } from "./tones";

/**
 * Which way of making a sound is being tried.
 *
 * - `call` — what `PlaybackQueue` does now: a bare `AudioContext`, straight to
 *   its destination. The control.
 * - `buffered` — the same, asking for a media-sized buffer instead of the
 *   low-latency output WebAudio prefers. Android's low-latency path does not
 *   exist on every route, and what it falls back to is not always audible.
 * - `element` — rendered into a `MediaStream` and played through an
 *   `<audio>` element, so the sound leaves the page as media rather than as
 *   Web Audio. Some car routes carry one and not the other.
 */
export type ProbePath = "call" | "buffered" | "element";

/** What each probe is, in the words the page shows. */
export const PROBE_COPY: Record<ProbePath, { title: string; detail: string }> = {
  call: {
    title: "As a call plays it",
    detail: "A plain AudioContext, straight to its destination. This is the control.",
  },
  buffered: {
    title: "With a media-sized buffer",
    detail: "latencyHint: playback — asks for a normal output instead of a low-latency one.",
  },
  element: {
    title: "Through an audio element",
    detail: "Rendered to a MediaStream and played as media rather than as Web Audio.",
  },
};

export interface ProbeResult {
  path: ProbePath;
  /** Whether the browser actually let the sound start. */
  started: boolean;
  /** Why it did not start, or what went wrong on the way. `""` when it did. */
  detail: string;
  /** Where the browser said it was sending it, read once the output settled. */
  route: AudioRoute;
}

/**
 * How long to let the output settle before reading its latency.
 *
 * `outputLatency` is a property of a stream that is running; read in the same
 * tick the context was resumed it is either zero or missing, and a diagnostic
 * that reports zero for the number the whole question turns on is worse than no
 * diagnostic.
 */
const ROUTE_SETTLE_MS = 300;

const sleep = (ms: number): Promise<void> => new Promise((done) => setTimeout(done, ms));

/**
 * Plays the check tone one way and reports where it went.
 *
 * Resolves when the tone has finished and its context is closed, so the caller
 * can render "playing" for exactly as long as something is playing. It never
 * rejects: a probe that throws has reported nothing, and every way this can
 * fail is itself a finding.
 */
export async function probeOutput(path: ProbePath): Promise<ProbeResult> {
  switch (path) {
    case "call":
      return probeCall();
    case "buffered":
      return probeContext("buffered", "playback");
    case "element":
      return probeElement();
    default: {
      // Exhaustive: a fourth way to make a sound must say what it is here.
      const unexpected: never = path;
      return { path: unexpected, started: false, detail: "unknown probe", route: NO_ROUTE };
    }
  }
}

/**
 * The control, and it uses the real class rather than imitating it.
 *
 * Imitating `PlaybackQueue` would make this probe a test of the imitation. If
 * the car plays this, the shipped output path works there.
 */
async function probeCall(): Promise<ProbeResult> {
  let queue: PlaybackQueue;
  try {
    queue = new PlaybackQueue();
  } catch (err) {
    return { path: "call", started: false, detail: String(err), route: NO_ROUTE };
  }
  const started = queue.tone(playCheckTone);
  const running = await queue.ready();
  await sleep(ROUTE_SETTLE_MS);
  const route = queue.describe();
  await sleep(CHECK_TONE_SECONDS * 1000);
  await queue.close();
  return {
    path: "call",
    started: started && running,
    detail: running ? "" : "the browser would not run the audio context",
    route,
  };
}

/** A bare context with one option changed, so only that option is being tested. */
async function probeContext(path: ProbePath, latencyHint: AudioContextLatencyCategory) {
  let ctx: AudioContext;
  try {
    ctx = new AudioContext({ latencyHint });
  } catch (err) {
    return { path, started: false, detail: String(err), route: NO_ROUTE };
  }
  let started = false;
  let detail = "";
  try {
    playCheckTone(ctx);
    started = true;
  } catch (err) {
    detail = String(err);
  }
  const running = await resume(ctx);
  if (!running) detail = "the browser would not run the audio context";
  await sleep(ROUTE_SETTLE_MS);
  const route = readRoute(ctx);
  await sleep(CHECK_TONE_SECONDS * 1000);
  await closeQuietly(ctx);
  return { path, started: started && running, detail, route };
}

/**
 * The tone as media: rendered into a stream, played by an element.
 *
 * The element is in the document because a detached one is not reliably played
 * on mobile, and it is removed again — a hidden `<audio>` left behind would
 * hold the route open after the probe that borrowed it is over.
 */
async function probeElement(): Promise<ProbeResult> {
  let ctx: AudioContext;
  try {
    ctx = new AudioContext();
  } catch (err) {
    return { path: "element", started: false, detail: String(err), route: NO_ROUTE };
  }

  const sink = ctx.createMediaStreamDestination();
  const audio = document.createElement("audio");
  audio.autoplay = true;
  // Without this iOS takes the sound fullscreen; it is meaningless elsewhere
  // and harmless everywhere.
  audio.setAttribute("playsinline", "");
  audio.style.display = "none";
  audio.srcObject = sink.stream;
  document.body.appendChild(audio);

  let detail = "";
  try {
    playCheckTone(ctx, sink);
  } catch (err) {
    detail = String(err);
  }

  // Both in the gesture: the element needs its own permission to make noise,
  // separately from the context that feeds it.
  const playing = audio
    .play()
    .then(() => true)
    .catch((err: unknown) => {
      detail = `the audio element refused to play: ${String(err)}`;
      return false;
    });
  const running = await resume(ctx);
  if (!running) detail = "the browser would not run the audio context";
  const played = await playing;

  await sleep(ROUTE_SETTLE_MS);
  const route = readRoute(ctx);
  await sleep(CHECK_TONE_SECONDS * 1000);

  audio.pause();
  audio.srcObject = null;
  audio.remove();
  await closeQuietly(ctx);

  return { path: "element", started: played && running && !detail, detail, route };
}

async function resume(ctx: AudioContext): Promise<boolean> {
  if (ctx.state === "suspended") {
    try {
      await ctx.resume();
    } catch {
      // A refusal and a resume that changes nothing are the same outcome here,
      // and the state below is what says which happened.
    }
  }
  return ctx.state === "running";
}

async function closeQuietly(ctx: AudioContext): Promise<void> {
  if (ctx.state === "closed") return;
  try {
    await ctx.close();
  } catch {
    // A context that will not close is one we are done with either way.
  }
}
