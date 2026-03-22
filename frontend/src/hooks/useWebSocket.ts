import { useRef } from "react";
import { WsClient } from "~/lib/ws-client";

let globalClient: WsClient | null = null;

function getClient(): WsClient {
  if (!globalClient) {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    // In dev mode (Vite on :5173), connect directly to the Go backend on :8080.
    // In production, the Go server serves both the frontend and WebSocket.
    const host =
      window.location.port === "5173" ? `${window.location.hostname}:8080` : window.location.host;
    const url = `${protocol}//${host}/ws`;
    globalClient = new WsClient(url);
    globalClient.connect();
  }
  return globalClient;
}

export function useWebSocket(): WsClient {
  const clientRef = useRef(getClient());
  return clientRef.current;
}
