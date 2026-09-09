/**
 * Playback worklet: one ring buffer at the context rate, fed by frames the
 * main thread posts, resampled *statefully* on the way in.
 *
 * It is loaded as a real bundled file, never from a blob: URL — the app's CSP
 * is `script-src 'self'` with no blob:, so the usual runtime-blob trick is
 * blocked (see `vite.config.ts`, `assetsInlineLimit`).
 *
 * ## Why the resampling is here
 *
 * Engine audio arrives as ~30 ms frames at the engine's rate (24 kHz), and the
 * context runs at the hardware's — 48 kHz on a media route, 16 or 8 kHz on a
 * Bluetooth hands-free one. Playback used to build one AudioBuffer per frame
 * at the source rate and let the browser resample each on playback. That
 * resampler carries no state across buffers: every frame boundary is a
 * discontinuity, and thirty of them a second is a periodic click train heard
 * as a buzz under the speech. Upsampling into 48 kHz hides it almost entirely;
 * downsampling 24 → 16 kHz does not, which is exactly "the beep when the right
 * codec works" reported from the car.
 *
 * So the conversion happens once, here, with the read position and the last
 * sample carried from one frame to the next, mirroring what `mic-worklet.js`
 * does in the other direction: a box average over each output sample's window
 * when going down, linear interpolation when going up. The output is then a
 * single continuous stream at the context's own rate, and the browser has
 * nothing left to resample.
 *
 * ## Messages
 *
 * In:  `{ type: "frame", rate, pcm }` — Int16 little-endian mono at `rate`.
 *      `{ type: "clear" }`            — drop everything held (barge-in, hangup).
 * Out: `{ type: "drained" }`          — the buffer just ran empty.
 *      `{ type: "underrun", count }`  — it ran empty mid-stream; see below.
 */

/** Seconds of audio the ring holds before it has to grow. It grows. */
const INITIAL_SECONDS = 2;

/**
 * A gap shorter than this between the buffer running dry and the next frame
 * is an underrun rather than the end of an utterance.
 *
 * The worklet cannot tell "the reply finished" from "the network hiccupped"
 * at the moment the buffer empties — both look like no more samples. It can
 * tell afterwards: a reply's end is followed by the operator speaking and a
 * model thinking, which is seconds, while a starved stream resumes within
 * tens of milliseconds. Counting on resumption is what keeps the number
 * honest about audible gaps and silent about natural pauses.
 */
const UNDERRUN_GAP_SECONDS = 0.25;

class PlaybackProcessor extends AudioWorkletProcessor {
  constructor() {
    super();

    this.ring = new Float32Array(Math.max(1, Math.round(sampleRate * INITIAL_SECONDS)));
    this.readPos = 0;
    this.writePos = 0;
    this.held = 0;

    // The source rate, learned from the first frame and re-learned if it
    // changes. Zero means no frame has arrived yet.
    this.rate = 0;
    // Input samples per output sample. Above 1 when the engine's rate is
    // higher than the context's (24 kHz into a 16 kHz hands-free route),
    // below 1 when lower (24 kHz into 48 kHz), and both happen.
    this.ratio = 1;
    this.resetConversion();

    this.underruns = 0;
    // Context time at which the ring last ran dry, or -1 while it holds
    // samples or after a clear.
    this.dryAt = -1;

    this.port.onmessage = (event) => this.onMessage(event.data);
  }

  resetConversion() {
    // Decimation state: the window being averaged and how far through it we
    // are. Interpolation state: the last input sample of the previous frame
    // and the read position within the current one. All carried across
    // frames — resetting any of them at a frame boundary is the click this
    // worklet exists to remove.
    this.acc = 0;
    this.accCount = 0;
    this.phase = 0;
    this.prev = 0;
    this.pos = 0;
  }

  onMessage(msg) {
    if (!msg || typeof msg !== "object") return;

    if (msg.type === "clear") {
      this.readPos = 0;
      this.writePos = 0;
      this.held = 0;
      this.dryAt = -1;
      this.resetConversion();
      return;
    }

    if (msg.type === "frame") {
      const rate = typeof msg.rate === "number" && msg.rate > 0 ? msg.rate : sampleRate;
      if (rate !== this.rate) {
        this.rate = rate;
        this.ratio = rate / sampleRate;
        this.resetConversion();
      }

      // A frame arriving just after the ring emptied means the stream was
      // starved, not finished.
      if (this.held === 0 && this.dryAt >= 0 && currentTime - this.dryAt < UNDERRUN_GAP_SECONDS) {
        this.underruns++;
        this.port.postMessage({ type: "underrun", count: this.underruns });
      }
      this.dryAt = -1;

      const pcm = msg.pcm;
      if (!(pcm instanceof ArrayBuffer) || pcm.byteLength < 2) return;
      this.convert(new Int16Array(pcm, 0, pcm.byteLength >> 1));
    }
  }

