import { startTransition } from "react";
import { parseServerEvent } from "~/lib/events";
import type { HistoryResult, HistoryTurn } from "~/lib/generated-types";
// Type-only: the runtime dependency goes the other way (ingest.ts registers
// its replayer here), so this cannot become a module cycle.
import type { SessionEventPayload } from "~/lib/session/ingest";
import { uuid } from "~/lib/utils";
import type { WsClient } from "~/lib/ws-client";
import type { Turn } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import { useEventSeqStore } from "~/stores/event-seq";

const INITIAL_TURN_LIMIT = 20;

// --- Live events parked during a history load ---
//
// A history snapshot replaces a session's turns wholesale and reseeds the
// wire-seq tracker to the snapshot's high-water mark. A live event applied
// during the fetch's round trip was therefore wiped by the replacement and
// never redelivered: the tracker rewound to the snapshot's mark, so the
// wiped events read as already-seen duplicates when nothing redelivered
// them. If that window held the turn's result, the turn rendered
// permanently unfinished. So while a load is in flight NOTHING applies —
// the ingest path parks raw payloads here and the load's completion replays
// them through the full gate+parse+apply, where the gate drops what the
// snapshot already contains (seq <= highWaterSeq) and applies the rest.

/** Wired once by lib/session/ingest.ts, which owns gate+parse+apply. A
 *  registration seam rather than an import, to avoid the module cycle. */
let replayLiveEvent: ((ws: WsClient, payload: SessionEventPayload) => void) | undefined;

export function setLiveEventReplayer(
  fn: (ws: WsClient, payload: SessionEventPayload) => void,
): void {
  replayLiveEvent = fn;
}

const parkedLiveEvents = new Map<string, SessionEventPayload[]>();

/**
 * Parks a live `session.event` while a history load is in flight for its
 * session. Returns false when no load is parking (the caller applies the
 * event normally).
 */
export function parkLiveEventDuringLoad(payload: SessionEventPayload): boolean {
  const parked = parkedLiveEvents.get(payload.sessionId);
  if (!parked) return false;
  parked.push(payload);
  return true;
}

function beginParking(sessionId: string): void {
  // Without a registered replayer parking would swallow events for good;
  // apply-live-then-get-wiped is the lesser failure.
  if (!replayLiveEvent) return;
  if (!parkedLiveEvents.has(sessionId)) parkedLiveEvents.set(sessionId, []);
}

function drainParked(ws: WsClient, sessionId: string): void {
  const parked = parkedLiveEvents.get(sessionId);
  // Deleted BEFORE replaying: a replay must not park into the list being
  // drained. If a replayed gap starts another load, that load begins a fresh
  // list and the remaining payloads park there instead of jumping it.
  parkedLiveEvents.delete(sessionId);
  if (!parked || parked.length === 0 || !replayLiveEvent) return;
  if (!useChatStore.getState().sessions[sessionId]) return; // deleted mid-load
  for (const payload of parked) {
    replayLiveEvent(ws, payload);
  }
}

/** Number of history turns to process per batch before yielding. */
const CONVERT_BATCH_SIZE = 10;

