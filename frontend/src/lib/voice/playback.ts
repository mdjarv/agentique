/**
 * Scheduled playback of streamed PCM.
 *
 * Engine audio arrives as a series of frames that have to play back-to-back
 * with no gap and no overlap. An <audio> element cannot do that, so each frame
 * becomes an AudioBufferSourceNode started at an explicitly computed time and
 * the queue tracks where the next one begins.
 */

/**
 * Cushion applied when the schedule has fallen behind the clock. Starting a
 * source in the past plays it immediately, so several late frames would all
 * fire at once and overlap into noise; this re-anchors slightly ahead instead.
 */
const DRIFT_GUARD_SECONDS = 0.01;

export class PlaybackQueue {
  private ctx: AudioContext;
  private gain: GainNode;
  private sampleRate: number;

  /** Sources still scheduled or playing, so a flush can stop every one. */
  private active: AudioBufferSourceNode[] = [];

  /** Context time at which the next frame should start. */
  private nextStart = 0;

  /**
   * Playback gets its own AudioContext, running at the engine's output rate.
   *
   * Capture runs at 16 kHz and an engine returns audio at its own rate, so a
   * single shared context would resample every played frame down to the
   * capture rate and throw away the difference. Two contexts is the cost of
   * both directions sounding right.
   */
  constructor(sampleRate: number) {
    this.sampleRate = sampleRate;
    this.ctx = new AudioContext({ sampleRate });
    this.gain = this.ctx.createGain();
    this.gain.connect(this.ctx.destination);
  }

  /** Resumes a context that started suspended, as mobile ones do. */
  async ready(): Promise<void> {
    if (this.ctx.state === "suspended") await this.ctx.resume();
  }

  /** Queues one frame of Int16 little-endian mono PCM. */
  enqueue(pcm: ArrayBuffer): void {
    const samples = new Int16Array(pcm);
    if (samples.length === 0) return;

    const floats = new Float32Array(samples.length);
    for (let i = 0; i < samples.length; i++) {
      // Divide by 32768 in both directions: the asymmetry of Int16 costs less
      // than a scale that clips at +1.0.
      floats[i] = (samples[i] ?? 0) / 32768;
    }

    const buffer = this.ctx.createBuffer(1, floats.length, this.sampleRate);
    buffer.getChannelData(0).set(floats);

    const source = this.ctx.createBufferSource();
    source.buffer = buffer;
    source.connect(this.gain);
    source.onended = () => {
      const i = this.active.indexOf(source);
      if (i !== -1) this.active.splice(i, 1);
    };
    this.active.push(source);

    const now = this.ctx.currentTime;
    if (this.nextStart < now) this.nextStart = now + DRIFT_GUARD_SECONDS;
    source.start(this.nextStart);
    this.nextStart += buffer.duration;
  }

  /**
   * Stops everything queued and resets the schedule.
   *
   * This is what makes barge-in work. Without it the engine stops generating
   * the moment it is interrupted, but the browser keeps playing the seconds
   * already queued — straight over the person who interrupted. Call it on
   * every turn_complete, interrupted or not.
   */
  flush(): void {
    for (const source of this.active) {
      try {
        source.stop();
      } catch {
        // Already stopped or never started — nothing to undo.
      }
    }
    this.active = [];
    this.nextStart = 0;
  }

  /** Whether anything is currently scheduled or playing. */
  get isPlaying(): boolean {
    return this.active.length > 0;
  }

  async close(): Promise<void> {
    this.flush();
    this.gain.disconnect();
    if (this.ctx.state !== "closed") {
      try {
        await this.ctx.close();
      } catch {
        // Already closing.
      }
    }
  }
}
