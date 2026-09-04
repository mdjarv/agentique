import { beforeEach, describe, expect, it } from "vitest";
import { hydrateMachineCache, saveMachineCache } from "~/lib/machines/cache";
import { useAppStore } from "~/stores/app-store";
import type { SessionMetadata } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";

const MACHINE = "m-1";
const KEY = `agentique:machine-cache:${MACHINE}`;

function meta(overrides: Partial<SessionMetadata> = {}): SessionMetadata {
  return {
    id: "s-1",
    projectId: "p-1",
    name: "Cached Session",
    state: "idle",
    connected: false,
    pinned: false,
    pinOrder: 0,
    model: "sonnet",
    permissionMode: "default",
    autoApproveMode: "manual",
    behaviorPresets: { autoCommit: true, suggestParallel: true, planFirst: false, terse: false },
    totalCost: 0,
    turnCount: 0,
    commitsAhead: 0,
    commitsBehind: 0,
    gitVersion: 0,
    createdAt: "2026-08-01T00:00:00Z",
    updatedAt: "2026-08-01T00:00:00Z",
    ...overrides,
  };
}

const project = {
  id: "p-1",
  name: "Repo",
  slug: "repo",
  path: "/repo",
  machineId: MACHINE,
} as unknown as ReturnType<typeof useAppStore.getState>["projects"][number];

beforeEach(() => {
  localStorage.clear();
  useChatStore.setState({ sessions: {}, activeSessionId: null });
  useAppStore.setState({ projects: [] });
});

describe("machine cache", () => {
  it("round-trips a session through save and hydrate", () => {
    useAppStore.setState({ projects: [project] });
    useChatStore.getState().addSession(meta({ archivedAt: "2026-08-20T10:00:00Z" }));

    saveMachineCache(MACHINE);
    useChatStore.setState({ sessions: {} });
    useAppStore.setState({ projects: [] });
    hydrateMachineCache(MACHINE);

    expect(useChatStore.getState().sessions["s-1"]?.meta.archivedAt).toBe("2026-08-20T10:00:00Z");
  });

  // The reported symptom: after upgrading, a reload showed a list of greyed
  // sessions that vanished one after another. They came from THIS cache —
  // written by the release before the rename, so the archive marker was stored
  // as `completedAt`. Hydration read `archivedAt`, found nothing, and put every
  // archived session on that machine into Open until the live list corrected it.
  it("hydrates a cache written before the archive rename", () => {
    localStorage.setItem(
      KEY,
      JSON.stringify({
        savedAt: "2026-08-20T10:00:00Z",
        projects: [project],
        sessions: [{ ...meta(), completedAt: "2026-08-20T10:00:00Z" }],
      }),
    );

    hydrateMachineCache(MACHINE);

    const cached = useChatStore.getState().sessions["s-1"]?.meta;
    expect(cached?.archivedAt).toBe("2026-08-20T10:00:00Z");
  });

  it("leaves an un-archived cached session open", () => {
    localStorage.setItem(
      KEY,
      JSON.stringify({
        savedAt: "2026-08-20T10:00:00Z",
        projects: [project],
        sessions: [meta()],
      }),
    );

    hydrateMachineCache(MACHINE);

    expect(useChatStore.getState().sessions["s-1"]?.meta.archivedAt).toBeUndefined();
  });

  // The snapshot sanitizes live-ness on its way to localStorage: a cached
  // "running" OR "merging" session on an unreachable machine would rehydrate
  // as phantom live-ness on reload — the rail animating its live mark for a
  // machine that is away, and Archive refusing. Neither state can be true on
  // a machine that stopped answering.
  it("settles live states out of the snapshot before persisting", () => {
    useAppStore.setState({ projects: [project] });
    useChatStore.getState().addSession(meta({ id: "s-run", state: "running", connected: true }));
    useChatStore.getState().addSession(meta({ id: "s-merge", state: "merging", connected: true }));
    useChatStore.getState().addSession(meta({ id: "s-done", state: "done", connected: false }));

    saveMachineCache(MACHINE);
    useChatStore.setState({ sessions: {} });
    useAppStore.setState({ projects: [] });
    hydrateMachineCache(MACHINE);

    const sessions = useChatStore.getState().sessions;
    expect(sessions["s-run"]?.meta.state).toBe("idle");
    expect(sessions["s-run"]?.meta.connected).toBe(false);
    expect(sessions["s-merge"]?.meta.state).toBe("idle");
    expect(sessions["s-merge"]?.meta.connected).toBe(false);
    // An outcome is a fact, not live-ness — the snapshot keeps it.
    expect(sessions["s-done"]?.meta.state).toBe("done");
  });

  // A cache written by a BUILD WE DO NOT KNOW cannot be interpreted safely —
  // rendering a guess is worse than showing nothing, since the live re-sync
  // repopulates the moment the machine connects.
  it("discards a cache from a newer build rather than guessing at it", () => {
    localStorage.setItem(
      KEY,
      JSON.stringify({
        version: 9999,
        savedAt: "2026-08-20T10:00:00Z",
        projects: [project],
        sessions: [meta()],
      }),
    );

    hydrateMachineCache(MACHINE);

    expect(useChatStore.getState().sessions["s-1"]).toBeUndefined();
    expect(useAppStore.getState().projects).toHaveLength(0);
  });
});
