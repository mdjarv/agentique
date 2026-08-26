/**
 * Telling the owning machine that a finished turn has now been looked at.
 *
 * The unseen-completion mark is server state, so clearing it is a message, not
 * a local edit — that is what makes opening a session on the phone clear the
 * badge on the laptop. The request routes by `sessionId`, so a session on a
 * paired machine is marked seen on the machine that holds it.
 *
 * Best effort by construction: a peer that predates the op rejects it, and the
 * rejection is swallowed rather than retried. The local clear has already
 * happened, and a machine that cannot remember the mark simply keeps the
 * behaviour it had before the field existed.
 */
import { getRoutingClient } from "~/lib/machines/router";
import { define, QUICK } from "~/lib/ws-rpc";

const markSeenRpc = define<void, { sessionId: string }>("session.markSeen", QUICK);

export function markSessionSeen(sessionId: string): void {
  if (!sessionId) return;
  const ws = getRoutingClient();
  // Nothing to send it down. A request would sit waiting on a socket that is
  // not there, and the reconnect refetches the session list anyway — which
  // carries the mark, so the reconciliation happens without this.
  if (ws.connectionState !== "connected") return;
  markSeenRpc(ws, { sessionId }).catch(() => {});
}
