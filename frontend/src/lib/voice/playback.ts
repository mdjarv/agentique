/**
 * Scheduled playback of streamed PCM.
 *
 * Engine audio arrives as a series of frames that have to play back-to-back
 * with no gap and no overlap. An <audio> element cannot do that, so each frame
 * becomes an AudioBufferSourceNode started at an explicitly computed time and
 * the queue tracks where the next one begins.
 */

import { type AudioRoute, readRoute } from "./audio-route";

/**
 * Cushion applied when the schedule has fallen behind the clock. Starting a
 * source in the past plays it immediately, so several late frames would all
 * fire at once and overlap into noise; this re-anchors slightly ahead instead.
 */
const DRIFT_GUARD_SECONDS = 0.01;

/** Gestures that count as "the operator touched the call" for a retry. */
const GESTURE_EVENTS = ["pointerdown", "touchend", "keydown"] as const;

export class PlaybackQueue {
  private ctx: AudioContext;
  private gain: GainNode;

  /** Sources still scheduled or playing, so a flush can stop every one. */
  private active: AudioBufferSourceNode[] = [];

  /** Context time at which the next frame should start. */
  private nextStart = 0;

  /** Removes the gesture retry listeners, when one is armed. */
  private disarm: (() => void) | null = null;

  /** Set once resume() has been tried and failed: the next gesture rebuilds. */
  private rebuildNext = false;

  private closed = false;

  /**
   * Playback gets its own AudioContext, and it is built inside the gesture that
   * started the call.
   *
   * Capture runs at 16 kHz and an engine returns audio at its own rate, so a
   * single shared context would resample every played frame down to the capture
   * rate and throw away the difference. Two contexts is the cost of both
   * directions sounding right.
   *
   * The engine's rate is deliberately NOT forced on the context. It is not
   * known at the moment the operator clicks, and waiting to learn it is what
   * broke playback: a context constructed seconds after the gesture starts
   * suspended and stays suspended, so control frames still rendered and nothing
   * was ever heard. The context takes the hardware's rate instead and each
   * buffer is created at the rate the server announced, which the browser
   * resamples on playback.
   */
  constructor() {
    this.ctx = new AudioContext();
    this.gain = this.ctx.createGain();
    this.gain.connect(this.ctx.destination);
  }

  /** Whether the browser is actually letting audio out of this context. */
  get isRunning(): boolean {
    return this.ctx.state === "running";
  }

  /**
   * The context's own clock, in seconds.
   *
   * Exposed for one reason: a running context's clock always advances, so a
   * clock that has stopped while the context still calls itself "running" is a
   * wedged audio path — which is what a Bluetooth profile switch mid-call looks
   * like from in here.
   */
  get contextTime(): number {
    return this.ctx.currentTime;
  }

  /**
   * Where the browser is sending this queue's audio, as it describes it.
   *
   * Separate from [isRunning] because they answer different questions and only
   * one of them has ever been the fault in a car: running is whether the
   * browser is playing, this is who receives it. The context stays private —
   * what leaves is a record, not a handle.
   */
  describe(): AudioRoute {
    return readRoute(this.ctx);
  }

  /**
   * Resumes the context and reports whether it is running.
   *
   * A boolean rather than a throw, because a suspended context is a normal
   * browser decision rather than an exception: `resume()` on a context created
   * outside a gesture resolves happily while the context stays suspended. The
   * caller decides what to say about it — the point is that it is said, rather
   * than the call sitting mute.
   */
  async ready(): Promise<boolean> {
    if (this.closed) return false;
    if (this.ctx.state === "suspended") {
      try {
        await this.ctx.resume();
      } catch (err) {
        console.warn("[voice] playback resume rejected", err);
      }
    }
    return this.isRunning;
  }

