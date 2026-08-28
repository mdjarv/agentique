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
 * **A probe sounds until it is stopped, never for a fixed duration.** A
 * Bluetooth or projection sink can take most of a second to wake, and a tone
 * that ends inside that window is inaudible on a route that works perfectly —
 * which would make this answer the wrong question, confidently. The operator
 * holds it open for as long as they need and stops it when they have heard it
 * or given up. `startCheckTone` is unbroken for the same reason: silence in the
 * middle lets the sink suspend and pay the wake-up cost again.
 *
 * Every probe must reach `new AudioContext()` synchronously from the click that
 * asked for it, which is why starting one is not `async`. That is the same rule
 * the call itself obeys and for the same reason: a context built after an await
 * starts suspended, resumes to nothing, and would report a silence it caused.
 */
import { type AudioRoute, NO_ROUTE, readRoute } from "./audio-route";
import { PlaybackQueue } from "./playback";
import { CHECK_TONE_MAX_SECONDS, type CheckTone, startCheckTone } from "./tones";

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

/** Whether the browser let this probe make a sound, once it has decided. */
export interface ProbeReady {
  ok: boolean;
  /** Why not, when not. `""` when it started. */
  detail: string;
}

/** A probe that is sounding until it is stopped. */
export interface RunningProbe {
  path: ProbePath;
  /**
   * Settles once the browser has decided whether this can be heard at all.
   *
   * Never rejects. Every way a probe can fail is itself a finding, and one that
   * threw would have reported nothing about the fault it was opened to explain.
   */
  ready: Promise<ProbeReady>;
  /**
   * Where the browser says it is sending it, right now.
   *
   * Meant to be polled while the tone runs. A route that *moves* mid-tone is
   * the exact fault this subsystem suspects, and a single reading cannot show a
   * move.
   */
  route(): AudioRoute;
  /** Stops the tone and releases the context. Idempotent. */
  stop(): Promise<void>;
}

/**
 * How long to let the output settle before its latency means anything.
 *
 * `outputLatency` describes a stream that is running; read in the tick the
 * context was resumed it is zero or missing, and a diagnostic reporting zero for
 * the number the whole question turns on is worse than no diagnostic.
 */
export const ROUTE_SETTLE_MS = 300;

const sleep = (ms: number): Promise<void> => new Promise((done) => setTimeout(done, ms));

/**
 * Starts the check tone one way, and hands back the handle that stops it.
 *
 * Synchronous by contract: everything that must happen inside the user's
 * gesture — building the context, starting the tone, asking an element to play
 * — happens before this returns.
 *
 * `onEnded` fires if the cap stops it first, so a surface showing "playing" can
 * stop saying so without polling for it.
 */
export function startProbe(path: ProbePath, onEnded?: () => void): RunningProbe {
  switch (path) {
    case "call":
      return startCallProbe(onEnded);
    case "buffered":
      return startContextProbe("buffered", "playback", onEnded);
    case "element":
      return startElementProbe(onEnded);
    default: {
      // Exhaustive: a fourth way to make a sound must say what it is here.
      const unexpected: never = path;
      return {
        path: unexpected,
        ready: Promise.resolve({ ok: false, detail: "unknown probe" }),
        route: () => NO_ROUTE,
        stop: async () => {},
      };
    }
  }
}

/**
 * The control, and it uses the real class rather than imitating it.
 *
 * Imitating `PlaybackQueue` would make this probe a test of the imitation. If
 * the car plays this, the shipped output path works there.
 */
function startCallProbe(onEnded?: () => void): RunningProbe {
  let queue: PlaybackQueue;
  try {
    queue = new PlaybackQueue();
  } catch (err) {
    return failed("call", String(err));
  }

  let tone: CheckTone | null = null;
  const scheduled = queue.tone((ctx) => {
    tone = startCheckTone(ctx);
  });

  const cap = capTimer(onEnded);
  return {
    path: "call",
    ready: queue.ready().then(async (running) => {
      // The same settle the other two get inside `resume`, so every probe's
      // first route reading is worth the same amount.
      await sleep(ROUTE_SETTLE_MS);
      return {
        ok: scheduled && running,
        detail: running ? "" : "the browser would not run the audio context",
      };
    }),
    route: () => queue.describe(),
    stop: async () => {
      cap.clear();
      tone?.stop();
      await queue.close();
    },
  };
}

/** A bare context with one option changed, so only that option is being tested. */
function startContextProbe(
  path: ProbePath,
  latencyHint: AudioContextLatencyCategory,
  onEnded?: () => void,
): RunningProbe {
  let ctx: AudioContext;
  try {
    ctx = new AudioContext({ latencyHint });
  } catch (err) {
    return failed(path, String(err));
  }

  let tone: CheckTone | null = null;
  let detail = "";
  try {
    tone = startCheckTone(ctx);
  } catch (err) {
    detail = String(err);
  }

  const cap = capTimer(onEnded);
  return {
    path,
    ready: resume(ctx).then((running) => ({
      ok: tone !== null && running,
      detail: running ? detail : "the browser would not run the audio context",
    })),
    route: () => readRoute(ctx),
    stop: async () => {
      cap.clear();
      tone?.stop();
      await closeQuietly(ctx);
    },
  };
}

/**
 * The tone as media: rendered into a stream, played by an element.
 *
 * The element is in the document because a detached one is not reliably played
 * on mobile, and it is removed again — a hidden `<audio>` left behind would
 * hold the route open after the probe that borrowed it is over.
 */
function startElementProbe(onEnded?: () => void): RunningProbe {
  let ctx: AudioContext;
  try {
    ctx = new AudioContext();
  } catch (err) {
    return failed("element", String(err));
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

  let tone: CheckTone | null = null;
  let detail = "";
  try {
    tone = startCheckTone(ctx, sink);
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

  const cap = capTimer(onEnded);
  return {
    path: "element",
    ready: Promise.all([playing, resume(ctx)]).then(([played, running]) => ({
      ok: played && running && tone !== null && !detail,
      detail: running ? detail : "the browser would not run the audio context",
    })),
    route: () => readRoute(ctx),
    stop: async () => {
      cap.clear();
      tone?.stop();
      audio.pause();
      audio.srcObject = null;
      audio.remove();
      await closeQuietly(ctx);
    },
  };
}

/** A probe that never got as far as making a sound. */
function failed(path: ProbePath, detail: string): RunningProbe {
  return {
    path,
    ready: Promise.resolve({ ok: false, detail }),
    route: () => NO_ROUTE,
    stop: async () => {},
  };
}

/**
 * The backstop that stops a forgotten probe.
 *
 * The tone stops itself on the audio clock regardless — `startCheckTone`
 * schedules its own end — so this exists only to tell the surface, and to let
 * the context go.
 */
function capTimer(onEnded?: () => void): { clear: () => void } {
  if (!onEnded) return { clear: () => {} };
  const timer = setTimeout(onEnded, CHECK_TONE_MAX_SECONDS * 1000);
  return { clear: () => clearTimeout(timer) };
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
  // Give the output a moment to exist before anyone reads its latency.
  await sleep(ROUTE_SETTLE_MS);
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
