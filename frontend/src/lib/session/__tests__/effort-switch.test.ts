import { beforeEach, describe, expect, it, vi } from "vitest";
import { changeSessionEffort, isEffortChangeOutstanding } from "~/lib/session/effort-switch";
import type { WsClient } from "~/lib/ws-client";
import { useChatStore } from "~/stores/chat-store";
import type { SessionMetadata } from "~/stores/chat-types";

// The ramp names every stop a drag crosses. The thumb must follow at once, the
// wire must carry one request per session at a time, and a failure must put
// back what the server last confirmed.

function makeMeta(overrides: Partial<SessionMetadata> = {}): SessionMetadata {
  return {
    id: "sess-1",
    projectId: "proj-1",
    name: "Test Session",
    state: "idle",
    connected: true,
    pinned: false,
    pinOrder: 0,
    model: "opus",
    effort: "low",
    permissionMode: "default",
    autoApproveMode: "manual",
    behaviorPresets: { autoCommit: true, suggestParallel: true, planFirst: false, terse: false },
    totalCost: 0,
    turnCount: 0,
    commitsAhead: 0,
    commitsBehind: 0,
    gitVersion: 0,
    createdAt: "2024-01-01T00:00:00Z",
    updatedAt: "2024-01-01T00:00:00Z",
    ...overrides,
  };
}

/** A ws whose every request waits until the test answers it. */
function heldWs() {
  const calls: { effort: string; resolve: (v: unknown) => void; reject: (e: unknown) => void }[] =
    [];
  const request = vi.fn(
    (_type: string, payload: { effort: string }) =>
      new Promise((resolve, reject) => calls.push({ effort: payload.effort, resolve, reject })),
  );
  return { ws: { request } as unknown as WsClient, request, calls };
}

const effort = () => useChatStore.getState().sessions["sess-1"]?.meta.effort;
const flush = () => new Promise((r) => setTimeout(r, 0));

function report() {
  return { failed: vi.fn(), adjusted: vi.fn() };
}

describe("changeSessionEffort", () => {
  beforeEach(() => {
    useChatStore.setState({ sessions: {}, activeSessionId: null });
    useChatStore.getState().addSession(makeMeta());
  });

  it("moves the level at once and sends it", async () => {
    const { ws, request, calls } = heldWs();
    const r = report();

    changeSessionEffort(ws, "sess-1", "high", r);
    expect(effort()).toBe("high");
    expect(request).toHaveBeenCalledWith(
      "session.set-effort",
      { sessionId: "sess-1", effort: "high" },
      undefined,
    );
    expect(isEffortChangeOutstanding("sess-1")).toBe(true);

    calls[0]?.resolve({ effort: "high" });
    await flush();
    expect(isEffortChangeOutstanding("sess-1")).toBe(false);
    expect(effort()).toBe("high");
    expect(r.adjusted).not.toHaveBeenCalled();
  });

  it("sends only the latest of the levels chosen while one is out", async () => {
    const { ws, calls } = heldWs();
    const r = report();

    changeSessionEffort(ws, "sess-1", "medium", r);
    changeSessionEffort(ws, "sess-1", "high", r);
    changeSessionEffort(ws, "sess-1", "max", r);
    expect(effort()).toBe("max");
    expect(calls.map((c) => c.effort)).toEqual(["medium"]);

    calls[0]?.resolve({ effort: "medium" });
    await flush();
    expect(calls.map((c) => c.effort)).toEqual(["medium", "max"]);
    // The first answer does not drag the thumb back to where the drag began.
    expect(effort()).toBe("max");

    calls[1]?.resolve({ effort: "max" });
    await flush();
    expect(isEffortChangeOutstanding("sess-1")).toBe(false);
    expect(effort()).toBe("max");
  });

  it("puts back the confirmed level when the server refuses", async () => {
    const { ws, calls } = heldWs();
    const r = report();

    changeSessionEffort(ws, "sess-1", "max", r);
    calls[0]?.reject(new Error("session not live"));
    await flush();

    expect(effort()).toBe("low");
    expect(r.failed).toHaveBeenCalledTimes(1);
    expect(isEffortChangeOutstanding("sess-1")).toBe(false);
  });

  it("reports a level the provider adjusted", async () => {
    const { ws, calls } = heldWs();
    const r = report();

    changeSessionEffort(ws, "sess-1", "max", r);
    calls[0]?.resolve({ effort: "xhigh" });
    await flush();

    expect(r.adjusted).toHaveBeenCalledWith("max", "xhigh");
    // The row keeps the request; the ramp shows what resume will ask for.
    expect(effort()).toBe("max");
  });

  it("does not call a reset or an effortless model an adjustment", async () => {
    const { ws, calls } = heldWs();
    const r = report();

    changeSessionEffort(ws, "sess-1", "", r);
    calls[0]?.resolve({ effort: "high" });
    await flush();
    changeSessionEffort(ws, "sess-1", "high", r);
    calls[1]?.resolve({ effort: "" });
    await flush();

    expect(r.adjusted).not.toHaveBeenCalled();
  });
});
