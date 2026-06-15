import { create } from "zustand";

/**
 * Per-session wire-sequence tracking for the `session.event` push stream.
 *
 * The backend stamps every session.event with a monotonic `seq` (1-based) and
 * an `epoch` (the emitting pipeline's lifetime id). This store keeps the last
 * seen (epoch, lastSeq) per session so the transport layer can:
 *   - drop duplicate / out-of-order redeliveries (seq <= lastSeq),
 *   - detect a gap (seq jump) and trigger a history resync,
 *   - detect a pipeline rebuild (epoch change, e.g. lazy resume) and resync.
 *
 * It is deliberately a DEDICATED store with NO UI subscribers: every
 * session.event — including high-frequency transient deltas — advances the
 * sequence, so recording it in the reactive chat-store would churn the session
 * object reference on every delta and re-render the whole chat view. Here it
 * notifies nobody. O(1) per event, two scalars per session, no per-event Set.
 */

export interface SeqState {
  /** Epoch of the last accepted event, or null when seeded from a history
   *  response for a session that wasn't live (no pipeline → epoch 0). */
  epoch: number | null;
  /** Highest wire seq accepted so far (the high-water mark). */
  lastSeq: number;
}

export type SeqAction = "accept" | "drop" | "resync";

export interface SeqDecision {
  action: SeqAction;
  next: SeqState;
}

/**
 * Pure decision for an incoming event's (epoch, seq) against the prior state.
 *
 * - no prior state → seed and accept (first event for the session).
 * - epoch changed (and was known) → pipeline rebuilt; accept, reseed, RESYNC.
 * - seq === lastSeq + 1 → in order; accept and advance.
 * - seq  >  lastSeq + 1 → gap; accept and advance, but RESYNC to backfill.
 * - seq <= lastSeq → duplicate / out-of-order; DROP (state unchanged).
 *
 * A null prior epoch (seeded from history) is adopted from this event WITHOUT
 * counting as a change, so the post-load high-water seq still gates duplicates.
 */
export function decideSeq(prev: SeqState | undefined, epoch: number, seq: number): SeqDecision {
  if (!prev) {
    return { action: "accept", next: { epoch, lastSeq: seq } };
  }

  if (prev.epoch !== null && epoch !== prev.epoch) {
    return { action: "resync", next: { epoch, lastSeq: seq } };
  }

  const expected = prev.lastSeq + 1;
  if (seq === expected) {
    return { action: "accept", next: { epoch, lastSeq: seq } };
  }
  if (seq > expected) {
    return { action: "resync", next: { epoch, lastSeq: seq } };
  }
  return { action: "drop", next: prev };
}

interface EventSeqStore {
  states: Record<string, SeqState>;
  /** Record the post-decision state for a session. */
  record: (sessionId: string, next: SeqState) => void;
  /**
   * Authoritatively (re)seed from a history response. Overwrites any state a
   * concurrent live event may have set mid-load, so the snapshot's high-water
   * wins — a subsequent live event already in the snapshot (seq <= highWaterSeq)
   * is then dropped. epoch 0 (offline session, no pipeline) is stored as null.
   */
  seedFromHistory: (sessionId: string, epoch: number, highWaterSeq: number) => void;
  /** Clear one session's state (on session.deleted). */
  clearSession: (sessionId: string) => void;
  /** Drop all state (on WS reconnect — history reloads will reseed). */
  reset: () => void;
}

export const useEventSeqStore = create<EventSeqStore>((set) => ({
  states: {},

  record: (sessionId, next) => set((s) => ({ states: { ...s.states, [sessionId]: next } })),

  seedFromHistory: (sessionId, epoch, highWaterSeq) =>
    set((s) => ({
      states: {
        ...s.states,
        [sessionId]: { epoch: epoch > 0 ? epoch : null, lastSeq: highWaterSeq },
      },
    })),

  clearSession: (sessionId) =>
    set((s) => {
      if (!(sessionId in s.states)) return s;
      const { [sessionId]: _, ...rest } = s.states;
      return { states: rest };
    }),

  reset: () => set({ states: {} }),
}));
