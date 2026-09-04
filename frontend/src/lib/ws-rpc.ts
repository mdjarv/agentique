import { isUnknownOpError, LEGACY_OP } from "~/lib/wire-compat";
import type { WsClient } from "~/lib/ws-client";

/**
 * Timeout buckets (ms) for WS RPCs. Centralizes the formerly-inline, inconsistent
 * per-call timeouts. Omitting a timeout from `define` falls through to
 * `WsClient.request`'s 30s default — it is not duplicated here.
 */
export const QUICK = 10_000; // status polls expected to return promptly
export const MEDIUM = 60_000; // single AI generation
export const LONG = 120_000; // git mutations, session lifecycle, multi-step AI

/** A typed WS RPC caller. Takes no `params` argument when the RPC has no payload. */
export type RpcCaller<TResult, TParams> = [TParams] extends [undefined]
  ? (ws: WsClient) => Promise<TResult>
  : (ws: WsClient, params: TParams) => Promise<TResult>;

/**
 * Which ops a given connection has told us it does not understand.
 *
 * A client holds one connection per paired machine, and only some of them may
 * predate a rename — so this is keyed by connection, not global. The handle
 * call sites hold is the routing facade, so `define` resolves the actual
 * per-machine client first and keys on that. Keyed weakly so a machine going
 * away takes its entry with it, and cleared on the connection's next connect,
 * which is exactly when a peer may have been upgraded underneath us.
 */
const legacyPeers = new WeakMap<WsClient, Set<string>>();

/** Connections whose forget-on-reconnect hook is installed (once each). */
const reconnectHooked = new WeakSet<WsClient>();

function prefersLegacy(ws: WsClient, type: string): boolean {
  return legacyPeers.get(ws)?.has(type) ?? false;
}

function rememberLegacy(ws: WsClient, type: string): void {
  const known = legacyPeers.get(ws);
  if (known) known.add(type);
  else legacyPeers.set(ws, new Set([type]));
  if (!reconnectHooked.has(ws)) {
    reconnectHooked.add(ws);
    ws.onConnect(() => legacyPeers.delete(ws));
  }
}

/**
 * Declares a typed WS RPC bound to its wire `type` and (optional) timeout, and
 * returns a caller. The timeout is attached once at the definition site rather
 * than repeated at each call.
 *
 * When the op has been renamed, a peer still running the older release rejects
 * the new name; the call then retries under the old one (see `wire-compat`) and
 * the connection is remembered, so the wasted round-trip happens once per
 * socket rather than once per click.
 */
export function define<TResult = void, TParams = undefined>(
  type: string,
  timeoutMs?: number,
): RpcCaller<TResult, TParams> {
  const legacyType = LEGACY_OP[type];

  return ((ws: WsClient, params?: TParams) => {
    const payload = params ?? {};
    if (!legacyType) return ws.request<TResult>(type, payload, timeoutMs);

    // The peer that can be behind a rename is the per-machine connection, not
    // the routing facade every call site holds — resolve it first, and key the
    // legacy memory on it.
    const peer = ws.resolveClient(payload);
    if (prefersLegacy(peer, type)) return peer.request<TResult>(legacyType, payload, timeoutMs);

    return peer.request<TResult>(type, payload, timeoutMs).catch((err: unknown) => {
      if (!isUnknownOpError(err)) throw err;
      rememberLegacy(peer, type);
      return peer.request<TResult>(legacyType, payload, timeoutMs);
    });
  }) as RpcCaller<TResult, TParams>;
}
