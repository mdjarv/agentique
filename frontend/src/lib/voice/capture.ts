import { frameLevel, publishMicLevel, resetMicLevel } from "./level";
import workletUrl from "./mic-worklet.js?url";

/** The rate the voice socket expects, fixed by the realtime speech API. */
export const INPUT_SAMPLE_RATE = 16000;

export interface MicCaptureOptions {
  /** Called with each ~32ms frame of Int16 little-endian mono PCM. */
  onFrame: (frame: ArrayBuffer) => void;
  /** Called if the microphone track ends on its own (unplugged, revoked, a call). */
  onEnded?: () => void;
}

/**
 * Microphone capture at the socket's sample rate.
 *
 * The AudioContext is created at 16 kHz so the browser resamples once, in the
 * audio thread, rather than leaving us to do it per frame in JS.
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

    this.ctx = new AudioContext({ sampleRate: INPUT_SAMPLE_RATE });
    // A context created during a gesture can still start suspended on mobile.
    if (this.ctx.state === "suspended") await this.ctx.resume();

    await this.ctx.audioWorklet.addModule(workletUrl);

    this.source = this.ctx.createMediaStreamSource(this.stream);
    this.node = new AudioWorkletNode(this.ctx, "mic-processor");
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
