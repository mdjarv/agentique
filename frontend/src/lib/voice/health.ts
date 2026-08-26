/**
 * Why a live call has gone quiet.
 *
 * A call in a car is diagnosed by ear or not at all, and "silence" is three
 * different faults wearing the same face: nobody heard you, something answered
 * and the audio never came, or the audio came and could not be played. The
 * operator cannot tell them apart, and until this existed neither could the
 * app — the standing suspicion, a Bluetooth profile switch when the microphone
 * opens, breaks any of the three depending on which way the route falls.
 *
 * So each one is named from evidence the client already holds: the microphone
 * level, the engine's own transcripts, and the PCM frames it enqueues. This
 * file is the judgement and nothing else — no timers, no context, no store — so
 * the three verdicts can be argued with in a table rather than in a car.
 */

/** What the call is currently suffering from. "ok" is the absence of all three. */
export type VoiceAudioHealth = "ok" | "cannot-play" | "mic-silent" | "no-audio";

/**
 * What each fault is told to the operator as.
 *
 * Written as what to check rather than what went wrong, because the reader is
 * driving. `cannot-play` keeps the wording the blocked-audio line already used:
 * it is the same fault, and the same gesture fixes it.
 */
export const HEALTH_MESSAGE: Record<Exclude<VoiceAudioHealth, "ok">, string> = {
  "cannot-play": "Sound is blocked — tap the call to enable it",
  "mic-silent": "The microphone is picking up nothing — check Bluetooth audio.",
  "no-audio": "The assistant replied but no audio is arriving.",
};

/**
 * Mic level below which the input is silence rather than a quiet room.
 *
 * The published level is `sqrt(peak / 32768)`, so this is a peak amplitude of
 * about 13 out of 32768 — digital silence with rounding, not a quiet passenger.
 * A microphone that a route switch has stranded reports exactly zero.
 */
export const MIC_SILENCE_FLOOR = 0.02;

/**
 * How long the microphone must hear nothing before that is worth saying.
 *
 * Long enough to cover a pause before the operator speaks first, short enough
 * that they are told before they have said a whole sentence into a dead mic.
 */
export const MIC_SILENCE_MS = 6000;

/** How long after the engine speaks its audio may take to start arriving. */
export const REPLY_AUDIO_MS = 5000;

/** How recently a PCM frame must have arrived to count as "audio is arriving". */
export const FRAME_RECENT_MS = 5000;

/** Everything the verdict is drawn from, sampled at one instant. */
export interface AudioHealthSample {
  /** Now, in epoch milliseconds. */
  now: number;
  /** When the call went live, in epoch milliseconds. 0 while it is not live. */
  liveSince: number;
  /** Last time the microphone was above the floor. 0 = not once this call. */
  micSoundAt: number;
  /** Last engine transcript received. 0 = the assistant has not spoken. */
  engineSpokeAt: number;
  /** Last PCM frame received. 0 = no audio has arrived at all. */
  audioFrameAt: number;
  /** Whether the playback context reports itself running. */
  audioRunning: boolean;
  /**
   * Whether the context's clock moved since the previous check.
   *
   * A running context's clock always advances, so one that has stopped while
   * still calling itself running is a wedged audio path. Unknown — the first
   * check, with nothing to compare against — is true: not yet proven broken.
   */
  clockAdvancing: boolean;
}

/**
 * Names the one thing most worth saying about a live call's audio.
 *
 * One verdict, never a summary: the status line holds a single message, and a
 * line reporting three faults at once reports none of them. They are ranked by
 * what the operator would do about them.
 *
 * 1. **cannot-play** — the output path is provably broken, and it is the only
 *    one with a fix: a gesture. It outranks the rest because everything else
 *    would be inaudible anyway.
 * 2. **mic-silent** — nothing is being heard, so anything downstream (including
 *    a missing reply) follows from it. Naming the cause beats naming the
 *    symptom.
 * 3. **no-audio** — something answered and its audio never came. Last because
 *    it needs the two above to be healthy before it means anything.
 *
 * The first two cannot both be true of the same evidence in the way that
 * matters — `cannot-play` requires either a dead context or frames that are
 * arriving, and `no-audio` requires that no frames arrived — so the ranking
 * settles overlap rather than hiding it.
 */
export function assessAudioHealth(s: AudioHealthSample): VoiceAudioHealth {
  // Nothing is wrong with a call that is not live yet: there is no microphone
  // open, and the assistant has not been given a chance to say anything.
  if (s.liveSince <= 0 || s.now < s.liveSince) return "ok";

  const framesArriving = s.audioFrameAt > 0 && s.now - s.audioFrameAt <= FRAME_RECENT_MS;
  if (!s.audioRunning || (framesArriving && !s.clockAdvancing)) return "cannot-play";

  const heardNothing = s.micSoundAt <= 0 || s.now - s.micSoundAt >= MIC_SILENCE_MS;
  if (s.now - s.liveSince >= MIC_SILENCE_MS && heardNothing) return "mic-silent";

  const spokeLongEnoughAgo = s.engineSpokeAt > 0 && s.now - s.engineSpokeAt >= REPLY_AUDIO_MS;
  if (spokeLongEnoughAgo && s.audioFrameAt < s.engineSpokeAt) return "no-audio";

  return "ok";
}
