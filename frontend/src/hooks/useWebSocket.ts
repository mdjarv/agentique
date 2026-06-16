import { useRef } from "react";
import { WsClient } from "~/lib/ws-client";

let globalClient: WsClient | null = null;

function getClient(): WsClient {
  if (!globalClient) {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${protocol}//${window.location.host}/ws`;
    globalClient = new WsClient(url);
    globalClient.connect();
  }
  return globalClient;
}

/** Force reconnect the global WS client (e.g. after auth changes). */
export function reconnectWebSocket(): void {
  // Reconnect in place instead of disconnecting + replacing the object, so any
  // component already holding the client via useRef keeps a valid, live
  // reference (useRef never observes a swapped-out instance — it would keep
  // subscribing/requesting on a permanently-dead socket). forceReconnect drops
  // the current socket and re-establishes with the latest auth.
  getClient().forceReconnect();
}

export function useWebSocket(): WsClient {
  const clientRef = useRef(getClient());
  return clientRef.current;
}
