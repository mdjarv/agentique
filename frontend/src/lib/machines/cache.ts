import type { Project } from "~/lib/types";
import { useAppStore } from "~/stores/app-store";
import type { SessionMetadata } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";

/**
 * Per-machine offline cache (multi-machine M4). Machines come and go — a
 * laptop suspends in a bag while the VPS keeps serving the PWA — so each
 * machine's last-known projects and session metadata persist locally and
 * hydrate the stores at startup. The machine's half of the sidebar stays
 * visible and navigable while it is unreachable; the live re-sync on
 * (re)connect is authoritative and overwrites everything cached.
 *
 * Deliberately metadata-only: no turns/history (heavy, and useless without
 * the machine to act on them). Opening a cached session while its machine is
 * down shows the machine-offline status via the existing chips/dots.
 */

interface MachineCache {
  savedAt: string;
  projects: Project[];
  sessions: SessionMetadata[];
}

const keyFor = (machineId: string) => `agentique:machine-cache:${machineId}`;

/** Snapshot the machine's current store state. Called after a successful
 *  live load and again on disconnect (the freshest state we'll have). */
export function saveMachineCache(machineId: string): void {
  const projects = useAppStore.getState().projects.filter((p) => p.machineId === machineId);
  if (projects.length === 0) return;
  const projectIds = new Set(projects.map((p) => p.id));

  const sessions: SessionMetadata[] = [];
  for (const data of Object.values(useChatStore.getState().sessions)) {
    if (!projectIds.has(data.meta.projectId)) continue;
    // Sanitize live-ness out of the snapshot: a cached "running" session on
    // an unreachable machine would render a live pulse, and stale pending
    // approvals would beg for input nothing can answer.
    sessions.push({
      ...data.meta,
      connected: false,
      state: data.meta.state === "running" ? "idle" : data.meta.state,
      pendingApproval: undefined,
      pendingQuestion: undefined,
    });
  }

  try {
    localStorage.setItem(
      keyFor(machineId),
      JSON.stringify({
        savedAt: new Date().toISOString(),
        projects,
        sessions,
      } satisfies MachineCache),
    );
  } catch {
    // Quota/serialization failures just mean no offline view — never fatal.
  }
}

/** Hydrate the stores from the machine's cache. Merge-only (never
 *  authoritative): anything the live connection later reports wins. Skipped
 *  when the machine's projects are already loaded. */
export function hydrateMachineCache(machineId: string): void {
  if (useAppStore.getState().projects.some((p) => p.machineId === machineId)) return;

  let cache: MachineCache;
  try {
    const raw = localStorage.getItem(keyFor(machineId));
    if (!raw) return;
    cache = JSON.parse(raw) as MachineCache;
  } catch {
    return;
  }
  if (!Array.isArray(cache.projects) || cache.projects.length === 0) return;

  useAppStore.getState().setMachineProjects(machineId, cache.projects);
  const byProject = new Map<string, SessionMetadata[]>();
  for (const meta of cache.sessions ?? []) {
    const list = byProject.get(meta.projectId);
    if (list) list.push(meta);
    else byProject.set(meta.projectId, [meta]);
  }
  for (const [projectId, metas] of byProject) {
    useChatStore.getState().setSessions(metas, projectId, false);
  }
}

export function clearMachineCache(machineId: string): void {
  try {
    localStorage.removeItem(keyFor(machineId));
  } catch {
    // ignore
  }
}
