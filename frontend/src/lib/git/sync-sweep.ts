/**
 * Keeping per-project git state true, cheaply.
 *
 * Two beats, deliberately different:
 *
 * - **status** is a local `git rev-list` against whatever the checkout already
 *   knows — cheap, but it can only report drift someone has already fetched.
 * - **fetch** talks to the remote, so it is the only thing that can discover
 *   that another machine pushed. It is expensive and therefore rare.
 *
 * The old polling hook ran status for every project every 10s, which was both
 * too much (a network round-trip per remote-machine project, every tick) and
 * too little (a number nobody had fetched since boot). Here the beats are
 * split, the sweeps are staggered rather than fired as one burst, and projects
 * on unreachable machines are skipped instead of timing out one by one.
 */
import { fetchProject, getProjectGitStatus } from "~/lib/project-actions";
import type { Project } from "~/lib/types";
import type { WsClient } from "~/lib/ws-client";
import { useAppStore } from "~/stores/app-store";
import { useMachineStore } from "~/stores/machine-store";

/** Local status refresh — every project, no network. */
export const STATUS_INTERVAL_MS = 60_000;
/** Remote fetch sweep — the only way to learn another machine pushed. */
export const FETCH_INTERVAL_MS = 10 * 60_000;
/** Gap between requests inside one sweep, so a sweep never lands as a burst. */
const STATUS_STAGGER_MS = 120;
const FETCH_STAGGER_MS = 600;
/** Beyond this, the dock stops presenting counts as fact. */
export const STALE_AFTER_MS = 30 * 60_000;

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * Projects worth asking about right now: a project on a remote machine that
 * isn't connected can only produce a timeout, and its cached status is already
 * the best answer we have.
 */
export function reachableProjects(projects: Project[]): Project[] {
  const statuses = useMachineStore.getState().statuses;
  return projects.filter((p) => !p.machineId || statuses[p.machineId] === "connected");
}

/** One project's status → store. Returns false when the request failed. */
async function refreshStatus(ws: WsClient, projectId: string): Promise<boolean> {
  try {
    const status = await getProjectGitStatus(ws, projectId);
    useAppStore.getState().setProjectGitStatus(status);
    return true;
  } catch (err) {
    console.error("git status failed", projectId, err);
    return false;
  }
}

/** One project's `git fetch` → store, stamping when it happened. */
async function refreshFetch(ws: WsClient, projectId: string): Promise<boolean> {
  try {
    const status = await fetchProject(ws, projectId);
    const store = useAppStore.getState();
    store.setProjectGitStatus(status);
    store.markProjectFetched(projectId, Date.now());
    return true;
  } catch (err) {
    console.error("git fetch failed", projectId, err);
    return false;
  }
}

/**
 * Sweep helper: walk projects one at a time with a gap between them, aborting
 * as soon as the caller's signal fires so a slow sweep can't outlive its
 * interval (or the component that started it).
 */
async function sweep(
  projects: Project[],
  gapMs: number,
  signal: AbortSignal | undefined,
  step: (projectId: string) => Promise<boolean>,
): Promise<void> {
  for (const project of projects) {
    if (signal?.aborted) return;
    await step(project.id);
    if (gapMs > 0) await sleep(gapMs);
  }
}

export function statusSweep(
  ws: WsClient,
  projects: Project[],
  signal?: AbortSignal,
): Promise<void> {
  return sweep(reachableProjects(projects), STATUS_STAGGER_MS, signal, (id) =>
    refreshStatus(ws, id),
  );
}

export function fetchSweep(ws: WsClient, projects: Project[], signal?: AbortSignal): Promise<void> {
  // A project without a remote has nothing to fetch from; skipping keeps the
  // sweep proportional to what can actually drift.
  const targets = reachableProjects(projects).filter((p) => {
    const status = useAppStore.getState().projectGitStatus[p.id];
    // Unknown status: try once — the first fetch is also what establishes it.
    return status ? status.hasRemote : true;
  });
  return sweep(targets, FETCH_STAGGER_MS, signal, (id) => refreshFetch(ws, id));
}

/**
 * The same repo's other checkouts — same canonical remote (`remote_url`, the
 * key `groupProjects` groups on), different physical project.
 */
export function siblingCheckouts(projects: Project[], projectId: string): Project[] {
  const source = projects.find((p) => p.id === projectId);
  if (!source?.remote_url) return [];
  return projects.filter((p) => p.id !== projectId && p.remote_url === source.remote_url);
}

/**
 * A push moves the *remote*, so every other checkout of that repo just went
 * behind — and no local command can discover it, only a fetch. Without this,
 * pushing from here leaves the laptop's copy of the same repo looking clean
 * until the ten-minute sweep comes round.
 *
 * Best-effort by design: it runs after the push has already been applied,
 * skips machines that are away (their cached status stays the best answer),
 * and a failure only means the sweep learns it later.
 */
export function fetchSiblings(
  ws: WsClient,
  projectId: string,
  signal?: AbortSignal,
): Promise<void> {
  const siblings = reachableProjects(
    siblingCheckouts(useAppStore.getState().projects, projectId),
  ).filter((p) => useAppStore.getState().projectGitStatus[p.id]?.hasRemote !== false);
  return sweep(siblings, FETCH_STAGGER_MS, signal, (id) => refreshFetch(ws, id));
}

/** Oldest fetch stamp across the given projects, or null if none have one. */
export function oldestFetchedAt(projectIds: string[]): number | null {
  const stamps = useAppStore.getState().projectGitFetchedAt;
  let oldest: number | null = null;
  for (const id of projectIds) {
    const at = stamps[id];
    if (at === undefined) continue;
    if (oldest === null || at < oldest) oldest = at;
  }
  return oldest;
}
