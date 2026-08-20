import { WsClient } from "~/lib/ws-client";
import { useMachineStore } from "~/stores/machine-store";

/**
 * Per-machine WebSocket clients + authenticated REST for remote machines
 * (multi-machine). One WsClient per machine, created once and reconnected
 * in place, never replaced — components hold clients via useRef, so a swapped
 * instance would strand them on a dead socket (same constraint as the primary
 * client in useWebSocket.ts).
 *
 * Remote sockets authenticate with a one-time short-lived ticket minted per
 * connect attempt (the long-lived bearer never appears in a URL). The primary
 * machine's client stays cookie-authenticated and same-origin.
 */

const clients = new Map<string, WsClient>();

let primaryClient: WsClient | null = null;

/** Listeners fired when a machine client is created (router replays subscriptions). */
const clientCreatedListeners = new Set<(machineId: string, client: WsClient) => void>();

export function onMachineClientCreated(fn: (machineId: string, client: WsClient) => void): void {
  clientCreatedListeners.add(fn);
}

/** The same-origin, cookie-authenticated client for the machine serving this SPA. */
export function getPrimaryClient(): WsClient {
  if (!primaryClient) {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    primaryClient = new WsClient(`${protocol}//${window.location.host}/ws`);
    primaryClient.connect();
  }
  return primaryClient;
}

function machineEntry(machineId: string) {
  const entry = useMachineStore.getState().machines[machineId];
  if (!entry) throw new Error(`unknown machine ${machineId}`);
  return entry;
}

/** Authenticated fetch against a remote machine's REST API. */
export async function machineFetch(
  machineId: string,
  path: string,
  init: RequestInit = {},
): Promise<Response> {
  const entry = machineEntry(machineId);
  const headers = new Headers(init.headers);
  // Token-less entries are auth-disabled machines — no credential to send.
  if (entry.token) headers.set("Authorization", `Bearer ${entry.token}`);
  return fetch(entry.baseUrl + path, { ...init, headers });
}

/** Mints a one-time WebSocket ticket and builds the wss URL for one attempt.
 *  Auth-disabled machines connect without a ticket. */
async function resolveTicketUrl(machineId: string): Promise<string> {
  const entry = machineEntry(machineId);
  const base = new URL(entry.baseUrl);
  const protocol = base.protocol === "https:" ? "wss:" : "ws:";
  if (!entry.token) return `${protocol}//${base.host}/ws`;

  const resp = await machineFetch(machineId, "/api/auth/ws-ticket", { method: "POST" });
  if (!resp.ok) throw new Error(`ws-ticket mint failed (${resp.status})`);
  const { ticket } = (await resp.json()) as { ticket: string };
  return `${protocol}//${base.host}/ws?wsTicket=${encodeURIComponent(ticket)}`;
}

/**
 * Returns the machine's client, creating and connecting it on first use.
 * Status changes stream into machine-store for the per-machine UI dots.
 */
export function getMachineClient(machineId: string): WsClient {
  const client = clients.get(machineId);
  if (client) return client;

  const created = new WsClient(() => resolveTicketUrl(machineId));
  clients.set(machineId, created);

  const setStatus = () => useMachineStore.getState().setStatus(machineId, created.connectionState);
  created.onConnectionStateChange(setStatus);
  setStatus();

  for (const fn of clientCreatedListeners) fn(machineId, created);
  created.connect();
  return created;
}

/** The machine's client if it exists, without creating one. */
export function peekMachineClient(machineId: string): WsClient | undefined {
  return clients.get(machineId);
}

/** All live machine clients (not the primary). */
export function machineClients(): ReadonlyMap<string, WsClient> {
  return clients;
}

/** Tears down a removed machine's connection. The instance stays out of the
 * registry; a re-add creates a fresh client. */
export function disconnectMachine(machineId: string): void {
  const client = clients.get(machineId);
  if (!client) return;
  clients.delete(machineId);
  client.disconnect();
}
