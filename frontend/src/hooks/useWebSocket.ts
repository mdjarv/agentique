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

export function useWebSocket(): WsClient {
	const clientRef = useRef(getClient());
	return clientRef.current;
}
