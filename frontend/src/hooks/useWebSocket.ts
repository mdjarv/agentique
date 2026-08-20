import { useRef } from "react";
import { getPrimaryClient } from "~/lib/machines/registry";
import { getRoutingClient } from "~/lib/machines/router";
import type { WsClient } from "~/lib/ws-client";

/** Force reconnect the primary WS client (e.g. after auth changes). */
export function reconnectWebSocket(): void {
  // Reconnect in place instead of disconnecting + replacing the object, so any
  // component already holding the client via useRef keeps a valid, live
  // reference (useRef never observes a swapped-out instance — it would keep
  // subscribing/requesting on a permanently-dead socket). forceReconnect drops
  // the current socket and re-establishes with the latest auth.
  getPrimaryClient().forceReconnect();
}

/**
 * The app-wide WS handle. Since multi-machine M1 this is a routing facade:
 * requests dispatch to the machine that owns the entity in the payload
 * (session/project id), subscriptions fan in from every paired machine, and
 * connection lifecycle reflects the primary machine. See
 * ~/lib/machines/router.ts.
 */
export function useWebSocket(): WsClient {
  const clientRef = useRef(getRoutingClient());
  return clientRef.current;
}
