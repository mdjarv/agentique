/**
 * A live effort change, sent one at a time per session.
 *
 * The ramp is a slider, so one drag names every stop it crosses, and each
 * `session.set-effort` is a control round trip to the CLI whose next request
 * then re-writes the prompt cache. So the level moves in the store at once —
 * the thumb has to follow the pointer — while the wire carries at most one
 * request per session: whatever is chosen while one is out replaces the
 * pending level, and goes when that one answers.
 *
 * The server broadcasts `session.effort-changed` for every request, this tab's
 * included, and those pushes describe levels the user has already dragged
 * past. While a change is outstanding they are ignored (`isEffortChangeOutstanding`)
 * and the settle writes the level last sent, so the thumb never steps back
 * through the drag. A failure puts back the last level the server confirmed.
 */
import { setSessionEffort } from "~/lib/session/actions";
import type { WsClient } from "~/lib/ws-client";
import { useChatStore } from "~/stores/chat-store";

export interface EffortChangeReport {
  /** The server refused; the store is back at the last confirmed level. */
  failed: (err: unknown) => void;
  /** The provider took a different level than the one requested. */
  adjusted: (requested: string, applied: string) => void;
}

interface Outstanding {
  /** The level the server last confirmed — what a failure restores. */
  confirmed: string;
  /** Chosen while a request was out; sent when it answers. */
  pending: string | null;
}

const outstanding = new Map<string, Outstanding>();

/** Whether this tab has an effort change for the session not yet answered. */
export function isEffortChangeOutstanding(sessionId: string): boolean {
  return outstanding.has(sessionId);
}

export function changeSessionEffort(
  ws: WsClient,
  sessionId: string,
  level: string,
  report: EffortChangeReport,
): void {
  const store = useChatStore.getState();
  const inFlight = outstanding.get(sessionId);
  if (inFlight) {
    inFlight.pending = level;
    store.setSessionEffort(sessionId, level);
    return;
  }
  const confirmed = store.sessions[sessionId]?.meta.effort ?? "";
  outstanding.set(sessionId, { confirmed, pending: null });
  store.setSessionEffort(sessionId, level);
  void send(ws, sessionId, level, report);
}

async function send(
  ws: WsClient,
  sessionId: string,
  level: string,
  report: EffortChangeReport,
): Promise<void> {
  const entry = outstanding.get(sessionId);
  if (!entry) return;

  let applied: string;
  try {
    applied = await setSessionEffort(ws, sessionId, level);
  } catch (err) {
    outstanding.delete(sessionId);
    useChatStore.getState().setSessionEffort(sessionId, entry.confirmed);
    report.failed(err);
    return;
  }

  entry.confirmed = level;
  const next = entry.pending;
  entry.pending = null;
  if (next !== null && next !== level) {
    await send(ws, sessionId, next, report);
    return;
  }

  outstanding.delete(sessionId);
  useChatStore.getState().setSessionEffort(sessionId, level);
  // A reset answers with the model's own default, which is not an adjustment,
  // and "" is a model that takes no effort at all, which is not one either.
  if (level !== "" && applied !== "" && applied !== level) report.adjusted(level, applied);
}
