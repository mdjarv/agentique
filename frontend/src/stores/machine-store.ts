import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { ConnectionState } from "~/lib/ws-client";

/**
 * Catalog of paired remote machines (multi-machine,
 * docs/multi-machine-research.md M1). Each entry is one remote agentique
 * server reached by base URL + bearer token (minted via `agentique pair` on
 * that machine). The primary machine — the one serving this SPA — is implicit
 * and never in this catalog; its identity lives in feature-store.
 *
 * Entries persist to localStorage; per-machine connection status is volatile.
 */
export interface MachineEntry {
  machineId: string;
  label: string;
  /** Origin the machine is reached on, no trailing slash (e.g. "https://zbook.tail1234.ts.net:19201"). */
  baseUrl: string;
  /** Long-lived bearer session token from the pairing exchange. */
  token: string;
  addedAt: string;
}

export type MachineStatus = ConnectionState;

interface MachineState {
  machines: Record<string, MachineEntry>;
  statuses: Record<string, MachineStatus>;

  addMachine: (entry: MachineEntry) => void;
  removeMachine: (machineId: string) => void;
  setStatus: (machineId: string, status: MachineStatus) => void;
}

export const useMachineStore = create<MachineState>()(
  persist(
    (set) => ({
      machines: {},
      statuses: {},

      addMachine: (entry) =>
        set((s) => ({ machines: { ...s.machines, [entry.machineId]: entry } })),
      removeMachine: (machineId) =>
        set((s) => {
          const machines = { ...s.machines };
          delete machines[machineId];
          const statuses = { ...s.statuses };
          delete statuses[machineId];
          return { machines, statuses };
        }),
      setStatus: (machineId, status) =>
        set((s) =>
          s.statuses[machineId] === status
            ? s
            : { statuses: { ...s.statuses, [machineId]: status } },
        ),
    }),
    {
      name: "agentique:machines",
      // Connection status is per-tab runtime state, never persisted.
      partialize: (s) => ({ machines: s.machines }),
    },
  ),
);