  /** Appends one output-rate sample, growing the ring when it is full. */
  push(sample) {
    if (this.held === this.ring.length) this.grow();
    this.ring[this.writePos] = sample;
    this.writePos = (this.writePos + 1) % this.ring.length;
    this.held++;
  }

  /**
   * Doubles the ring, keeping what it holds in order.
   *
   * An engine can send several seconds of a reply faster than real time, and
   * dropping the excess would cut the end off every long answer. Growth is
   * rare — the ring settles at the longest reply seen — and copying a few
   * seconds of floats is nothing next to a dropped sentence.
   */
  grow() {
    const next = new Float32Array(this.ring.length * 2);
    const tail = this.ring.length - this.readPos;
    next.set(this.ring.subarray(this.readPos, this.readPos + Math.min(tail, this.held)), 0);
    if (this.held > tail) next.set(this.ring.subarray(0, this.held - tail), tail);
    this.ring = next;
    this.readPos = 0;
    this.writePos = this.held;
  }

  /** Converts one frame of Int16 samples to the context rate and holds it. */
  convert(int16) {
    const n = int16.length;
    const floats = new Float32Array(n);
    for (let i = 0; i < n; i++) {
      // Divide by 32768 in both directions: the asymmetry of Int16 costs less
      // than a scale that clips at +1.0.
      floats[i] = int16[i] / 32768;
    }
    if (this.ratio >= 1) this.decimate(floats);
    else this.interpolate(floats);
  }

  /**
   * Down to the context rate, averaging each output sample's whole window.
   *
   * The average is the anti-alias guard: point-sampling 24 kHz down to 16
   * folds everything above 8 kHz back into the speech. A box filter is not a
   * brick wall, but it is the guard that fits in a message handler, and the
   * samples being skipped are already in hand. A ratio of exactly 1 falls out
   * as a pass-through — every window holds one sample.
   */
  decimate(samples) {
    for (let i = 0; i < samples.length; i++) {
      this.acc += samples[i];
      this.accCount++;
      this.phase++;
      if (this.phase >= this.ratio) {
        this.push(this.acc / this.accCount);
        // Subtract rather than reset: 24 → 16 kHz is a ratio of 1.5, and
        // resetting would drift the output rate away from the context's.
        this.phase -= this.ratio;
        this.acc = 0;
        this.accCount = 0;
      }
    }
  }

  /**
   * Up to the context rate, interpolating between neighbouring samples.
   *
   * `prev` makes the previous frame's last sample available, so the first
   * output of a frame is interpolated across the boundary rather than snapped
   * to it — that boundary is the click the per-buffer resampler made.
   */
  interpolate(samples) {
    const n = samples.length;
    // pos is measured over [prev, ...samples]: index 0 is prev, index i is
    // samples[i - 1]. Staying below n keeps both reads in range.
    while (this.pos < n) {
      const i = Math.floor(this.pos);
      const frac = this.pos - i;
      const before = i === 0 ? this.prev : samples[i - 1];
      this.push(before + (samples[i] - before) * frac);
      this.pos += this.ratio;
    }
    this.prev = samples[n - 1];
    // Rebase onto the next frame, whose index 0 is this frame's last sample.
    this.pos -= n;
  }

  process(_inputs, outputs) {
    const out = outputs[0]?.[0];
    if (!out) return true;

    const n = Math.min(out.length, this.held);
    for (let i = 0; i < n; i++) {
      out[i] = this.ring[this.readPos];
      this.readPos = (this.readPos + 1) % this.ring.length;
    }
    // The spec zeroes output buffers before each call; stated anyway, because
    // a stale tail in a reused buffer is a click.
    for (let i = n; i < out.length; i++) out[i] = 0;
    this.held -= n;

    if (n > 0 && this.held === 0) {
      this.dryAt = currentTime;
      this.port.postMessage({ type: "drained" });
    }
    // Always alive: a playback source has nothing to wait for and the next
    // frame can arrive at any time.
    return true;
  }
}

registerProcessor("playback-processor", PlaybackProcessor);
