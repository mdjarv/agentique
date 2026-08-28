import { frameLevel, publishMicLevel, resetMicLevel } from "./level";
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

const NO_CAPTURE: CaptureRoute = {
  active: false,
  device: "",
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
}

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
 */
export class MicCapture {
  private ctx: AudioContext | null = null;
  private stream: MediaStream | null = null;
  private node: AudioWorkletNode | null = null;
  private source: MediaStreamAudioSourceNode | null = null;
  private sink: GainNode | null = null;

  async start(opts: MicCaptureOptions): Promise<void> {
    // Echo cancellation is not a nicety here: it is the only thing stopping
    // the agent from hearing its own voice through the speakers and
    // interrupting itself. There is no server-side echo cancellation.
    this.stream = await navigator.mediaDevices.getUserMedia({
      audio: {
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true,
        channelCount: 1,
      },
    });

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
    if (!this.ctx || !track) return NO_CAPTURE;
    const settings = track.getSettings();
    return {
      active: track.readyState === "live",
      device: track.label,
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
