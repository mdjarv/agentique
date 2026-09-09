/**
 * Playback of streamed PCM through one worklet.
 *
 * Engine audio arrives as a series of ~30 ms frames that have to play
 * back-to-back with no gap and no overlap. An <audio> element cannot do that,
 * and one AudioBufferSourceNode per frame — the previous design — could, but
 * left the browser to resample each buffer to the context rate on its own,
 * with no state carried from one to the next. Every frame boundary was then a
 * discontinuity, and thirty a second is a buzz under the speech. It hid
 * inside a 48 kHz context and was plain in a 16 kHz one, which is the route a
 * car's hands-free profile puts the call on.
 *
 * So frames are posted to `playback-worklet.js`, which resamples them
 * statefully into a ring buffer at the context's own rate and plays it out as
 * one continuous stream. There is no schedule to keep and no drift to guard:
 * the worklet is pulled by the audio thread and the ring is the queue.
 */

import { type AudioRoute, readRoute } from "./audio-route";
import workletUrl from "./playback-worklet.js?url";

/** Gestures that count as "the operator touched the call" for a retry. */
const GESTURE_EVENTS = ["pointerdown", "touchend", "keydown"] as const;

/** One frame waiting for the worklet node to exist. */
interface PendingFrame {
  rate: number;
  pcm: ArrayBuffer;
}

/** What the worklet says back. Anything else is ignored. */
type WorkletMessage = { type: "drained" } | { type: "underrun"; count: number };

export class PlaybackQueue {
  private ctx: AudioContext;
  private gain: GainNode;

  /** The worklet node, once its module has loaded on the current context. */
  private node: AudioWorkletNode | null = null;

  /**
   * Frames that arrived before the node existed. The module load is a fetch
   * and a parse, and the first frames of a reply must not be lost to it.
   */
  private pending: PendingFrame[] = [];

  /** Whether the ring buffer holds samples, as last reported. */
  private playing = false;

  /** Underruns the worklet has counted on this context. */
  private underrunCount = 0;

  /** Removes the gesture retry listeners, when one is armed. */
  private disarm: (() => void) | null = null;

  /** Set once resume() has been tried and failed: the next gesture rebuilds. */
  private rebuildNext = false;

  private closed = false;

  /**
   * Playback gets its own AudioContext, built at the hardware's rate.
   *
   * Capture converts to 16 kHz in its own worklet and an engine returns audio
   * at its own rate, so a single shared context would resample every played
   * frame down to the capture rate and throw away the difference. Two contexts
   * is the cost of both directions sounding right.
   *
   * The engine's rate is deliberately NOT forced on the context. It is not
   * known when the context is built, and a requested rate is only a request
   * anyway — see `capture.ts`. The context takes the hardware's rate and the
   * worklet converts each frame into it, carrying its state across frames.
   *
   * The constructor is synchronous so it can be reached from inside a user
   * gesture; the worklet module loads behind it and frames wait for it.
   */
  constructor() {
    this.ctx = new AudioContext();
    this.gain = this.ctx.createGain();
    this.gain.connect(this.ctx.destination);
    this.attach(this.ctx);
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
   * Times the ring buffer ran dry mid-stream on this context.
   *
   * A gap the operator hears as a stutter and the health watchdog cannot see:
   * frames arrived, the context ran, and the audio still broke up. The worklet
   * counts only gaps followed by more audio within a quarter second, so the
   * end of a reply is not an underrun.
   */
  get underruns(): number {
    return this.underrunCount;
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
    this.node = null;
    this.pending = [];
    this.playing = false;
    this.attach(next);
    void Promise.resolve()
      .then(() => (old.state === "closed" ? undefined : old.close()))
      .catch(() => {
        // The context we are walking away from. Its failure is not news.
      });
    return true;
  }

  /**
   * Loads the worklet onto a context and wires its node, releasing any frames
   * that arrived while it loaded.
   *
   * Guarded on the context still being current: a rebuild can replace it while
   * the module is in flight, and a node built on the abandoned one would play
   * into a context nobody hears.
   */
  private attach(ctx: AudioContext): void {
    void ctx.audioWorklet
      .addModule(workletUrl)
      .then(() => {
        if (this.closed || this.ctx !== ctx) return;
        const node = new AudioWorkletNode(ctx, "playback-processor", {
          numberOfInputs: 0,
          numberOfOutputs: 1,
          outputChannelCount: [1],
        });
        node.port.onmessage = (event: MessageEvent<WorkletMessage>) => this.onWorklet(event.data);
        node.connect(this.gain);
        this.node = node;
        const waiting = this.pending;
        this.pending = [];
        for (const frame of waiting) this.post(frame);
      })
      .catch((err: unknown) => {
        // Loud, because the symptom is a call with transcripts and no sound,
        // and the likeliest cause is a build that inlined the module where the
        // CSP cannot admit it (see vite.config.ts). Frames keep queueing; the
        // health watchdog reports the silence.
        console.warn("[voice] playback worklet failed to load", err);
      });
  }

  private onWorklet(msg: WorkletMessage): void {
    if (!msg || typeof msg !== "object") return;
    switch (msg.type) {
      case "drained":
        this.playing = false;
        return;
      case "underrun":
        this.underrunCount = msg.count;
        return;
      default:
        return;
    }
  }

  private post(frame: PendingFrame): void {
    if (!this.node) {
      this.pending.push(frame);
      return;
    }
    this.node.port.postMessage({ type: "frame", rate: frame.rate, pcm: frame.pcm }, [frame.pcm]);
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
   * Hands one frame of Int16 little-endian mono PCM, recorded at sampleRate,
   * to the worklet.
   *
   * The rate is the server's announced one, not the context's: the worklet
   * converts between them, which is what lets the context be created before
   * the engine has said anything. The buffer is transferred, not copied — it
   * came off the socket and nothing else reads it.
   */
  enqueue(pcm: ArrayBuffer, sampleRate: number): void {
    if (this.closed) return;
    if (pcm.byteLength < 2) return;

    // A rate the server never announced would be a division by zero in the
    // worklet; the context's own is the only other honest guess.
    const rate = sampleRate > 0 ? sampleRate : this.ctx.sampleRate;
    this.playing = true;
    this.post({ rate, pcm });
  }

  /**
   * Drops everything held and waiting.
   *
   * This is what makes barge-in work. Without it the engine stops generating
   * the moment it is interrupted, but the ring keeps playing the seconds
   * already held — straight over the person who interrupted. Call it on
   * every turn_complete, interrupted or not.
   */
  flush(): void {
    this.pending = [];
    this.playing = false;
    this.node?.port.postMessage({ type: "clear" });
  }

  /** Whether the ring buffer holds samples — audio is, or is about to be, sounding. */
  get isPlaying(): boolean {
    return this.playing;
  }

  async close(): Promise<void> {
    this.closed = true;
    this.disarmGesture();
    this.flush();
    this.node?.port.close();
    this.node?.disconnect();
    this.node = null;
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
