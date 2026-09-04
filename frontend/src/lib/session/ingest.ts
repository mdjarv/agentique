import { parseServerEvent } from "~/lib/events";
import { loadSessionHistory } from "~/lib/session/history";
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

  // --- Wire-sequence gate (runs FIRST, before any store mutation) ---
  // seq 0 = unsequenced (e.g. a channel message to an offline session) —
  // skip ordering/dedup checks and apply directly. Otherwise drop
  // duplicates/out-of-order, and resync on a gap or pipeline rebuild.
  const seq = payload.seq ?? 0;
  if (seq > 0) {
    const prev = useEventSeqStore.getState().states[sid];
    const { action, next } = decideSeq(prev, payload.epoch ?? 0, seq);
    if (action === "drop") return;
    useEventSeqStore.getState().record(sid, next);
    if (action === "resync") {
      // Backfill missed events. Coalesced by loadSessionHistory's
      // historyLoading in-flight guard; the force-load reseeds the seq
      // state authoritatively from the response's high-water mark.
      loadSessionHistory(ws, sid, true);
    }
  }

  const raw = payload.event as Record<string, unknown>;
  const event = parseServerEvent(raw);
  if (!event) return;

  // The orchestrator owns all cross-store sequencing (chat-store +
  // streaming-store + toolBlockIndex).
  applyEvent(sid, event, raw);
}
