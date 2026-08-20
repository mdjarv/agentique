import { useEffect, useRef } from "react";
import { subscribeAndLoad } from "~/hooks/useGlobalSubscriptions";
import { disconnectMachine, getMachineClient, machineFetch } from "~/lib/machines/registry";
import { remoteSlug } from "~/lib/machines/slug";
import type { Project } from "~/lib/types";
import type { WsClient } from "~/lib/ws-client";
import { useAppStore } from "~/stores/app-store";
import { useMachineStore } from "~/stores/machine-store";

/**
 * Drives connections to paired remote machines (multi-machine M1). For each
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
}

export function useMachineConnections(): void {
  const machines = useMachineStore((s) => s.machines);
  const teardownsRef = useRef(new Map<string, () => void>());

  useEffect(() => {
    const teardowns = teardownsRef.current;

    for (const machineId of Object.keys(machines)) {
      if (teardowns.has(machineId)) continue;

      const client = getMachineClient(machineId);
      const load = () =>
        loadMachine(machineId, client).catch((err) =>
          console.error(`machine ${machineId} load failed`, err),
        );

      const unsub = client.onConnect(load);
      // getMachineClient connects asynchronously (ticket mint), so attaching
      // onConnect here normally races nothing — but if the socket is somehow
      // already up (re-add after remove), load immediately.
      if (client.connectionState === "connected") load();

      teardowns.set(machineId, unsub);
    }

    for (const [machineId, teardown] of teardowns) {
      if (machines[machineId]) continue;
      teardown();
      disconnectMachine(machineId);
      useAppStore.getState().removeMachineProjects(machineId);
      teardowns.delete(machineId);
    }
  }, [machines]);
}
