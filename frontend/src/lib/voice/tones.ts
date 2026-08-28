/**
 * The sounds a call makes about itself: placing, ringing, connected, ended.
 *
 * Synthesised rather than shipped. An oscillator and a gain envelope are a few
 * hundred bytes of code with no asset to inline, nothing for the CSP to judge,
 * and no decode step between the click and the sound — which matters, because
 * the dial tone's second job is proof. It is played from the gesture that
 * opened the call, through the very context playback will use, so an operator
 * who hears it knows the audio path works before the model has said a word,
 * and one who hears nothing has learned something the silence would otherwise
 * have hidden.
 *
 * Every function here is pure in the way that matters: it takes the context,
 * schedules against that context's clock, and keeps no state of its own.
 */

/**
 * Peak gain. Modest on purpose — this plays in someone's ear, on a phone, and
 * a status beep that makes you flinch is worse than no status beep.
 */
const PEAK = 0.1;

/**
 * Ramp length at each end of a note.
 *
 * Not a nicety: a gain that steps from 0 to peak is a discontinuity, and a
 * discontinuity is a click. A few milliseconds either side is inaudible as a
 * fade and is the whole difference between a tone and a pop.
 */
const RAMP_SECONDS = 0.008;

/** How long the ending sound needs before its context may be closed. */
export const HANGUP_TONE_SECONDS = 0.25;

/** One note in a tone: when it starts relative to now, and how it sounds. */
interface Note {
  /** Hertz. */
  freq: number;
  /** Seconds after the tone begins. */
  at: number;
  /** Seconds of sound. Must comfortably exceed two ramps. */
  duration: number;
  /** Peak gain, 0..1. */
  peak: number;
}

/** A note that is scheduled or sounding, kept only so it can be cut short. */
interface Sounding {
  osc: OscillatorNode;
  gain: GainNode;
}

/**
 * Schedules a short sequence of enveloped sine notes and lets them clean
 * themselves up.
 *
 * Everything is scheduled against `ctx.currentTime` up front rather than timed
 * with setTimeout: the audio clock is the only one that does not drift under a
 * busy main thread, which is the same reason playback schedules its frames.
 *
 * The nodes come back so a caller that has to *interrupt* a sound can — only
 * the ringback needs that, and only because "stop" has to mean stop rather than
 * "after this burst".
 *
 * The destination defaults to the context's own and is a parameter for exactly
 * one caller: the output probe that plays through a `MediaStreamDestination`
 * to find out whether a car renders an element's audio when it will not render
 * a context's. A tone that could only reach `ctx.destination` could not answer
 * that question.
 */
function playNotes(ctx: AudioContext, notes: Note[], destination?: AudioNode): Sounding[] {
  const begin = ctx.currentTime;
  const out = destination ?? ctx.destination;
  const sounding: Sounding[] = [];

  for (const note of notes) {
    const start = begin + note.at;
    const end = start + note.duration;

    const osc = ctx.createOscillator();
    osc.type = "sine";
    osc.frequency.setValueAtTime(note.freq, start);

    const gain = ctx.createGain();
    gain.gain.setValueAtTime(0, start);
    gain.gain.linearRampToValueAtTime(note.peak, start + RAMP_SECONDS);
    gain.gain.setValueAtTime(note.peak, end - RAMP_SECONDS);
    gain.gain.linearRampToValueAtTime(0, end);

    osc.connect(gain);
    gain.connect(out);

    osc.onended = () => {
      osc.disconnect();
      gain.disconnect();
    };
    osc.start(start);
    osc.stop(end);
    sounding.push({ osc, gain });
  }

  return sounding;
}

/**
 * Cuts notes short, fading rather than chopping.
 *
 * Stopping an oscillator outright mid-cycle is the same discontinuity a missing
 * ramp is, so the gain is ramped to nothing first and the node stops after it.
 */
function silence(ctx: AudioContext, notes: Sounding[]): void {
  const now = ctx.currentTime;
  for (const { osc, gain } of notes) {
    try {
      gain.gain.cancelScheduledValues(now);
      gain.gain.setValueAtTime(gain.gain.value, now);
      gain.gain.linearRampToValueAtTime(0, now + RAMP_SECONDS);
      osc.stop(now + RAMP_SECONDS);
    } catch {
      // Already stopped, or never started. Either way there is nothing to cut.
    }
  }
}

/**
 * Placing the call: two short rising notes.
 *
 * Rising because the call is going out. Played inside the click, which is also
 * what unlocks the context on a mobile browser.
 */
export function playDialTone(ctx: AudioContext): void {
  playNotes(ctx, [
    { freq: 660, at: 0, duration: 0.09, peak: PEAK },
    { freq: 880, at: 0.11, duration: 0.09, peak: PEAK },
  ]);
}

