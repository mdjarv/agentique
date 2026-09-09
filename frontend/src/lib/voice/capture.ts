import { type AudioDevice, listInputs } from "./audio-route";
import { frameLevel, publishMicLevel, resetMicLevel } from "./level";
import { pickBluetoothInput } from "./mic-route";
import workletUrl from "./mic-worklet.js?url";

/** The rate the voice socket expects, fixed by the realtime speech API. */
export const INPUT_SAMPLE_RATE = 16000;

/**
 * Which microphone the browser actually opened, and how.
 *
 * The device is the half of a car fault this app could never see: asking for
 * echo cancellation is what makes Android pick a communication-mode input, and
 * which physical microphone that resolves to — the handset's or the car's —
 * decides whether the operator is heard and whether the output route moves
 * underneath the call at the same moment.
 */
export interface CaptureRoute {
  /** False when no microphone is open, which makes the rest meaningless. */
  active: boolean;
  /** The track's label. `""` on a platform that will not name it. */
  device: string;
  /** The track's device id, from what the browser granted. `""` when unreported. */
  deviceId: string;
  /**
   * The label of the device that was asked for by id, or `""` when the
   * request was left to the browser's default resolution.
   *
   * Kept separately from [device] because the gap between them is a finding:
   * the Bluetooth input was asked for and the handset's own was opened.
   */
  requestedDevice: string;
  /** Whether a Bluetooth input was enumerated when the microphone was opened. */
  bluetoothListed: boolean;
  /** How many times this capture has been opened, retries included. */
  opens: number;
  /**
   * The rate the capture context settled on — the hardware's, never one we
   * asked for.
   *
   * It is the single most diagnostic number on a car fault, because it names
   * the Bluetooth profile: 8 or 16 kHz is hands-free (HFP/SCO), 44.1 or 48 is
   * a media route. Nothing downstream may assume it equals
   * [INPUT_SAMPLE_RATE]; assuming that is what broke capture on every media
   * route.
   */
  contextSampleRate: number;
  /** What the track itself reports. `0` when it does not. */
  trackSampleRate: number;
  /** The rate the frames leave at, after the worklet's conversion. */
  uploadSampleRate: number;
  /** Whether the browser granted the echo cancellation that was asked for. */
  echoCancellation: boolean;
}

export const NO_CAPTURE: CaptureRoute = {
  active: false,
  device: "",
  deviceId: "",
  requestedDevice: "",
  bluetoothListed: false,
  opens: 0,
  contextSampleRate: 0,
  trackSampleRate: 0,
  uploadSampleRate: 0,
  echoCancellation: false,
};

export interface MicCaptureOptions {
  /** Called with each ~32ms frame of Int16 little-endian mono PCM. */
  onFrame: (frame: ArrayBuffer) => void;
  /** Called if the microphone track ends on its own (unplugged, revoked, a call). */
  onEnded?: () => void;
  /**
   * The input to ask for by id. Absent means "the Bluetooth input if one is
   * listed, else the browser's default" — the choice a call wants on its first
   * open. A retry passes the device the judge named.
   */
  device?: AudioDevice;
}

/**
 * The constraints every open asks for, whichever device carries them.
 *
 * Echo cancellation is not a nicety here: it is the only thing stopping the
 * agent from hearing its own voice through the speakers and interrupting
 * itself. There is no server-side echo cancellation. It is also what puts
 * Android into communication mode, which is what starts the hands-free link.
 */
const BASE_CONSTRAINTS: MediaTrackConstraints = {
  echoCancellation: true,
  noiseSuppression: true,
  autoGainControl: true,
  channelCount: 1,
};

/**
 * Microphone capture, converted to the socket's sample rate in the worklet.
 *
 * The context deliberately does NOT ask for [INPUT_SAMPLE_RATE], which is the
 * same rule playback settled on and for a closely related reason. A requested
 * rate is a request: a hands-free Bluetooth route is already at 8 or 16 kHz and
 * granted it, so capture worked there and only there. On a media route — A2DP,
 * projection, a laptop's own speakers — the device runs at 44.1 or 48 kHz, the
 * request is quietly not honoured, and nothing here checked, so every frame
 * went up mislabelled at three times its real speed. A microphone that fails on
 * exactly the routes that sound best is what that looks like from the car.
 *
 * Asking also had a cost even when it worked: a 16 kHz capture context shares
 * the audio device with playback, and pinning it to a telephony rate is a way
 * to drag the whole route onto the telephony profile.
 *
 * So the context takes the hardware's rate and the worklet converts, which is
 * the one place the real rate is knowable.
 *
 * **The device is chosen, not left to resolution.** Chrome on Android lists
 * inputs under fixed labels, and asking for the one labelled Bluetooth by exact
 * id is what makes it start the hands-free link deterministically. Left to the
 * default, the platform sometimes brings the link up and sometimes settles on
 * the handset's own microphone — which is the intermittent silence from the
 * car. An exact request the browser refuses falls back to the unconstrained
 * one, because a microphone is better than none, and what was asked for is
 * recorded so the diagnostic can show the gap.
 */
