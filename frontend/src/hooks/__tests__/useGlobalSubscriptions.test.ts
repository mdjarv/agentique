import { beforeEach, describe, expect, it, vi } from "vitest";
import { loadProjectsInOrder } from "~/hooks/useGlobalSubscriptions";
import type { Project } from "~/lib/types";
import type { WsClient } from "~/lib/ws-client";
import { useChatStore } from "~/stores/chat-store";

/**
 * A socket serves requests in the order they were sent, so the order the
 * boot fires them in IS the priority. These pin the order: the focused
 * project alone, its focused session's history next, everything else after.
 */

const project = (id: string, slug: string): Project =>
  ({ id, slug, name: slug, path: `/${slug}` }) as Project;

const meta = (id: string, projectId: string, archivedAt?: string) => ({
  id,
  projectId,
  name: id,
  state: "stopped",
  connected: false,
  pinned: false,
  pinOrder: 0,
  model: "sonnet",
  permissionMode: "default",
  autoApproveMode: "manual",
  behaviorPresets: { autoCommit: true, suggestParallel: true, planFirst: false, terse: false },
  totalCost: 0,
  turnCount: 1,
  commitsAhead: 0,
  commitsBehind: 0,
  gitVersion: 0,
  createdAt: "2024-01-01T00:00:00Z",
  updatedAt: "2024-01-01T00:00:00Z",
  archivedAt,
});

const SESSIONS: Record<string, ReturnType<typeof meta>[]> = {
  A: [
    meta("a-one-11111111", "A"),
    meta("a-two-22222222", "A"),
    meta("a-old-33333333", "A", "2024-02-01T00:00:00Z"),
  ],
  B: [meta("b-one-44444444", "B")],
  C: [],
};

/** A fake socket that answers everything and records the order of asking. */
function fakeWs() {
  const sent: string[] = [];
  const ws = {
    request: vi.fn((type: string, payload: Record<string, unknown>) => {
      const subject = (payload.projectId ?? payload.sessionId ?? "") as string;
      sent.push(`${type} ${subject}${payload.limit ? " tail" : ""}`.trim());
      // Answer on a later tick so ordering reflects the sequencing logic
      // rather than synchronous resolution.
      return new Promise((resolve) => {
        setTimeout(() => {
          if (type === "session.list") {
            resolve({ sessions: SESSIONS[subject] ?? [] });
          } else if (type === "session.history") {
            resolve({
              turns: [{ prompt: "p", events: [{ type: "result" }], turnIndex: 0 }],
              hasMore: !!payload.limit,
              totalTurns: 2,
              epoch: 1,
              highWaterSeq: 1,
            });
          } else if (type === "project.git-status") {
            resolve({
              projectId: subject,
              branch: "main",
              hasRemote: false,
              aheadRemote: 0,
              behindRemote: 0,
              uncommittedCount: 0,
            });
          } else {
            resolve([]);
          }
        }, 0);
      });
    }),
  } as unknown as WsClient;
  return { ws, sent };
}

async function settled(sent: string[], histories: number) {
  // Every project lists; every open session asks for history at least once.
  await vi.waitFor(() => {
    expect(sent.filter((s) => s.startsWith("session.list"))).toHaveLength(3);
    expect(sent.filter((s) => s.startsWith("session.history"))).toHaveLength(histories);
  });
}

describe("loadProjectsInOrder", () => {
  beforeEach(() => {
    useChatStore.setState({
      sessions: {},
      activeSessionId: null,
      loadedProjects: new Set(),
      historyLoading: new Set(),
    });
  });

  it("puts the focused project and its session ahead of every other project", async () => {
    const { ws, sent } = fakeWs();
    const projects = [project("B", "beta"), project("A", "alpha"), project("C", "gamma")];

    await loadProjectsInOrder(ws, projects, { projectSlug: "alpha", sessionShortId: "a-two" });
    // Three open sessions plus the focused one's full snapshot.
    await settled(sent, 4);

    const firstOther = sent.findIndex((s) => s.endsWith(" B") || s.endsWith(" C"));
    const focusedHistory = sent.indexOf("session.history a-two-22222222 tail");
    expect(sent[0]).toBe("project.subscribe A");
    expect(sent[1]).toBe("session.list A");
    expect(focusedHistory).toBeGreaterThan(-1);
    expect(focusedHistory).toBeLessThan(firstOther);
    // The focused session is asked first among A's sessions.
    expect(sent.indexOf("session.history a-one-11111111 tail")).toBeGreaterThan(focusedHistory);
  });

  it("gives the focused session its full history and the rest only a tail", async () => {
    const { ws, sent } = fakeWs();
    const projects = [project("A", "alpha"), project("B", "beta"), project("C", "gamma")];

    await loadProjectsInOrder(ws, projects, { projectSlug: "alpha", sessionShortId: "a-two" });
    await settled(sent, 4);
    // The focused session's tail said hasMore, so it goes on to the full
    // snapshot; nothing else does.
    await vi.waitFor(() => {
      expect(sent).toContain("session.history a-two-22222222");
    });

    const histories = sent.filter((s) => s.startsWith("session.history"));
    expect(histories.filter((s) => !s.endsWith("tail"))).toEqual([
      "session.history a-two-22222222",
    ]);
    expect(histories).not.toContain("session.history a-old-33333333 tail");
    expect(useChatStore.getState().sessions["a-one-11111111"]?.historyComplete).toBe(false);
  });

  it("fires everything at once when nothing is focused", async () => {
    const { ws, sent } = fakeWs();
    const projects = [project("A", "alpha"), project("B", "beta"), project("C", "gamma")];

    await loadProjectsInOrder(ws, projects, {});

    // All three lists are on the wire before any answer has arrived.
    expect(sent.filter((s) => s.startsWith("session.list"))).toHaveLength(3);
    // Nothing is focused, so nothing loads in full: one tail per open session.
    await settled(sent, 3);
    expect(sent.filter((s) => s.startsWith("session.history") && !s.endsWith("tail"))).toEqual([]);
  });
});
