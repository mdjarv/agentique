import { parseServerEvent } from "~/lib/events";
import {
  loadSessionHistory,
  parkLiveEventDuringLoad,
  setLiveEventReplayer,
} from "~/lib/session/history";
import type { WsClient } from "~/lib/ws-client";
import { applyEvent } from "~/stores/event-orchestrator";
import { decideSeq, useEventSeqStore } from "~/stores/event-seq";

/** The `session.event` push payload, as the wire carries it. */
export interface SessionEventPayload {
  sessionId: string;
  event: unknown;
  /** Wire sequence, 1-based; 0 or absent means unsequenced. */
  seq?: number;
  /** Emitting pipeline's lifetime id; 0 or absent means no pipeline. */
  epoch?: number;
}

/**
 * The single entry point for a live `session.event`: wire-sequence gate first,
 * then parse, then the cross-store apply.
 *
 * The gate judges the RAW payload, before and independent of parsing. Every
 * stamped event advances the server's counter — including types this build
 * does not render (`workflow_launched` is persisted and seq-stamped but
 * ignored, and a newer server can add types this build has never heard of).
 * When the gate ran after the parse, each such event returned early without
 * recording its seq, so the next real event arrived at lastSeq+2 and a
 * manufactured "gap" forced a full-history resync mid-turn. Only the apply is
 * skipped for an unparseable event; its seq always counts.
 */
export function ingestSessionEvent(ws: WsClient, payload: SessionEventPayload): void {
  const sid = payload.sessionId;

  // While a history load is in flight for this session, nothing applies: the
  // snapshot will replace the turns wholesale and reseed the seq tracker, so
  // anything applied during the round trip would be wiped and read as
  // already-seen. The payload parks in history.ts and replays through this
  // same function once the snapshot has landed.
  if (parkLiveEventDuringLoad(payload)) return;

  // --- Wire-sequence gate (runs FIRST, before any store mutation) ---
  // seq 0 = unsequenced (e.g. a channel message to an offline session) —
  // skip ordering/dedup checks and apply directly. Otherwise drop
  // duplicates/out-of-order, and resync on a gap or pipeline rebuild.
  const seq = payload.seq ?? 0;
  if (seq > 0) {
    const prev = useEventSeqStore.getState().states[sid];
    const { action, next } = decideSeq(prev, payload.epoch ?? 0, seq);
    if (action === "drop") return;
    if (action === "resync") {
      // Backfill missed events. Coalesced by loadSessionHistory's
      // historyLoading in-flight guard; the force-load reseeds the seq
      // state authoritatively from the response's high-water mark.
      loadSessionHistory(ws, sid, true);
      // The trigger itself parks too: the snapshot the load fetches will
      // contain it, and applying it now would mutate turns the snapshot is
      // about to replace. Recording is skipped for the same reason — the
      // replay re-runs the gate against the seeded state. Falls through to
      // record+apply only when the load did not begin parking (session
      // unknown, or no replayer wired in a bare test).
      if (parkLiveEventDuringLoad(payload)) return;
    }
    useEventSeqStore.getState().record(sid, next);
  }

  const raw = payload.event as Record<string, unknown>;
  const event = parseServerEvent(raw);
  if (!event) return;

  // The orchestrator owns all cross-store sequencing (chat-store +
  // streaming-store + toolBlockIndex).
  applyEvent(sid, event, raw);
}

// Wire the replay half: history.ts drains its parked payloads back through
// the full gate+parse+apply. A registration rather than an import from
// history.ts, which would be a module cycle.
setLiveEventReplayer(ingestSessionEvent);