/** Yield to the main thread so higher-priority work (input, paint) can run. */
function yieldToMain(): Promise<void> {
  if (
    "scheduler" in globalThis &&
    typeof (globalThis as Record<string, unknown>).scheduler === "object"
  ) {
    const s = (globalThis as unknown as { scheduler: { yield?: () => Promise<void> } }).scheduler;
    if (typeof s.yield === "function") return s.yield();
  }
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function convertTurn(ht: HistoryTurn): Turn {
  const events = (ht.events as Record<string, unknown>[])
    .map(parseServerEvent)
    .filter((e): e is NonNullable<typeof e> => e !== undefined);
  return {
    id: events[0]?.id ?? uuid(),
    prompt: ht.prompt,
    attachments: (ht.attachments ?? []).map((a) => ({ ...a, id: uuid() })),
    events,
    complete: ht.events.some((e) => (e as Record<string, unknown>).type === "result"),
    turnIndex: ht.turnIndex,
    origin: ht.origin,
  };
}

/** Convert history turns in batches, yielding between batches to keep the main thread responsive. */
async function historyToTurnsChunked(history: HistoryTurn[]): Promise<Turn[]> {
  const result: Turn[] = [];
  for (let i = 0; i < history.length; i += CONVERT_BATCH_SIZE) {
    if (i > 0) await yieldToMain();
    const batch = history.slice(i, i + CONVERT_BATCH_SIZE);
    for (const ht of batch) {
      result.push(convertTurn(ht));
    }
  }
  return result;
}

/** Convert turns synchronously (used for small partial loads where yielding adds unnecessary latency). */
function historyToTurns(history: HistoryTurn[]): Turn[] {
  return history.map(convertTurn);
}

const shortId = (id: string) => id.slice(0, 8);

async function fetchAndApplyFullHistory(
  ws: WsClient,
  sessionId: string,
  tag: string,
): Promise<void> {
  performance.mark(`${tag}:backfill:request`);
  const full = await ws.request<HistoryResult>("session.history", { sessionId }, 30_000);
  performance.mark(`${tag}:backfill:response`);
  performance.measure(
    `${tag} ws-roundtrip (full)`,
    `${tag}:backfill:request`,
    `${tag}:backfill:response`,
  );

  if (!useChatStore.getState().sessions[sessionId]) return;

  if (full.turns.length === 0) {
    // Still authoritative about sequencing: seed the tracker so parked live
    // events replay against this snapshot's high-water mark. Left unseeded,
    // every replay re-read its own seq as a gap and started another load.
    useEventSeqStore.getState().seedFromHistory(sessionId, full.epoch, full.highWaterSeq);
    useChatStore.getState().setHistoryLoading(sessionId, false);
    return;
  }

  performance.mark(`${tag}:convert:start`);
  const turns = await historyToTurnsChunked(full.turns);
  performance.mark(`${tag}:convert:end`);
  performance.measure(
    `${tag} convert (${full.turns.length} turns)`,
    `${tag}:convert:start`,
    `${tag}:convert:end`,
  );

  if (!useChatStore.getState().sessions[sessionId]) return;

  performance.mark(`${tag}:store:start`);
  startTransition(() => {
    useChatStore.getState().setSessionHistory(sessionId, turns, true);
  });
  // Authoritatively reseed the wire-seq tracker from this snapshot, so a live
  // event already contained in it (seq <= highWaterSeq) is dropped. Overwrites
  // any state a concurrent live event set mid-load — the snapshot wins.
  useEventSeqStore.getState().seedFromHistory(sessionId, full.epoch, full.highWaterSeq);
  performance.mark(`${tag}:store:end`);
  performance.measure(`${tag} store-update`, `${tag}:store:start`, `${tag}:store:end`);
}

export function loadSessionHistory(ws: WsClient, sessionId: string, force = false): void {
  const store = useChatStore.getState();
  const session = store.sessions[sessionId];
  if (!session) return;
  if (!force && session.historyComplete) return;
  if (store.historyLoading.has(sessionId)) return;

  const sid = shortId(sessionId);
  const tag = `history:${sid}`;
  performance.mark(`${tag}:request`);

  store.setHistoryLoading(sessionId, true);
  // From here to the drain, live events for this session park instead of
  // applying — see the block comment on parkedLiveEvents. The drain runs in
  // finally: after the seed on success, and on failure too, where the replay
  // simply meets the pre-load gate state (a genuine gap starts another load).
  beginParking(sessionId);

  // Force-reload of an already-complete history: skip the partial phase.
  // Truncating from N→20 just to refetch all N is destructive — causes DOM
  // churn and scroll-position jumps in long sessions. Go straight to full.
  if (force && session.historyComplete) {
    fetchAndApplyFullHistory(ws, sessionId, tag)
      .catch((err) => {
        useChatStore.getState().setHistoryLoading(sessionId, false);
        console.error("Failed to load session history:", err);
      })
      .finally(() => drainParked(ws, sessionId));
    return;
  }

  // Phase 1: fetch recent turns for instant display
  ws.request<HistoryResult>("session.history", { sessionId, limit: INITIAL_TURN_LIMIT }, 10_000)
    .then(async (hist) => {
      performance.mark(`${tag}:response`);
      performance.measure(`${tag} ws-roundtrip (partial)`, `${tag}:request`, `${tag}:response`);

      if (!useChatStore.getState().sessions[sessionId]) return;

      if (hist.turns.length > 0) {
        // Partial load is small — convert synchronously for minimum latency
        const turns = historyToTurns(hist.turns);
        // Set partial history — historyComplete stays false, historyLoading stays true
        useChatStore.getState().setSessionHistory(sessionId, turns, !hist.hasMore);
      }

      if (!hist.hasMore) {
        // No backfill phase — this partial IS the complete snapshot, so reseed
        // the wire-seq tracker from it (fetchAndApplyFullHistory won't run).
        useEventSeqStore.getState().seedFromHistory(sessionId, hist.epoch, hist.highWaterSeq);
        useChatStore.getState().setHistoryLoading(sessionId, false);
        return;
      }

      // Phase 2: fetch full history for backfill (chunked conversion + low-priority render)
      await fetchAndApplyFullHistory(ws, sessionId, tag);
    })
    .catch((err) => {
      useChatStore.getState().setHistoryLoading(sessionId, false);
      console.error("Failed to load session history:", err);
    })
    .finally(() => drainParked(ws, sessionId));
}
