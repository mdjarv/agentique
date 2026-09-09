/**
 * Where the browser is actually sending a call's audio.
 *
 * `health.ts` asks whether the audio path is *working*; this asks where it
 * goes, and they are different questions. A call can render perfectly into a
 * device nobody is listening to: the context calls itself running, its clock
 * advances, every frame is consumed on time, and the car is silent. Nothing in
 * the health verdict can see that, because by every measure it holds, nothing
 * is wrong.
 *
 * That is the fault wireless Android Auto produces — the handset's media route
 * is not the head unit — and the only evidence for it is the shape of the
 * output the browser opened. So this module reads the route: the facts, and the
 * two independent readings those facts support. It is pure and holds nothing,
 * so a reading can be argued with in a test rather than in a car park.
 *
 * Every field degrades to a stated unknown. `outputLatency` and `sinkId` are
 * recent and not everywhere, and a diagnostic that quietly reports 0 for a
 * number the browser refused to give is worse than one that says it does not
 * know.
 */

/**
 * The parts of `AudioContext` that are newer than the DOM lib we compile
 * against, declared here rather than asserted at each read.
 */
interface RoutingContext {
  outputLatency?: number;
  sinkId?: string | { type: string };
}

/** What the browser will say about the output it opened for one context. */
export interface AudioRoute {
  /** `none` is no context at all, which is different from one that is closed. */
  state: AudioContextState | "none";
  /** Hertz. The hardware rate the context settled on, never one we asked for. */
  sampleRate: number;
  /** Seconds of buffering the context adds. `-1` when unreported. */
  baseLatency: number;
  /** Seconds from `currentTime` to the speaker. `-1` when unreported. */
  outputLatency: number;
  /**
   * The chosen output device, or `""` for the browser default.
   *
   * Android Chrome does not implement output selection, so `""` here is the
   * normal case and is itself the finding: nothing in this app can move the
   * audio, because the platform offers nowhere to move it to.
   */
  sinkId: string;
  /** The context clock in seconds — a stalled one is a wedged path. */
  clock: number;
}

/**
 * No context. A module-level constant because it is returned from a hook path
 * and a fresh object each call re-renders forever (CLAUDE.md).
 */
export const NO_ROUTE: AudioRoute = {
  state: "none",
  sampleRate: 0,
  baseLatency: -1,
  outputLatency: -1,
  sinkId: "",
  clock: 0,
};

/** Reads everything the browser will say about one context's output. */
export function readRoute(ctx: AudioContext | null | undefined): AudioRoute {
  if (!ctx) return NO_ROUTE;
  const extra = ctx as unknown as RoutingContext;
  // `sinkId` is a string for a device and an object for the "no output" sink;
  // only the string form names somewhere audio could come out.
  const sink = extra.sinkId;
  return {
    state: ctx.state,
    sampleRate: ctx.sampleRate,
    baseLatency: typeof ctx.baseLatency === "number" ? ctx.baseLatency : -1,
    outputLatency: typeof extra.outputLatency === "number" ? extra.outputLatency : -1,
    sinkId: typeof sink === "string" ? sink : "",
    clock: ctx.currentTime,
  };
}

/**
 * Seconds of output latency above which the sink is somewhere else.
 *
 * A handset's own speaker or a wired jack is tens of milliseconds. Anything
 * carried over Bluetooth or a projection link buffers in the hundreds, because
 * the link itself has to. The gap between those two is wide and empty, which is
 * what makes one threshold honest.
 */
export const EXTERNAL_LATENCY_SECONDS = 0.1;

/**
 * What the latency says about how far away the speaker is.
 *
 * The reading that matters in a car: it separates "the phone is playing this to
 * itself" from "this is going out over the link", which is the whole question
 * when the car is quiet and the app is convinced it is fine.
 */
export type DistanceReading = "unknown" | "handset" | "external";

export function readDistance(route: AudioRoute): DistanceReading {
  if (route.outputLatency < 0) return "unknown";
  return route.outputLatency >= EXTERNAL_LATENCY_SECONDS ? "external" : "handset";
}

export const DISTANCE_COPY: Record<DistanceReading, string> = {
  unknown: "the browser does not report output latency",
  handset: "short — consistent with the phone's own speaker",
  external: "long — consistent with Bluetooth or a projected car route",
};

/**
 * Highest rate that is a telephony route rather than a media one.
 *
 * Hands-free Bluetooth (HFP/SCO) runs at 8 kHz narrowband or 16 kHz wideband;
 * every media path runs at 44.1 or 48. So a *playback* context that came up at
 * or below this is on the call profile, which is the profile a head unit
 * reserves for telephony and may not render for an app at all.
 */
export const HANDSFREE_RATE_CEILING = 16000;

/** Which Bluetooth profile the output rate implies. */
export type ProfileReading = "unknown" | "media" | "handsfree";

export function readProfile(route: AudioRoute): ProfileReading {
  if (route.sampleRate <= 0) return "unknown";
  return route.sampleRate <= HANDSFREE_RATE_CEILING ? "handsfree" : "media";
}

export const PROFILE_COPY: Record<ProfileReading, string> = {
  unknown: "no output is open",
  media: "a media route (A2DP, projection, or the phone itself)",
  handsfree: "a hands-free/telephony route — a head unit may not play this",
};

/** One audio device the browser is willing to name. */
export interface AudioDevice {
  id: string;
  /** `""` before permission is granted, and on platforms that never label. */
  label: string;
}

/** @deprecated name kept for the diagnostic page; the shape is [AudioDevice]. */
export type OutputDevice = AudioDevice;

export interface AudioDevices {
  /** False when the browser has no device enumeration at all. */
  supported: boolean;
  /**
   * For outputs, empty is a finding rather than a failure: Android Chrome
   * enumerates no output devices, which is the same fact `sinkId` reports from
   * the other side — there is nowhere else for this app to send the audio.
   *
   * For inputs, labels are empty until a microphone permission has been
   * granted, so a list read before the first `getUserMedia` names nothing.
   */
  devices: AudioDevice[];
}

export type OutputDevices = AudioDevices;

const NO_DEVICES: AudioDevices = { supported: false, devices: [] };

/**
 * Lists the audio devices of one kind that the browser admits to, best effort.
 *
 * Never throws: this feeds a diagnostic and a device choice, and either one
 * failing because enumeration was refused reports nothing about the fault it
 * was opened to explain. The refusal degrades to "unsupported", which every
 * caller already handles.
 */
async function listDevices(kind: MediaDeviceKind): Promise<AudioDevices> {
  if (typeof navigator === "undefined" || !navigator.mediaDevices?.enumerateDevices) {
    return NO_DEVICES;
  }
  try {
    const all = await navigator.mediaDevices.enumerateDevices();
    return {
      supported: true,
      devices: all.filter((d) => d.kind === kind).map((d) => ({ id: d.deviceId, label: d.label })),
    };
  } catch {
    return NO_DEVICES;
  }
}

/** The output devices, for the diagnostic's footnote. */
export function listOutputs(): Promise<AudioDevices> {
  return listDevices("audiooutput");
}

/**
 * The input devices, for choosing the car's microphone.
 *
 * Android Chrome labels these from a fixed set ("Default", "Speakerphone",
 * "Wired headset", "Bluetooth headset", "USB audio"), so the label is what a
 * Bluetooth input is recognised by; see `mic-route.ts`.
 */
export function listInputs(): Promise<AudioDevices> {
  return listDevices("audioinput");
}
