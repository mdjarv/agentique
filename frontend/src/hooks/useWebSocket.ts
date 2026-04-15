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
  if (globalClient) {
    globalClient.disconnect();
    globalClient = null;
  }
  getClient();
}

export function useWebSocket(): WsClient {
  const clientRef = useRef(getClient());
  return clientRef.current;
}
