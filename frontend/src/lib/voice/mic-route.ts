/**
 * Whether the microphone the browser opened is the one the car has.
 *
 * On an Android handset paired to a head unit, `getUserMedia` with echo
 * cancellation is what makes the platform enter communication mode and bring
 * up the Bluetooth hands-free link (HFP/SCO). Whether that link actually comes
 * up is not reported anywhere: when it fails, the request quietly resolves to
 * the handset's own microphone on the media route, and the driver is speaking
 * into a phone in the door pocket. The call sounds fine and hears nothing.
 *
 * So the call asks for the Bluetooth input **by device** rather than leaving
 * it to default resolution, and then checks what it got. This module is the
 * check: a pure verdict over the device list, the capture route and the
 * playback route, so the rule can be argued with in a test rather than in a
 * car park. The retry loop that acts on it lives in `call.ts`.
 */
import {
  type AudioDevice,
  type AudioRoute,
  HANDSFREE_RATE_CEILING,
  readDistance,
} from "./audio-route";
import type { CaptureRoute } from "./capture";

/**
 * How Android Chrome names a Bluetooth input.
 *
 * The labels come from a fixed set in the platform's audio manager rather than
 * from the device, which is what makes a substring match honest here: there is
 * exactly one label with this word in it, and it is the one the car answers to.
 */
const BLUETOOTH_LABEL = /bluetooth/i;

/** Whether a device label names a Bluetooth input. */
export function isBluetoothInput(label: string): boolean {
  return BLUETOOTH_LABEL.test(label);
}

/**
 * The Bluetooth input among the listed devices, or null when there is none —
 * which is also what an unlabelled list says, before any permission has been
 * granted.
 */
export function pickBluetoothInput(devices: AudioDevice[]): AudioDevice | null {
  return devices.find((d) => isBluetoothInput(d.label)) ?? null;
}

/** Everything the verdict is drawn from, read just after the microphone opened. */
export interface MicRouteEvidence {
  /** The input devices as enumerated now, with labels if permission allows. */
  devices: AudioDevice[];
  /** What capture actually opened. */
  capture: CaptureRoute;
  /**
   * The playback route, if a context exists yet. `NO_ROUTE` is allowed and
   * makes the rate rule inconclusive rather than wrong.
   */
  playback: AudioRoute;
}

/** Why a retry was asked for. */
export type MicRetryReason = "not-bluetooth" | "media-rate";

export type MicRouteVerdict =
  | { action: "ok" }
  | { action: "retry-with-device"; device: AudioDevice; reason: MicRetryReason };

/**
 * Judges whether the opened microphone is the car's, and if not, which device
 * to re-open with.
 *
 * Two rules, both needing a Bluetooth input to exist — without one there is
 * nothing to retry with, and a handset on its own microphone is the correct
 * outcome rather than a fault:
 *
 * 1. **not-bluetooth** — a Bluetooth input is listed and the opened track is
 *    not it. Default resolution picked the handset, which is the intermittent
 *    failure from the car.
 * 2. **media-rate** — the track is the Bluetooth one but the capture context
 *    came up at a media rate while the output is somewhere external. HFP runs
 *    at 8 or 16 kHz, so a 48 kHz capture on a Bluetooth route means the
 *    hands-free link never came up and the audio is on A2DP, where the car's
 *    microphone does not exist. An output that reads as the handset's own
 *    speaker makes this inconclusive: there is no link to have failed.
 *
 * The identity check accepts either the device id or a Bluetooth label on the
 * track, because a platform may answer an exact-id request with a track
 * labelled by the platform's own name for the device.
 */
export function judgeMicRoute(evidence: MicRouteEvidence): MicRouteVerdict {
  const { devices, capture, playback } = evidence;
  if (!capture.active) return { action: "ok" };

  const bluetooth = pickBluetoothInput(devices);
  if (!bluetooth) return { action: "ok" };

  const opened = capture.deviceId === bluetooth.id || isBluetoothInput(capture.device);
  if (!opened) return { action: "retry-with-device", device: bluetooth, reason: "not-bluetooth" };

  const mediaRate = capture.contextSampleRate > HANDSFREE_RATE_CEILING;
  if (mediaRate && readDistance(playback) === "external") {
    return { action: "retry-with-device", device: bluetooth, reason: "media-rate" };
  }

  return { action: "ok" };
}

/**
 * How many times the call re-opens the microphone before taking what it has.
 *
 * Two, because the switch either works on the second ask or the head unit is
 * not going to do it, and every retry is a pause with the ring silent. A call
 * that gives up here still opens — on the handset's microphone, which the
 * diagnostic says in words — rather than refusing to exist.
 */
export const MIC_ROUTE_RETRIES = 2;

/**
 * Pause between stopping a track and asking again, in ms.
 *
 * Long enough for the platform to tear down the stream it just had — asking
 * again before that is answered from the same routing decision — and short
 * enough that two retries fit inside the ringing anyone would expect.
 */
export const MIC_ROUTE_RETRY_PAUSE_MS = 400;

/** What the status line says while the retry runs. */
export const MIC_ROUTE_MESSAGE = "Connecting to the car's microphone…";
