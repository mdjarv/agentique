import type { Project } from "~/lib/types";
import { readArchivedAt } from "~/lib/wire-compat";
import { useAppStore } from "~/stores/app-store";
import type { SessionMetadata } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import { settleAwayState } from "~/stores/chat-types";
import { usePulseStore } from "~/stores/pulse-store";

/**
 * Per-machine offline cache (multi-machine). Machines come and go — a
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

/**
 * Shape of the persisted session metadata. Bump whenever a field a cached
 * session carries is renamed or removed, and teach {@link migrateCached} how to
 * read the previous shape.
 *
 * This exists because the cache is a serialization of an INTERNAL type, so it
 * drifts the moment that type changes — and a stale cache does not fail loudly,
 * it renders wrong. Version 1 is the `completedAt` → `archivedAt` rename: caches
 * written before it made every archived session on that machine hydrate as open
 * work, so a reload showed a list of greyed rows that vanished one after another
 * as each machine's live list arrived and corrected them.
 */
const CACHE_VERSION = 1;

interface MachineCache {
  /** Absent means version 0 — written before the cache was versioned. */
  version?: number;
  savedAt: string;
  projects: Project[];
  sessions: SessionMetadata[];
}

/**
 * Bring cached sessions up to the current shape, or refuse them.
 *
 * Returns null when the cache cannot be interpreted — a build we do not know
 * wrote it. Showing nothing is strictly better than showing a guess: this is an
 * offline convenience, and the live re-sync repopulates it the moment the
 * machine connects.
 */
function migrateCached(cache: MachineCache): SessionMetadata[] | null {
  const sessions = cache.sessions ?? [];
  const version = cache.version ?? 0;

  if (version > CACHE_VERSION) return null;
  if (version === CACHE_VERSION) return sessions;

  // v0 → v1: the archive marker was called `completedAt`.
  return sessions.map((meta) => ({ ...meta, archivedAt: readArchivedAt(meta) }));
}

const keyFor = (machineId: string) => `agentique:machine-cache:${machineId}`;

/**
 * Freeze this machine's sessions in the LIVE store when it goes away.
 *
 * The snapshot below sanitizes live-ness on its way to localStorage, but the
 * store kept whatever was true when the laptop closed its lid — so a session
 * that was mid-turn kept pulsing, and its pending approval kept offering
 * Allow/Deny buttons that could only time out. Away means settled.
 */
export function freezeMachineSessions(machineId: string): void {
  const projectIds = new Set(
    useAppStore
      .getState()
      .projects.filter((p) => p.machineId === machineId)
      .map((p) => p.id),
  );
  if (projectIds.size === 0) return;

  const sessionIds = Object.values(useChatStore.getState().sessions)
    .filter((data) => projectIds.has(data.meta.projectId))
    .map((data) => data.meta.id);
  if (sessionIds.length === 0) return;

  useChatStore.getState().markSessionsAway(sessionIds);
  // The pulse is the live narration ("editing derive.ts · 12 tool calls").
  // Nothing is editing anything on a machine that is asleep.
  const pulses = usePulseStore.getState();
  for (const id of sessionIds) pulses.clearPulse(id);
}

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
      state: settleAwayState(data.meta.state),
      pendingApproval: undefined,
      pendingQuestion: undefined,
    });
  }

  try {
    localStorage.setItem(
      keyFor(machineId),
      JSON.stringify({
        version: CACHE_VERSION,
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

  // Refuse a cache we cannot read rather than hydrating a guess — nothing is
  // shown for this machine until it connects, which is the honest state.
  const cachedSessions = migrateCached(cache);
  if (cachedSessions === null) return;

  useAppStore.getState().setMachineProjects(machineId, cache.projects);
  const byProject = new Map<string, SessionMetadata[]>();
  for (const meta of cachedSessions) {
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