export class MicCapture {
  private ctx: AudioContext | null = null;
  private stream: MediaStream | null = null;
  private node: AudioWorkletNode | null = null;
  private source: MediaStreamAudioSourceNode | null = null;
  private sink: GainNode | null = null;

  private requestedDevice = "";
  private bluetoothListed = false;
  private opens = 0;

  async start(opts: MicCaptureOptions): Promise<void> {
    this.opens++;

    // Enumerate first, every time: labels are empty until a permission has
    // been granted, so the list a retry sees can name a device the first open
    // could not.
    const listed = await listInputs();
    const bluetooth = pickBluetoothInput(listed.devices);
    this.bluetoothListed = bluetooth !== null;
    const wanted = opts.device ?? bluetooth ?? undefined;
    this.requestedDevice = wanted?.label ?? "";

    this.stream = await openStream(wanted);

    const track = this.stream.getAudioTracks()[0];
    if (track && opts.onEnded) {
      track.addEventListener("ended", opts.onEnded);
    }

    this.ctx = new AudioContext();
    // A context created during a gesture can still start suspended on mobile.
    if (this.ctx.state === "suspended") await this.ctx.resume();

    await this.ctx.audioWorklet.addModule(workletUrl);

    this.source = this.ctx.createMediaStreamSource(this.stream);
    // The target rides the node rather than being compiled into the worklet, so
    // the socket's rate is stated once, here, next to the code that sends it.
    this.node = new AudioWorkletNode(this.ctx, "mic-processor", {
      processorOptions: { targetSampleRate: INPUT_SAMPLE_RATE },
    });
    this.node.port.onmessage = (event: MessageEvent<ArrayBuffer>) => {
      // The level is a scan of a frame we already hold — no second audio node,
      // no analyser, no extra graph. Measured before the frame is handed on,
      // because the socket send is the slower of the two.
      publishMicLevel(frameLevel(event.data));
      opts.onFrame(event.data);
    };

    // A worklet only runs while it is connected to the graph, but connecting it
    // to the destination would play the microphone back through the speakers.
    // A muted sink keeps it pulled without making a sound.
    this.sink = this.ctx.createGain();
    this.sink.gain.value = 0;
    this.source.connect(this.node);
    this.node.connect(this.sink);
    this.sink.connect(this.ctx.destination);
  }

  /**
   * Which microphone is open, best effort and never throwing.
   *
   * `getSettings` is what the browser *granted* rather than what was asked
   * for, which is the only version worth reporting: a request for echo
   * cancellation that was quietly refused looks identical from the call site
   * and sounds like the agent interrupting itself.
   */
  describe(): CaptureRoute {
    const track = this.stream?.getAudioTracks()[0];
    if (!this.ctx || !track) {
      return { ...NO_CAPTURE, opens: this.opens, bluetoothListed: this.bluetoothListed };
    }
    const settings = track.getSettings();
    return {
      active: track.readyState === "live",
      device: track.label,
      deviceId: settings.deviceId ?? "",
      requestedDevice: this.requestedDevice,
      bluetoothListed: this.bluetoothListed,
      opens: this.opens,
      contextSampleRate: this.ctx.sampleRate,
      trackSampleRate: settings.sampleRate ?? 0,
      uploadSampleRate: INPUT_SAMPLE_RATE,
      // Some browsers answer with a mode ("all", "remote-only") rather than a
      // flag. Any of them means it was granted, which is the whole question.
      echoCancellation: Boolean(settings.echoCancellation),
    };
  }

  async stop(): Promise<void> {
    resetMicLevel();
    this.node?.port.close();
    this.node?.disconnect();
    this.source?.disconnect();
    this.sink?.disconnect();

    // Release the microphone before closing the context: the recording
    // indicator stays lit otherwise, which reads as "still listening".
    for (const track of this.stream?.getTracks() ?? []) track.stop();

    const ctx = this.ctx;
    this.node = null;
    this.source = null;
    this.sink = null;
    this.stream = null;
    this.ctx = null;

    if (ctx && ctx.state !== "closed") {
      try {
        await ctx.close();
      } catch {
        // Already closing — the tracks are released either way.
      }
    }
  }
}

/**
 * Opens the microphone, by exact device when one is named.
 *
 * An exact request can be refused for reasons that have nothing to do with
 * permission — the device went away between the list and the ask, or the
 * platform will not honour the id — and the unconstrained request is the
 * answer to all of them. A permission refusal fails both the same way, and the
 * second throw is the one the caller sees.
 */
async function openStream(device?: AudioDevice): Promise<MediaStream> {
  if (!device) {
    return navigator.mediaDevices.getUserMedia({ audio: BASE_CONSTRAINTS });
  }
  try {
    return await navigator.mediaDevices.getUserMedia({
      audio: { ...BASE_CONSTRAINTS, deviceId: { exact: device.id } },
    });
  } catch (err) {
    console.warn("[voice] exact microphone refused, falling back", device.label, err);
    return navigator.mediaDevices.getUserMedia({ audio: BASE_CONSTRAINTS });
  }
}
