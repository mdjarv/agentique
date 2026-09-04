import { beforeEach, describe, expect, it } from "vitest";
import type { SessionMetadata } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";

function meta(overrides: Partial<SessionMetadata> = {}): SessionMetadata {
  return {
    id: "s-1",
    projectId: "p-1",
    name: "Seat-map race condition",
    state: "running",
    connected: true,
    createdAt: "",
    updatedAt: "",
    pinned: false,
    pinOrder: 0,
    ...overrides,
  } as SessionMetadata;
}

function seed(metas: SessionMetadata[]) {
  useChatStore.setState({
    sessions: Object.fromEntries(
      metas.map((m) => [
        m.id,
        {
          meta: m,
          pendingApproval:
            m.id === "s-1" ? ({ approvalId: "a1", toolName: "Bash", input: null } as never) : null,
          pendingQuestion: null,
          turns: [],
        } as never,
      ]),
    ),
  });
}

describe("markSessionsAway", () => {
  beforeEach(() => {
    useChatStore.setState({ sessions: {} });
  });

  // A laptop that closed its lid mid-turn used to keep pulsing "running"
  // forever, and kept offering Allow/Deny on a request nothing could answer.
  it("settles a mid-turn session and drops what can't be answered", () => {
    seed([meta()]);
    useChatStore.getState().markSessionsAway(["s-1"]);
    const data = useChatStore.getState().sessions["s-1"];
    expect(data?.meta.state).toBe("idle");
    expect(data?.meta.connected).toBe(false);
    expect(data?.pendingApproval).toBeNull();
  });

  // "merging" is as live as "running" — git is working — so it can no more be
  // true on an away machine. Left standing it animated the rail's live mark
  // for an offline machine and made canArchive refuse.
  it("settles a merging session too — merging is live-ness, not an outcome", () => {
    seed([meta({ id: "s-5", state: "merging", connected: false })]);
    useChatStore.getState().markSessionsAway(["s-5"]);
    const data = useChatStore.getState().sessions["s-5"];
    expect(data?.meta.state).toBe("idle");
  });

  it("leaves terminal states alone — away doesn't rewrite an outcome", () => {
    seed([meta({ id: "s-2", state: "failed", connected: true })]);
    useChatStore.getState().markSessionsAway(["s-2"]);
    expect(useChatStore.getState().sessions["s-2"]?.meta.state).toBe("failed");
    expect(useChatStore.getState().sessions["s-2"]?.meta.connected).toBe(false);
  });

  it("touches only the sessions it is given, and ignores unknown ids", () => {
    seed([meta(), meta({ id: "s-3", projectId: "p-2" })]);
    useChatStore.getState().markSessionsAway(["s-1", "nope"]);
    expect(useChatStore.getState().sessions["s-3"]?.meta.state).toBe("running");
  });

  it("is a no-op for sessions already settled (keeps the store reference)", () => {
    seed([meta({ id: "s-4", state: "stopped", connected: false })]);
    const before = useChatStore.getState().sessions;
    useChatStore.getState().markSessionsAway(["s-4"]);
    expect(useChatStore.getState().sessions).toBe(before);
  });
});
