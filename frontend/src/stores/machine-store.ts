import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { ConnectionState } from "~/lib/ws-client";

/**
 * Catalog of paired remote machines (multi-machine,
 * docs/multi-machine.md). Each entry is one remote agentique
 * server reached by base URL + bearer token (minted via `agentique pair` on
 * that machine). The primary machine — the one serving this SPA — is implicit
 * and never in this catalog; its identity lives in feature-store.
 *
 * The catalog is ACCOUNT state, mastered on the primary server
 * (/api/machines): a machine paired from the desktop must appear on the
 * phone's PWA. localStorage persistence is only the offline cache;
 * syncFromServer reconciles it (server wins) once the primary is reachable,
 * and add/remove write through to the server.
 */
export interface MachineEntry {
  machineId: string;
  label: string;
  /** Origin the machine is reached on, no trailing slash (e.g. "https://zbook.tail1234.ts.net:19201"). */
  baseUrl: string;
  /** Long-lived bearer session token from the pairing exchange. */
  token: string;
  addedAt: string;
  /** Icon id (lucide) — this host's presentation of that machine, never the
   *  machine's own opinion. Empty falls back to the generic server glyph. */
  icon?: string;
}

export type MachineStatus = ConnectionState;

interface MachineState {
  machines: Record<string, MachineEntry>;
  statuses: Record<string, MachineStatus>;

  addMachine: (entry: MachineEntry) => void;
  /** Rename / re-face a paired machine. Presentation is local to this host:
   *  nothing is written to the machine itself (docs/multi-machine.md). */
  renameMachine: (machineId: string, patch: { label?: string; icon?: string }) => Promise<void>;
  removeMachine: (machineId: string) => void;
  setStatus: (machineId: string, status: MachineStatus) => void;
  /** Reconcile the catalog from the primary's server-side copy (server
   *  wins). A failed fetch keeps the local cache — offline still works. */
  syncFromServer: () => Promise<void>;
}

export const useMachineStore = create<MachineState>()(
  persist(
    (set, get) => ({
      machines: {},
      statuses: {},

      addMachine: (entry) => {
        set((s) => ({ machines: { ...s.machines, [entry.machineId]: entry } }));
        fetch(`/api/machines/${encodeURIComponent(entry.machineId)}`, {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            label: entry.label,
            baseUrl: entry.baseUrl,
            token: entry.token,
            addedAt: entry.addedAt,
            icon: entry.icon ?? "",
          }),
        }).catch((err) => console.error("machine catalog save failed", err));
      },
      renameMachine: async (machineId, patch) => {
        const current = get().machines[machineId];
        if (!current) return;
        const next: MachineEntry = { ...current, ...patch };
        set((s) => ({ machines: { ...s.machines, [machineId]: next } }));
        const res = await fetch(`/api/machines/${encodeURIComponent(machineId)}`, {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            label: next.label,
            baseUrl: next.baseUrl,
            token: next.token,
            addedAt: next.addedAt,
            icon: next.icon ?? "",
          }),
        });
        if (!res.ok) {
          // Put the old presentation back rather than leaving the UI claiming
          // a name the catalog never accepted.
          set((s) => ({ machines: { ...s.machines, [machineId]: current } }));
          throw new Error(`rename failed (${res.status})`);
        }
      },
      removeMachine: (machineId) => {
        set((s) => {
          const machines = { ...s.machines };
          delete machines[machineId];
          const statuses = { ...s.statuses };
          delete statuses[machineId];
          return { machines, statuses };
        });
        fetch(`/api/machines/${encodeURIComponent(machineId)}`, { method: "DELETE" }).catch((err) =>
          console.error("machine catalog delete failed", err),
        );
      },
      setStatus: (machineId, status) =>
        set((s) =>
          s.statuses[machineId] === status
            ? s
            : { statuses: { ...s.statuses, [machineId]: status } },
        ),
      syncFromServer: async () => {
        let entries: MachineEntry[];
        try {
          const res = await fetch("/api/machines");
          if (!res.ok) return;
          entries = (await res.json()) as MachineEntry[];
        } catch {
          return;
        }
        set(() => {
          const machines: Record<string, MachineEntry> = {};
          for (const e of entries) {
            if (e.machineId && e.baseUrl) machines[e.machineId] = e;
          }
          return { machines };
        });
      },
    }),
    {
      name: "agentique:machines",
      // Connection status is per-tab runtime state, never persisted.
      partialize: (s) => ({ machines: s.machines }),
    },
  ),
);
