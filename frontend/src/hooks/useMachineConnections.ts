import { useEffect, useRef } from "react";
import { subscribeAndLoad } from "~/hooks/useGlobalSubscriptions";
import {
  clearMachineCache,
  freezeMachineSessions,
  hydrateMachineCache,
  saveMachineCache,
} from "~/lib/machines/cache";
import { disconnectMachine, getMachineClient, machineFetch } from "~/lib/machines/registry";
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

/** Re-syncs one machine's projects + sessions on demand (e.g. after creating
 *  a project on it from the primary UI). */
export function reloadMachineProjects(machineId: string): Promise<void> {
  return loadMachine(machineId, getMachineClient(machineId));
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

      const unsub = client.onConnect(load);
      // Snapshot at disconnect — the freshest state this machine will have
      // until it comes back. Freeze first, so the live store and the snapshot
      // tell the same story: away, settled, nothing pending.
      const unsubDisconnect = client.onDisconnect(() => {
        freezeMachineSessions(machineId);
        saveMachineCache(machineId);
      });
      // getMachineClient connects asynchronously (ticket mint), so attaching
      // onConnect here normally races nothing — but if the socket is somehow
      // already up (re-add after remove), load immediately.
      if (client.connectionState === "connected") load();

      teardowns.set(machineId, () => {
        unsub();
        unsubDisconnect();
      });
    }

    for (const [machineId, teardown] of teardowns) {
      if (machines[machineId]) continue;
      teardown();
      disconnectMachine(machineId);

      // Drop the machine's sessions before its projects — the project list is
      // how we know which sessions were its (sessions carry no machine tag).
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
      teardowns.delete(machineId);
    }
  }, [machines]);
}
