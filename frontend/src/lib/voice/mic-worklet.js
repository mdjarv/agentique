/**
 * Microphone capture worklet.
 *
 * Runs on the audio rendering thread, converts float samples to the Int16 PCM
 * the voice socket carries, and — the part that is load-bearing — converts the
 * *rate* as well. This is an AudioWorklet rather than the older
 * ScriptProcessorNode because ScriptProcessorNode is deprecated, runs on the
 * main thread, and glitches under exactly the load a busy app puts it under.
 *
 * It is loaded as a real bundled file, never from a blob: URL — the app's CSP
 * is `script-src 'self'` with no blob:, so the usual runtime-blob trick is
 * blocked.
 *
 * ## Why the rate conversion is here
 *
 * The capture context used to be constructed at 16 kHz so the browser would
 * resample once, in the audio thread, and the worklet could hand its samples
 * straight to the socket. A requested rate is a request: on a hands-free
 * Bluetooth route the hardware is already at 8 or 16 kHz and it was granted, so
 * capture worked. On every media route — A2DP, projection, a laptop's own
 * speakers — the device runs at 44.1 or 48 kHz, the request is not honoured,
 * and the worklet then posted 48 kHz samples that the socket labelled 16 kHz.
 * Three times too fast is not audio the speech model can transcribe, and the
 * only symptom is a microphone that appears dead on exactly the routes that
 * sound best.
 *
 * So the context now takes the hardware's rate (`capture.ts`), the same rule
 * playback already follows, and the conversion happens here where the real rate
 * is knowable: `sampleRate` in this scope is what the context actually got.
 */

/** The rate the voice socket carries, when the node is given no other. */
const DEFAULT_TARGET_RATE = 16000;

/**
 * Samples per posted frame, at the TARGET rate.
 *
 * ~32 ms of audio. A render quantum is 128 frames, so posting every quantum
 * would mean 125 messages and 125 socket frames a second for 8 ms of audio
 * each — all overhead, no benefit. Counting at the target rate rather than the
 * hardware's is what keeps that duration the same on every device.
 */
const FRAME_SAMPLES = 512;

class MicProcessor extends AudioWorkletProcessor {
  constructor(options) {
    super();
    const asked = options?.processorOptions?.targetSampleRate;
    const target = typeof asked === "number" && asked > 0 ? asked : DEFAULT_TARGET_RATE;

    // Input samples per output sample. Above 1 on every media route, exactly 1
    // on a wideband hands-free one, below 1 on narrowband — all three happen,
    // which is why neither direction may be assumed.
    this.ratio = sampleRate / target;

    this.buf = new Int16Array(FRAME_SAMPLES);
    this.n = 0;

    // Decimation state: the window being averaged, and how far through it we
    // are. Both carry across render quanta — a window reset at every quantum
    // boundary would put a periodic click into the stream at 125 Hz.
    this.acc = 0;
    this.accCount = 0;
    this.phase = 0;

    // Interpolation state: the last input sample of the previous quantum, and
    // the read position of the next output sample within the current one.
    this.prev = 0;
    this.pos = 0;
  }

  /** Buffers one output sample, posting a frame each time one fills. */
  emit(sample) {
    // Clamp before scaling: a sample outside [-1, 1] would wrap to the
    // opposite sign as Int16 and arrive as a click. Interpolation cannot
    // overshoot, but an averaged window of a clipping input can still be at
    // the rail, and the microphone itself is not promised to stay inside.
    const clamped = Math.max(-1, Math.min(1, sample));
    // Asymmetric scale — Int16 range is [-32768, 32767].
    this.buf[this.n++] = clamped < 0 ? clamped * 0x8000 : clamped * 0x7fff;

    if (this.n === this.buf.length) {
      const frame = this.buf.slice();
      this.port.postMessage(frame.buffer, [frame.buffer]);
      this.n = 0;
    }
  }

  /**
   * Down to the target rate, averaging each output sample's whole window.
   *
   * The average is the anti-alias filter. Point-sampling every third sample of
   * a 48 kHz stream folds everything above 8 kHz back into speech, and it costs
   * nothing here to sum the samples that are being skipped anyway. It is a box
   * filter and not a brick wall, which is the honest description: it is the
   * guard that fits in a render quantum, not a resampler.
   *
   * A ratio of exactly 1 falls out of this as a pass-through — every window
   * holds one sample — so a wideband hands-free route needs no special case.
   */
  decimate(channel) {
    for (let i = 0; i < channel.length; i++) {
      this.acc += channel[i];
      this.accCount++;
      this.phase++;
      if (this.phase >= this.ratio) {
        this.emit(this.acc / this.accCount);
        // Subtract rather than reset: the ratio is not an integer on a 44.1 kHz
        // device, and resetting would drift the output rate away from target.
        this.phase -= this.ratio;
        this.acc = 0;
        this.accCount = 0;
      }
    }
  }

  /**
   * Up to the target rate, interpolating between neighbouring samples.
   *
   * Only a narrowband hands-free route gets here. `prev` makes the previous
   * quantum's last sample available, so the first output of a quantum is
   * interpolated rather than snapped to a sample boundary.
   */
  interpolate(channel) {
    const n = channel.length;
    // pos is measured over [prev, ...channel]: index 0 is prev, index i is
    // channel[i - 1]. Staying below n is what keeps both reads in range.
    while (this.pos < n) {
      const i = Math.floor(this.pos);
      const frac = this.pos - i;
      const before = i === 0 ? this.prev : channel[i - 1];
      this.emit(before + (channel[i] - before) * frac);
      this.pos += this.ratio;
    }
    this.prev = channel[n - 1];
    // Rebase onto the next quantum, whose index 0 is this quantum's last
    // sample. Carrying the fraction is what stops a periodic click.
    this.pos -= n;
  }

  process(inputs) {
    const channel = inputs[0]?.[0];
    // No input yet (or the track ended). Returning true keeps the processor
    // alive so capture resumes when audio comes back.
    if (!channel || channel.length === 0) return true;

    if (this.ratio >= 1) this.decimate(channel);
    else this.interpolate(channel);

    return true;
  }
}

registerProcessor("mic-processor", MicProcessor);
