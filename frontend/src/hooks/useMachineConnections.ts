import { useEffect, useRef } from "react";
import { subscribeAndLoad } from "~/hooks/useGlobalSubscriptions";
import {
  clearMachineCache,
  freezeMachineSessions,
  hydrateMachineCache,
  saveMachineCache,
} from "~/lib/machines/cache";
import { probeIdentity, probeMachine } from "~/lib/machines/health";
import {
  disconnectMachine,
  getMachineClient,
  machineFetch,
  peekMachineClient,
} from "~/lib/machines/registry";
import { remoteSlug } from "~/lib/machines/slug";
import type { Project } from "~/lib/types";
import type { WsClient } from "~/lib/ws-client";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";
import { useMachineStore } from "~/stores/machine-store";

/**
 * Drives connections to paired remote machines (multi-machine). For each
 * catalog entry: ensures its WebSocket client exists, and on every (re)connect
 * loads that machine's projects — tagged with machineId and a qualified slug —
 * then subscribes + loads sessions per project over that machine's own socket.
 *
 * Deliberately separate from useGlobalSubscriptions' primary reconnect path:
 * a flaky remote machine re-syncs only its own projects and never resets
 * primary streaming state.
 */

/** A machine that can never accept us is retried on a slow beat, not the 30s one. */
const FAULTED_RETRY_MS = 5 * 60_000;
const NORMAL_RETRY_MS = 30_000;
/** Let a blip settle before deciding a machine is broken rather than away. */
const PROBE_DELAY_MS = 3_000;

/**
 * Ask the machine's own descriptor why it isn't connecting, and record the
 * answer only when it proves something (see lib/machines/health.ts). An
 * unreachable machine yields nothing — away is the assumption.
 */
async function diagnose(machineId: string): Promise<void> {
  const entry = useMachineStore.getState().machines[machineId];
  if (!entry) return;
  const fault = await probeMachine(entry);
  // The machine may have reconnected while the probe was in flight; a live
  // socket outranks anything the probe concluded.
  if (useMachineStore.getState().statuses[machineId] === "connected") return;
  useMachineStore.getState().setFault(machineId, fault);

  const client = peekMachineClient(machineId);
  client?.setMaxReconnectDelay(
    fault?.kind === "credential-rejected" ? FAULTED_RETRY_MS : NORMAL_RETRY_MS,
  );
}

/** Re-syncs one machine's projects + sessions on demand (e.g. after creating
 *  a project on it from the primary UI). */
export function reloadMachineProjects(machineId: string): Promise<void> {
  return loadMachine(machineId, getMachineClient(machineId));
}

/**
 * Enforce the identity pin on connect (docs/multi-machine.md: "Clients pin
 * machineId and verify it on pair and connect").
 *
 * Pairing checks the descriptor, but nothing re-checked it afterwards — so an
 * address that changed hands (re-provisioned host, reused tailnet name) would
 * connect happily and its projects would be ingested under the old machine's
 * identity. Verified here, before a single project is trusted.
 *
 * Returns false when the machine is not who we paired with; the caller must
 * not load anything from it.
 */
async function verifyIdentity(machineId: string): Promise<boolean> {
  const entry = useMachineStore.getState().machines[machineId];
  if (!entry) return false;
  const fault = await probeIdentity(entry);
  if (!fault) return true;
  useMachineStore.getState().setFault(machineId, fault);
  return false;
}

/**
 * Forget everything a machine gave us. Used when a machine is removed, and
 * when one is rejected at the gate: its cached projects are how the routing
 * facade finds it, so leaving them behind lets any stray request revive the
 * very connection we just refused.
 */
function dropMachineData(machineId: string): void {
  const projectIds = new Set(
    useAppStore
      .getState()
      .projects.filter((p) => p.machineId === machineId)
      .map((p) => p.id),
  );
  const chat = useChatStore.getState();
  for (const [sessionId, data] of Object.entries(chat.sessions)) {
    if (projectIds.has(data.meta.projectId)) chat.removeSession(sessionId);
  }
  useAppStore.getState().removeMachineProjects(machineId);
  clearMachineCache(machineId);
}

