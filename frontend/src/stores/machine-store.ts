import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { MachineFault } from "~/lib/machines/health";
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
  /** Public id of the remote auth session, used for rotation and revocation. */
  sessionId: string;
  /** Base64url P-256 SPKI public key pinned during pairing. */
  identityKey: string;
  addedAt: string;
  /** Icon id (lucide) — this host's presentation of that machine, never the
   *  machine's own opinion. Empty falls back to the generic server glyph. */
  icon?: string;
  /** The machine's own OS (GOOS: "linux" | "windows" | "darwin"), captured
   *  from its pairing descriptor and refreshed on connect. A fact, not
   *  presentation — never user-editable. Absent means unknown (an older row),
   *  which draws no platform mark. */
  platformOs?: string;
}

export type MachineStatus = ConnectionState;

interface MachineState {
  machines: Record<string, MachineEntry>;
  statuses: Record<string, MachineStatus>;
  /** machineId → epoch ms this machine was last connected. Machines suspend
   *  and wake constantly; "last seen 3h ago" is the honest way to say a
   *  machine is away without treating it as a failure. */
  lastSeenAt: Record<string, number>;
  /**
   * machineId → a *proven* fault (wrong machine, rejected credential, not an
   * agentique server). Absent means away, which is ordinary and silent — only
   * something that can never fix itself earns a place here.
   */
  faults: Record<string, MachineFault>;
  /** machineId → the build version its descriptor last reported. Persisted:
   *  an offline machine still shows what it was running when it left
   *  (docs/upgrades.md), greyed and with no action offered. */
  versions: Record<string, string>;

  addMachine: (entry: MachineEntry) => Promise<void>;
  /** Rename / re-face a paired machine. Presentation is local to this host:
   *  nothing is written to the machine itself (docs/multi-machine.md). */
  renameMachine: (machineId: string, patch: { label?: string; icon?: string }) => Promise<void>;
  removeMachine: (machineId: string) => Promise<void>;
  setStatus: (machineId: string, status: MachineStatus) => void;
  /** Record or clear a proven fault for a machine. */
  setFault: (machineId: string, fault: MachineFault | null) => void;
  /** Record the version a machine's descriptor reported. */
  setVersion: (machineId: string, version: string) => void;
  /** Record the OS a machine's descriptor reported. Self-heals rows paired
   *  before the catalog stored it; an empty answer never erases a known one. */
  setPlatform: (machineId: string, platformOs: string) => void;
  /** Reconcile the catalog from the primary's server-side copy (server
   *  wins). A failed fetch keeps the local cache — offline still works. */
  syncFromServer: () => Promise<void>;
}

export const useMachineStore = create<MachineState>()(
  persist(
    (set, get) => ({
      machines: {},
      statuses: {},
      lastSeenAt: {},
      faults: {},
      versions: {},

      addMachine: async (entry) => {
        const res = await fetch(`/api/machines/${encodeURIComponent(entry.machineId)}`, {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            label: entry.label,
            baseUrl: entry.baseUrl,
            token: entry.token,
            sessionId: entry.sessionId,
            identityKey: entry.identityKey,
            addedAt: entry.addedAt,
            icon: entry.icon ?? "",
            platformOs: entry.platformOs ?? "",
          }),
        });
        if (!res.ok) throw new Error(`machine catalog save failed (${res.status})`);
        set((s) => ({ machines: { ...s.machines, [entry.machineId]: entry } }));
      },
      renameMachine: async (machineId, patch) => {
        const current = get().machines[machineId];
        if (!current) return;
        const next: MachineEntry = { ...current, ...patch };
        set((s) => ({ machines: { ...s.machines, [machineId]: next } }));
        const res = await fetch(`/api/machines/${encodeURIComponent(machineId)}/presentation`, {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            label: next.label,
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
      removeMachine: async (machineId) => {
        const res = await fetch(`/api/machines/${encodeURIComponent(machineId)}`, {
          method: "DELETE",
        });
        if (!res.ok) {
          const detail = await res
            .json()
            .then((body: { error?: string }) => body.error)
            .catch(() => undefined);
          throw new Error(detail ?? `machine removal failed (${res.status})`);
        }
        set((s) => {
          const machines = { ...s.machines };
          delete machines[machineId];
          const statuses = { ...s.statuses };
          delete statuses[machineId];
          const faults = { ...s.faults };
          delete faults[machineId];
          const versions = { ...s.versions };
          delete versions[machineId];
          // lastSeenAt is persisted, so a missed prune here outlives the tab.
          const lastSeenAt = { ...s.lastSeenAt };
          delete lastSeenAt[machineId];
          return { machines, statuses, faults, versions, lastSeenAt };
        });
      },
      setFault: (machineId, fault) =>
        set((s) => {
          const current = s.faults[machineId];
          if (!fault) {
            if (!current) return s;
            const faults = { ...s.faults };
            delete faults[machineId];
            return { faults };
          }
          if (current?.kind === fault.kind) return s; // same fault, don't churn
          return { faults: { ...s.faults, [machineId]: fault } };
        }),

      setVersion: (machineId, version) =>
        set((s) => {
          // An empty answer never overwrites a real last-known version — a
          // descriptor from an older build simply says nothing about it.
          if (!version || s.versions[machineId] === version) return s;
          return { versions: { ...s.versions, [machineId]: version } };
        }),

      setPlatform: (machineId, platformOs) =>
        set((s) => {
          const entry = s.machines[machineId];
          if (!entry || !platformOs || entry.platformOs === platformOs) return s;
          return {
            machines: { ...s.machines, [machineId]: { ...entry, platformOs } },
          };
        }),

      setStatus: (machineId, status) =>
        set((s) => {
          if (s.statuses[machineId] === status) return s;
          const next: Partial<MachineState> = {
            statuses: { ...s.statuses, [machineId]: status },
          };
          // Stamp on the way in AND on the way out: while connected "last
          // seen" is now, and the moment it drops that stamp is when it was
          // last real.
          // Deliberately NOT clearing faults here: a socket opening proves
          // only that something answered. An address that changed hands opens
          // a socket perfectly happily — the fault clears when the machine is
          // admitted (identity verified), not when it merely connects.
          if (status === "connected") {
            next.lastSeenAt = { ...s.lastSeenAt, [machineId]: Date.now() };
          } else if (s.statuses[machineId] === "connected") {
            next.lastSeenAt = { ...s.lastSeenAt, [machineId]: Date.now() };
          }
          return next as MachineState;
        }),
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
      version: 2,
      // Connection status is per-tab runtime state, never persisted.
      // Faults are runtime findings — re-proven on the next failed connect,
      // never restored from a cache that might be describing yesterday.
      // Versions ARE persisted: "what was it running when it left" is the
      // honest answer for an away machine, and it is re-proven on connect.
      partialize: (s) => ({
        machines: Object.fromEntries(
          Object.entries(s.machines).map(([id, entry]) => [id, { ...entry, token: "" }]),
        ),
        lastSeenAt: s.lastSeenAt,
        versions: s.versions,
      }),
      migrate: (persisted) => {
        const state = persisted as Partial<MachineState>;
        return {
          ...state,
          versions: state.versions ?? {},
          machines: Object.fromEntries(
            Object.entries(state.machines ?? {}).map(([id, entry]) => [
              id,
              {
                ...entry,
                token: "",
                sessionId: entry.sessionId ?? "",
                identityKey: entry.identityKey ?? "",
              },
            ]),
          ),
        } as MachineState;
      },
    },
  ),
);