/**
 * How long the output check sounds for.
 *
 * Far longer than any status tone, and deliberately: this one is listened *for*
 * rather than noticed, in a moving car, by someone who has just pressed a
 * button and is waiting to find out whether the speakers are ours. A blip at
 * road speed is indistinguishable from silence.
 */
export const CHECK_TONE_SECONDS = 2;

/**
 * The output probe's tone: two long notes, an octave apart.
 *
 * Nothing else this app plays is sustained, so hearing it is unambiguous —
 * which is the whole job, since the operator's ear is the measurement and the
 * numbers beside it only say where it was sent.
 */
export function playCheckTone(ctx: AudioContext, destination?: AudioNode): void {
  playNotes(
    ctx,
    [
      { freq: 440, at: 0, duration: 0.9, peak: PEAK * 1.5 },
      { freq: 880, at: 1, duration: 1, peak: PEAK * 1.5 },
    ],
    destination,
  );
}

/** Seconds of sound in one ring burst. */
const RING_BURST_SECONDS = 0.4;

/** Seconds of silence between bursts. A phone's cadence, roughly. */
const RING_GAP_SECONDS = 1.8;

/**
 * Seconds between the dial tone and the first ring.
 *
 * Long enough that the two do not overlap — the dial tone is two notes over
 * 0.2s — and short enough that a call refused instantly never rings at all.
 */
const RING_LEAD_SECONDS = 0.35;

/** The two notes of a burst. A pair beats one note: it reads as a phone. */
const RING_LOW = 440;
const RING_HIGH = 480;

/**
 * A ringback that is playing until something stops it.
 *
 * A handle rather than a duration because the thing that ends it is an event —
 * the call going live, failing, or being hung up — and none of those can be
 * predicted from here.
 */
export interface Ringback {
  /** Silences it now, within the burst. Idempotent. */
  stop(): void;
}

/**
 * Ringing, until it is answered: a gentle dual-tone burst, then a long gap.
 *
 * The explicit reason is eyes-free: between placing a call and it going live
 * there is a socket, a briefing and a speech-model handshake, and someone
 * driving has no way to tell "still connecting" from "dead" without looking. A
 * ring says connecting, out loud, for as long as it is true.
 *
 * The second reason is diagnosis, and it is the one that pays in a car. The
 * ring plays through the very context the model's audio will use, continuously,
 * across the moment the microphone opens — which on Bluetooth hands-free is
 * when the handset switches profile and reroutes the audio. If the ring dies
 * there, the operator has *heard* the output path break, at the instant it
 * broke, rather than inferring it from silence later.
 *
 * Each burst is scheduled against `ctx.currentTime` when its timer fires, never
 * queued up ahead: bursts scheduled into the future would keep sounding after
 * the call went live, and the one thing a ringback must never do is play over a
 * live call.
 */
export function startRingback(ctx: AudioContext): Ringback {
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let sounding: Sounding[] = [];

  const burst = () => {
    timer = undefined;
    if (stopped) return;
    try {
      sounding = playNotes(ctx, [
        { freq: RING_LOW, at: 0, duration: RING_BURST_SECONDS, peak: PEAK * 0.55 },
        { freq: RING_HIGH, at: 0, duration: RING_BURST_SECONDS, peak: PEAK * 0.55 },
      ]);
    } catch {
      // A context that has gone away mid-ring ends the ring, never the call.
      stopped = true;
      return;
    }
    timer = setTimeout(burst, (RING_BURST_SECONDS + RING_GAP_SECONDS) * 1000);
  };

  timer = setTimeout(burst, RING_LEAD_SECONDS * 1000);

  return {
    stop() {
      if (stopped) return;
      stopped = true;
      if (timer !== undefined) clearTimeout(timer);
      timer = undefined;
      silence(ctx, sounding);
      sounding = [];
    },
  };
}

/**
 * The call went live: one short, higher, quieter blip.
 *
 * It earns its place on latency alone. Opening a call means a socket, a
 * briefing and a speech-model handshake, and until the first word is spoken
 * "still connecting" and "connected, waiting for you" look identical. Quieter
 * than the dial tone because it is an acknowledgement, not an announcement.
 */
export function playConnectedTone(ctx: AudioContext): void {
  playNotes(ctx, [{ freq: 1040, at: 0, duration: 0.07, peak: PEAK * 0.6 }]);
}

/**
 * The call ended: two short falling notes.
 *
 * One sound for every ending — the operator hanging up, the idle guard hanging
 * up, the engine failing. A distinct failure tone would be a second vocabulary
 * to learn for a fact the screen already carries in words, and the thing worth
 * hearing is the same either way: the line is down.
 */
export function playHangupTone(ctx: AudioContext): void {
  playNotes(ctx, [
    { freq: 660, at: 0, duration: 0.09, peak: PEAK },
    { freq: 440, at: 0.11, duration: 0.11, peak: PEAK },
  ]);
}