/**
 * Everything that must be true before a machine's data is trusted. Both entry
 * points — the onConnect handler and the already-connected fast path — go
 * through here; a gate with a way around it is not a gate.
 */
function admit(machineId: string, client: WsClient, load: () => void): void {
  client.setMaxReconnectDelay(NORMAL_RETRY_MS);
  void verifyIdentity(machineId).then((ok) => {
    if (!ok) {
      disconnectMachine(machineId);
      dropMachineData(machineId);
      return;
    }
    // Admitted: whoever this is, it is who we paired with.
    useMachineStore.getState().setFault(machineId, null);
    load();
  });
}

async function loadMachine(machineId: string, client: WsClient): Promise<void> {
  const resp = await machineFetch(machineId, "/api/projects");
  if (!resp.ok) throw new Error(`projects fetch failed (${resp.status})`);
  const wire = (await resp.json()) as Project[];
  const projects = wire.map((p) => ({
    ...p,
    machineId,
    slug: remoteSlug(p.slug, machineId),
  }));
  useAppStore.getState().setMachineProjects(machineId, projects);
  for (const project of projects) {
    subscribeAndLoad(client, project.id, true);
  }
  // Refresh the offline cache once the live project list has landed. Session
  // metas trickle in via the per-project loads above; the disconnect-time
  // snapshot catches those, and this one guarantees the project set is fresh.
  saveMachineCache(machineId);
}

export function useMachineConnections(): void {
  const machines = useMachineStore((s) => s.machines);
  const teardownsRef = useRef(new Map<string, () => void>());

  // The catalog is account state mastered on the primary: reconcile once at
  // startup so a machine paired from another device (desktop vs phone PWA)
  // appears here too. The cached copy connects immediately; the server sync
  // then adds/removes entries and the effect below reacts.
  const syncedRef = useRef(false);
  useEffect(() => {
    if (syncedRef.current) return;
    syncedRef.current = true;
    useMachineStore.getState().syncFromServer();
  }, []);

  useEffect(() => {
    const teardowns = teardownsRef.current;

    for (const machineId of Object.keys(machines)) {
      if (teardowns.has(machineId)) continue;

      // Last-known projects + session metadata render immediately — a
      // suspended laptop's half of the sidebar stays visible and navigable
      // while its connection state shows why nothing is live. The live load
      // below is authoritative and replaces all of it.
      hydrateMachineCache(machineId);

      const client = getMachineClient(machineId);
      const load = () =>
        loadMachine(machineId, client).catch((err) =>
          console.error(`machine ${machineId} load failed`, err),
        );

      // Identity first: a machine that isn't who we paired with never gets to
      // hand us projects, however willingly its socket opened.
      const unsub = client.onConnect(() => admit(machineId, client, load));
      // Snapshot at disconnect — the freshest state this machine will have
      // until it comes back. Freeze first, so the live store and the snapshot
      // tell the same story: away, settled, nothing pending.
      let probeTimer: ReturnType<typeof setTimeout> | null = null;
      const unsubDisconnect = client.onDisconnect(() => {
        freezeMachineSessions(machineId);
        saveMachineCache(machineId);
        // Diagnose after a beat: a two-second blip is away, not a fault.
        if (probeTimer) clearTimeout(probeTimer);
        probeTimer = setTimeout(() => {
          probeTimer = null;
          void diagnose(machineId);
        }, PROBE_DELAY_MS);
      });
      // getMachineClient connects asynchronously (ticket mint), so attaching
      // onConnect here normally races nothing — but if the socket is somehow
      // already up (re-add after remove), admit it now. Through the same gate:
      // this path once skipped the identity check entirely.
      if (client.connectionState === "connected") admit(machineId, client, load);

      teardowns.set(machineId, () => {
        unsub();
        unsubDisconnect();
        if (probeTimer) clearTimeout(probeTimer);
      });
    }

    for (const [machineId, teardown] of teardowns) {
      if (machines[machineId]) continue;
      teardown();
      disconnectMachine(machineId);
      // Sessions go before projects — the project list is how we know which
      // sessions were its (sessions carry no machine tag).
      dropMachineData(machineId);
      teardowns.delete(machineId);
    }
  }, [machines]);
}