  /**
   * Tries again on the operator's next gesture, and calls back once it works.
   *
   * Belt and braces for mobile, where a lapsed activation is the difference
   * between a call you can hear and one you cannot. Listening at the window
   * rather than on a component keeps it self-contained: anything the operator
   * touches — the call surface included — is a fresh activation.
   */
  resumeOnNextGesture(onResumed: () => void): void {
    if (this.disarm || this.closed || typeof window === "undefined") return;

    const attempt = () => {
      if (this.closed) return;
      // A context that resume() could not revive is not merely suspended, it is
      // wedged — which is what an audio route changing underneath it does, and
      // no amount of resuming fixes that. The next gesture gets a fresh context
      // instead, built synchronously so it is still inside the activation.
      if (this.rebuildNext && this.ctx.state !== "running") {
        this.rebuildNext = false;
        if (!this.rebuild()) return;
      }
      void this.ready().then((running) => {
        if (!running) {
          this.rebuildNext = true;
          return;
        }
        this.disarmGesture();
        onResumed();
      });
    };
    for (const name of GESTURE_EVENTS) {
      window.addEventListener(name, attempt, { capture: true });
    }
    this.disarm = () => {
      for (const name of GESTURE_EVENTS) {
        window.removeEventListener(name, attempt, { capture: true });
      }
    };
  }

  private disarmGesture(): void {
    this.disarm?.();
    this.disarm = null;
  }

  /**
   * Replaces the context with a fresh one, abandoning the old.
   *
   * The old one is not awaited and barely even closed: it is by definition the
   * one that would not run, and a wedged context can leave `close()` pending
   * forever. Anything queued on it is gone, which is the right trade — audio
   * nobody could hear is not worth carrying into a context that works, and
   * recovery applies from the next reply rather than by replaying the last.
   */
  private rebuild(): boolean {
    let next: AudioContext;
    try {
      next = new AudioContext();
    } catch (err) {
      console.warn("[voice] playback rebuild failed", err);
      return false;
    }
    const old = this.ctx;
    this.ctx = next;
    this.gain = next.createGain();
    this.gain.connect(next.destination);
    this.active = [];
    this.nextStart = 0;
    void Promise.resolve()
      .then(() => (old.state === "closed" ? undefined : old.close()))
      .catch(() => {
        // The context we are walking away from. Its failure is not news.
      });
    return true;
  }

  /**
   * Plays a short tone through this call's context, best effort.
   *
   * Here rather than at the call site so the context stays private and every
   * play is guarded in one place: a sound is never worth aborting a call, or a
   * hangup, over. Reports whether anything was scheduled, which is what lets a
   * hangup wait for its own tone before closing the context underneath it.
   */
  tone(play: (ctx: AudioContext) => void): boolean {
    if (this.closed || this.ctx.state === "closed") return false;
    try {
      play(this.ctx);
      return true;
    } catch (err) {
      console.warn("[voice] tone failed", err);
      return false;
    }
  }

  /**
   * Starts a repeating sound through this call's context and hands back its
   * handle, or null if there was no context to start it on.
   *
   * Separate from [tone] because a ring is not a sound you play, it is a sound
   * you stop: the caller owns the handle and is responsible for stopping it,
   * and a null return means there is nothing to stop.
   */
  ring<T>(start: (ctx: AudioContext) => T): T | null {
    if (this.closed || this.ctx.state === "closed") return null;
    try {
      return start(this.ctx);
    } catch (err) {
      console.warn("[voice] ring failed", err);
      return null;
    }
  }

  /**
   * Queues one frame of Int16 little-endian mono PCM, recorded at sampleRate.
   *
   * The rate is the server's announced one, not the context's: the browser
   * resamples a buffer whose rate differs from the context it plays in, which
   * is what lets the context be created before the engine has said anything.
   */
  enqueue(pcm: ArrayBuffer, sampleRate: number): void {
    if (this.closed) return;
    const samples = new Int16Array(pcm);
    if (samples.length === 0) return;

    const floats = new Float32Array(samples.length);
    for (let i = 0; i < samples.length; i++) {
      // Divide by 32768 in both directions: the asymmetry of Int16 costs less
      // than a scale that clips at +1.0.
      floats[i] = (samples[i] ?? 0) / 32768;
    }

    // A rate the server never announced would throw and take the frame with it;
    // the context's own is the only other honest guess.
    const rate = sampleRate > 0 ? sampleRate : this.ctx.sampleRate;
    const buffer = this.ctx.createBuffer(1, floats.length, rate);
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
    this.closed = true;
    this.disarmGesture();
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
