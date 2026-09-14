/**
 * Where an utterance begins and ends, judged from the microphone level.
 *
 * Server dictation needs this because the speech service cannot: a Gemini Live
 * session with its own activity detection sometimes never returned the second
 * sentence of a dictation, and with detection off it transcribes nothing until
 * the client closes an utterance (TestDictationProbeLive). So the client opens
 * an utterance when the level rises and closes it after a pause, and the text
 * for it comes back about 0.3s later.
 *
 * Pure: levels and clock in, `start`/`end` out. The thresholds are a first
 * reading, not a tuned result — they want a listen on real microphones, and
 * once settled they get pinned by a test rather than drifting.
 */

/** The published level is `sqrt(peak / 32768)` (`lib/voice/level.ts`). */
export interface UtteranceGateOptions {
  /** Level that opens an utterance, before the noise floor raises it. */
  openLevel?: number;
  /** Level that keeps one open; below it counts as pause. Hysteresis. */
  holdLevel?: number;
  /**
   * How long a pause closes the utterance. Long enough to span the gap
   * between words and a breath mid-sentence — a sentence split at every
   * comma comes back as fragments — short enough that text follows speech.
   */
  pauseMs?: number;
  /** How far above the measured room noise speech must rise to open. */
  floorFactor?: number;
}

export type GateEvent = "start" | "end" | null;

export const UTTERANCE_PAUSE_MS = 800;

export class UtteranceGate {
  private readonly openLevel: number;
  private readonly holdLevel: number;
  private readonly pauseMs: number;
  private readonly floorFactor: number;

  private open = false;
  private lastVoiceAt = 0;
  /** Slow average of the level while no utterance is open: the room. */
  private floor = 0;

  constructor(opts: UtteranceGateOptions = {}) {
    this.openLevel = opts.openLevel ?? 0.14;
    this.holdLevel = opts.holdLevel ?? 0.09;
    this.pauseMs = opts.pauseMs ?? UTTERANCE_PAUSE_MS;
    this.floorFactor = opts.floorFactor ?? 2.5;
  }

  get isOpen(): boolean {
    return this.open;
  }

  /** Feed one frame's level; returns the edge it caused, if any. */
  feed(level: number, now: number): GateEvent {
    if (!this.open) {
      // Track the room only while nobody is speaking, so speech never raises
      // the bar it has to clear.
      // Rising from zero rather than seeded with the first frame: a first
      // frame that is already speech would set the floor at a voice.
      this.floor = this.floor * 0.95 + level * 0.05;
      if (level >= Math.max(this.openLevel, this.floor * this.floorFactor)) {
        this.open = true;
        this.lastVoiceAt = now;
        return "start";
      }
      return null;
    }
    if (level >= this.holdLevel) {
      this.lastVoiceAt = now;
      return null;
    }
    if (now - this.lastVoiceAt >= this.pauseMs) {
      this.open = false;
      return "end";
    }
    return null;
  }

  /** Close whatever is open, for a stop. */
  flush(): GateEvent {
    if (!this.open) return null;
    this.open = false;
    return "end";
  }
}
