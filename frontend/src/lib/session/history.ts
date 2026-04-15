import { startTransition } from "react";
import { parseServerEvent } from "~/lib/events";
import type { HistoryResult, HistoryTurn } from "~/lib/generated-types";
import { uuid } from "~/lib/utils";
import type { WsClient } from "~/lib/ws-client";
import type { Turn } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";

const INITIAL_TURN_LIMIT = 20;

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

  // Phase 1: fetch recent turns for instant display
  ws.request<HistoryResult>("session.history", { sessionId, limit: INITIAL_TURN_LIMIT }, 10_000)
    .then((hist) => {
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
        useChatStore.getState().setHistoryLoading(sessionId, false);
        return;
      }

      // Phase 2: fetch full history for backfill (chunked conversion + low-priority render)
      performance.mark(`${tag}:backfill:request`);
      return ws
        .request<HistoryResult>("session.history", { sessionId }, 30_000)
        .then(async (full) => {
          performance.mark(`${tag}:backfill:response`);
          performance.measure(
            `${tag} ws-roundtrip (full)`,
            `${tag}:backfill:request`,
            `${tag}:backfill:response`,
          );

          if (!useChatStore.getState().sessions[sessionId]) return;

          if (full.turns.length > 0) {
            performance.mark(`${tag}:convert:start`);
            const turns = await historyToTurnsChunked(full.turns);
            performance.mark(`${tag}:convert:end`);
            performance.measure(
              `${tag} convert (${full.turns.length} turns)`,
              `${tag}:convert:start`,
              `${tag}:convert:end`,
            );

            // Re-check session still exists after async conversion
            if (!useChatStore.getState().sessions[sessionId]) return;

            performance.mark(`${tag}:store:start`);
            startTransition(() => {
              useChatStore.getState().setSessionHistory(sessionId, turns, true);
            });
            performance.mark(`${tag}:store:end`);
            performance.measure(`${tag} store-update`, `${tag}:store:start`, `${tag}:store:end`);
          } else {
            useChatStore.getState().setHistoryLoading(sessionId, false);
          }
        });
    })
    .catch((err) => {
      useChatStore.getState().setHistoryLoading(sessionId, false);
      console.error("Failed to load session history:", err);
    });
}
