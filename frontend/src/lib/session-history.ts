import { parseServerEvent } from "~/lib/events";
import { uuid } from "~/lib/utils";
import type { WsClient } from "~/lib/ws-client";
import type { Attachment, Turn } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";

interface HistoryTurn {
  prompt: string;
  attachments?: Attachment[];
  events: Record<string, unknown>[];
}

interface SessionHistoryResult {
  turns: HistoryTurn[];
}

function historyToTurns(history: HistoryTurn[]): Turn[] {
  return history.map((ht) => ({
    id: uuid(),
    prompt: ht.prompt,
    attachments: ht.attachments ?? [],
    events: ht.events.map(parseServerEvent),
    complete: ht.events.some((e) => e.type === "result"),
  }));
}

export function loadSessionHistory(ws: WsClient, sessionId: string): void {
  const store = useChatStore.getState();
  const session = store.sessions[sessionId];
  if (!session || session.turns.length > 0) return;
  if (store.historyLoading.has(sessionId)) return;

  store.setHistoryLoading(sessionId, true);
  ws.request<SessionHistoryResult>("session.history", { sessionId })
    .then((hist) => {
      if (hist.turns.length > 0) {
        useChatStore.getState().setSessionHistory(sessionId, historyToTurns(hist.turns));
      } else {
        useChatStore.getState().setHistoryLoading(sessionId, false);
      }
    })
    .catch((err) => {
      useChatStore.getState().setHistoryLoading(sessionId, false);
      console.error("Failed to load session history:", err);
    });
}
