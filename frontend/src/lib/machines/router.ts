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
 * Routing facade over the per-machine WebSocket clients (multi-machine).
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
/** One live fan-in subscription: its type, its handler, and the unsubscriber
 *  for every client it has been attached to — including clients created after
 *  subscribe time, which the replay below keeps appending to. */
interface FanInSubscription {
  type: string;
  handler: (payload: unknown) => void;
  unsubs: Array<() => void>;
}

class RoutingWsClient extends WsClient {
  private fanInSubs = new Set<FanInSubscription>();

  constructor() {
    super(""); // base never connects — connect() is overridden to a no-op
    onMachineClientCreated((_machineId, client) => {
      // Track the replayed subscription on its own record, so unsubscribing
      // detaches it from clients created after subscribe time too. Captured in
      // a closure-local array, handlers replayed here leaked onto every
      // later-paired machine for the tab's lifetime, stacking per remount.
      for (const sub of this.fanInSubs) {
        sub.unsubs.push(client.subscribe(sub.type, sub.handler));
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

  /** The per-machine connection this payload routes to. ws-rpc resolves
   *  through here so its per-connection legacy-op memory keys on the real
   *  peer: keyed on this facade, one pre-rename machine would flip every
   *  machine — the primary included — onto the legacy op name. */
  override resolveClient(payload: unknown): WsClient {
    return this.targetFor(payload);
  }

  override request<T = unknown>(type: string, payload: unknown = {}, timeoutMs?: number) {
    return this.targetFor(payload).request<T>(type, payload, timeoutMs);
  }

  override subscribe(type: string, handler: (payload: unknown) => void): () => void {
    const sub: FanInSubscription = {
      type,
      handler,
      unsubs: [getPrimaryClient().subscribe(type, handler)],
    };
    for (const client of machineClients().values()) {
      sub.unsubs.push(client.subscribe(type, handler));
    }
    this.fanInSubs.add(sub);

    return () => {
      // Idempotent: a second call must not re-run unsubscribers that a
      // later-created client's replay may have appended after the first.
      if (!this.fanInSubs.delete(sub)) return;
      for (const unsub of sub.unsubs) unsub();
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
