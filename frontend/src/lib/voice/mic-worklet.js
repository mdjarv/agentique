/**
 * Microphone capture worklet.
 *
 * Runs on the audio rendering thread and converts float samples to the Int16
 * PCM the voice socket carries. This is an AudioWorklet rather than the older
 * ScriptProcessorNode because ScriptProcessorNode is deprecated, runs on the
 * main thread, and glitches under exactly the load a busy app puts it under.
 *
 * It is loaded as a real bundled file, never from a blob: URL — the app's CSP
 * is `script-src 'self'` with no blob:, so the usual runtime-blob trick is
 * blocked.
 */

/**
 * Samples per posted frame. A render quantum is 128 frames, so at a 16 kHz
 * context this batches four of them into ~32 ms.
 *
 * Posting every quantum would mean 125 messages and 125 socket frames a second
 * for 8 ms of audio each — all overhead, no benefit.
 */
const FRAME_SAMPLES = 512;

class MicProcessor extends AudioWorkletProcessor {
  constructor() {
    super();
    this.buf = new Int16Array(FRAME_SAMPLES);
    this.n = 0;
  }

  process(inputs) {
    const channel = inputs[0]?.[0];
    // No input yet (or the track ended). Returning true keeps the processor
    // alive so capture resumes when audio comes back.
    if (!channel) return true;

    for (let i = 0; i < channel.length; i++) {
      // Clamp before scaling: a sample outside [-1, 1] would wrap to the
      // opposite sign as Int16 and arrive as a click.
      const sample = Math.max(-1, Math.min(1, channel[i]));
      // Asymmetric scale — Int16 range is [-32768, 32767].
      this.buf[this.n++] = sample < 0 ? sample * 0x8000 : sample * 0x7fff;

      if (this.n === this.buf.length) {
        const frame = this.buf.slice();
        this.port.postMessage(frame.buffer, [frame.buffer]);
        this.n = 0;
      }
    }

    return true;
  }
}

registerProcessor("mic-processor", MicProcessor);
