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
 * Declares a typed WS RPC bound to its wire `type` and (optional) timeout, and
 * returns a caller. The timeout is attached once at the definition site rather
 * than repeated at each call.
 */
export function define<TResult = void, TParams = undefined>(
  type: string,
  timeoutMs?: number,
): RpcCaller<TResult, TParams> {
  return ((ws: WsClient, params?: TParams) =>
    ws.request<TResult>(type, params ?? {}, timeoutMs)) as RpcCaller<TResult, TParams>;
}
