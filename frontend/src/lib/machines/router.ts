import {
  getMachineClient,
  getPrimaryClient,
  machineClients,
  onMachineClientCreated,
} from "~/lib/machines/registry";
import { WsClient } from "~/lib/ws-client";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";
import { useMachineStore } from "~/stores/machine-store";

/**
 * Routing facade over the per-machine WebSocket clients (multi-machine M1).
 *
 * The entire codebase threads one `ws` handle through its RPC helpers
 * (~/lib/ws-rpc.ts) and subscription hooks. Instead of teaching every call
 * site about machines, this facade implements the WsClient surface and:
 *
 * - routes each REQUEST to the machine that owns the entity it names:
 *   payload.sessionId → the session's project's machine; payload.projectId →
 *   that project's machine; anything else → the primary. Entity ids are
 *   UUIDs, unique across machines, so the mapping is unambiguous.
 * - fans SUBSCRIPTIONS in from every machine's socket (including machines
 *   paired later): push payloads carry session/project ids, so the stores
 *   don't care which socket delivered them.
 * - delegates connection lifecycle (onConnect, connectionState, reconnect)
 *   to the PRIMARY client only. Remote lifecycle is per-machine, owned by
 *   useMachineConnections — a flaky remote must never trigger the primary's
 *   reconnect-and-refetch-everything path.
 *
 * Extends WsClient purely to satisfy the type at existing call sites; every
 * inherited behavior is overridden and the base instance never connects.
 */
class RoutingWsClient extends WsClient {
  private fanInHandlers = new Map<string, Set<(payload: unknown) => void>>();

  constructor() {
    super(""); // base never connects — connect() is overridden to a no-op
    onMachineClientCreated((_machineId, client) => {
      for (const [type, handlers] of this.fanInHandlers) {
        for (const handler of handlers) client.subscribe(type, handler);
      }
    });
  }

  /** Resolve which machine owns the entity a request payload names. */
  private targetFor(payload: unknown): WsClient {
    if (payload && typeof payload === "object") {
      const p = payload as { sessionId?: string; projectId?: string };
      let projectId = p.projectId;
      if (!projectId && p.sessionId) {
        projectId = useChatStore.getState().sessions[p.sessionId]?.meta.projectId;
      }
      if (projectId) {
        const project = useAppStore.getState().projects.find((pr) => pr.id === projectId);
        if (project?.machineId && useMachineStore.getState().machines[project.machineId]) {
          return getMachineClient(project.machineId);
        }
      }
    }
    return getPrimaryClient();
  }

  override request<T = unknown>(type: string, payload: unknown = {}, timeoutMs?: number) {
    return this.targetFor(payload).request<T>(type, payload, timeoutMs);
  }

  override subscribe(type: string, handler: (payload: unknown) => void): () => void {
    let handlers = this.fanInHandlers.get(type);
    if (!handlers) {
      handlers = new Set();
      this.fanInHandlers.set(type, handlers);
    }
    handlers.add(handler);

    const unsubs = [getPrimaryClient().subscribe(type, handler)];
    for (const client of machineClients().values()) {
      unsubs.push(client.subscribe(type, handler));
    }
    return () => {
      this.fanInHandlers.get(type)?.delete(handler);
      for (const unsub of unsubs) unsub();
    };
  }

  // Lifecycle delegates to the primary. Remote machines report through
  // machine-store, and their resubscribe-on-reconnect lives in
  // useMachineConnections.
  override get connectionState() {
    return getPrimaryClient().connectionState;
  }
  override onConnectionStateChange(fn: () => void): () => void {
    return getPrimaryClient().onConnectionStateChange(fn);
  }
  override onConnect(fn: () => void): () => void {
    return getPrimaryClient().onConnect(fn);
  }
  override onDisconnect(fn: () => void): () => void {
    return getPrimaryClient().onDisconnect(fn);
  }
  override connect(): void {
    getPrimaryClient().connect();
  }
  override disconnect(): void {
    getPrimaryClient().disconnect();
  }
  override forceReconnect(): void {
    getPrimaryClient().forceReconnect();
  }
}

let router: RoutingWsClient | null = null;

/** The app-wide routing client returned by useWebSocket(). */
export function getRoutingClient(): WsClient {
  if (!router) router = new RoutingWsClient();
  return router;
}
