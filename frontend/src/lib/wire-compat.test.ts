import { describe, expect, it, vi } from "vitest";
import {
  GitSnapshotSchema,
  PushSessionEventSchema,
  PushTurnStartedSchema,
} from "~/lib/generated-schemas";
import {
  isUnknownOpError,
  LEGACY_OP,
  readArchivedAt,
  readUnseenCompletedAt,
} from "~/lib/wire-compat";
import type { WsClient } from "~/lib/ws-client";
import { define } from "~/lib/ws-rpc";

/**
 * A client talks to one server per paired machine, each on whatever release
 * that machine runs. These lock both halves of the archive rename's transition.
 */

describe("readArchivedAt", () => {
  it("reads the current field name", () => {
    expect(readArchivedAt({ archivedAt: "2026-08-24T06:00:00Z" })).toBe("2026-08-24T06:00:00Z");
  });

  // The bug this exists for: a peer predating the rename says completedAt, and
  // reading only archivedAt put every archived session on that machine back in
  // the Open section.
  it("falls back to the pre-rename field name", () => {
    expect(readArchivedAt({ completedAt: "2026-08-24T06:00:00Z" })).toBe("2026-08-24T06:00:00Z");
  });

  it("prefers the current name when a peer sends both", () => {
    expect(readArchivedAt({ archivedAt: "new", completedAt: "old" })).toBe("new");
  });

  // A current peer states archivedAt on every snapshot, empty included — that
  // empty value is authoritative and must win over a stale legacy field.
  it("treats an explicit empty archivedAt as not archived", () => {
    expect(readArchivedAt({ archivedAt: "" })).toBeUndefined();
    expect(readArchivedAt({ archivedAt: "", completedAt: "old" })).toBeUndefined();
  });

  it("reports not-archived for an empty or absent marker", () => {
    expect(readArchivedAt({})).toBeUndefined();
    expect(readArchivedAt({ completedAt: "" })).toBeUndefined();
    expect(readArchivedAt(undefined)).toBeUndefined();
  });
});

describe("readUnseenCompletedAt", () => {
  it("reads the mark a peer that keeps it sends", () => {
    expect(readUnseenCompletedAt({ unseenCompletedAt: "2026-08-26T09:00:00Z" })).toBe(
      "2026-08-26T09:00:00Z",
    );
  });

  // The regression this pins: the read receipt's broadcast states the mark as
  // "" (a current peer always states it), and that explicit empty must read as
  // a clear — mapping it to undefined made "cleared" indistinguishable from
  // "peer predates the field", so the badge never left the other clients.
  it("reads a stated empty mark as an explicit clear", () => {
    expect(readUnseenCompletedAt({ unseenCompletedAt: "" })).toBe("");
    expect(readUnseenCompletedAt({ unseenCompletedAt: null })).toBe("");
  });

  // A payload without the key says nothing — the shape a peer from before the
  // field produces, which must never clear a badge.
  it("reports nothing when the field is absent", () => {
    expect(readUnseenCompletedAt({})).toBeUndefined();
  });

  // Raw push payloads arrive here from any release, including ones that
  // predate the field and ones that put something else in its place.
  it("survives a payload that is not what it expected", () => {
    expect(readUnseenCompletedAt(undefined)).toBeUndefined();
    expect(readUnseenCompletedAt(null)).toBeUndefined();
    expect(readUnseenCompletedAt("nonsense")).toBeUndefined();
    expect(readUnseenCompletedAt({ unseenCompletedAt: 1756198800 })).toBeUndefined();
  });
});

describe("isUnknownOpError", () => {
  it("recognises the rejection a peer sends for an op it lacks", () => {
    expect(isUnknownOpError(new Error("unknown message type: session.archive"))).toBe(true);
  });

  it("does not swallow real failures", () => {
    expect(isUnknownOpError(new Error("session busy"))).toBe(false);
    expect(isUnknownOpError(new Error("session not found"))).toBe(false);
    expect(isUnknownOpError("unknown message type")).toBe(false);
  });
});

function fakeClient(handler: (type: string) => Promise<unknown>) {
  const request = vi.fn((type: string) => handler(type));
  return { client: { request } as unknown as WsClient, request };
}

describe("define — renamed ops against an older peer", () => {
  it("sends the current name to a peer that understands it", async () => {
    const { client, request } = fakeClient(() => Promise.resolve(undefined));
    await define<unknown, { sessionId: string }>("session.archive")(client, { sessionId: "s1" });
    expect(request).toHaveBeenCalledTimes(1);
    expect(request.mock.calls[0]?.[0]).toBe("session.archive");
  });

  // The reported failure: "unknown message type: session.archive".
  it("retries under the pre-rename name when the peer rejects it", async () => {
    const { client, request } = fakeClient((type) =>
      type === "session.archive"
        ? Promise.reject(new Error("unknown message type: session.archive"))
        : Promise.resolve("ok"),
    );

    await expect(
      define<unknown, { sessionId: string }>("session.archive")(client, { sessionId: "s1" }),
    ).resolves.toBe("ok");
    expect(request.mock.calls.map((c) => c[0])).toEqual(["session.archive", "session.mark-done"]);
  });

  // One wasted round-trip per socket, not per click.
  it("remembers an older peer and stops probing it", async () => {
    const { client, request } = fakeClient((type) =>
      type === "session.archive"
        ? Promise.reject(new Error("unknown message type: session.archive"))
        : Promise.resolve("ok"),
    );
    const archive = define<unknown, { sessionId: string }>("session.archive");

    await archive(client, { sessionId: "s1" });
    await archive(client, { sessionId: "s2" });
    await archive(client, { sessionId: "s3" });

    expect(request.mock.calls.map((c) => c[0])).toEqual([
      "session.archive",
      "session.mark-done",
      "session.mark-done",
      "session.mark-done",
    ]);
  });

  // Learned per connection: an up-to-date machine must not be dragged onto the
  // legacy name because a different machine is behind.
  it("keeps the verdict per connection", async () => {
    const old = fakeClient((type) =>
      type === "session.archive"
        ? Promise.reject(new Error("unknown message type: session.archive"))
        : Promise.resolve("ok"),
    );
    const current = fakeClient(() => Promise.resolve("ok"));
    const archive = define<unknown, { sessionId: string }>("session.archive");

    await archive(old.client, { sessionId: "s1" });
    await archive(current.client, { sessionId: "s2" });

    expect(old.request.mock.calls.map((c) => c[0])).toEqual([
      "session.archive",
      "session.mark-done",
    ]);
    expect(current.request.mock.calls.map((c) => c[0])).toEqual(["session.archive"]);
  });

  it("propagates real errors instead of retrying", async () => {
    const { client, request } = fakeClient(() => Promise.reject(new Error("session busy")));
    await expect(
      define<unknown, { sessionId: string }>("session.archive")(client, { sessionId: "s1" }),
    ).rejects.toThrow("session busy");
    expect(request).toHaveBeenCalledTimes(1);
  });

  it("leaves ops that were never renamed alone", async () => {
    const { client, request } = fakeClient(() =>
      Promise.reject(new Error("unknown message type: session.stop")),
    );
    await expect(
      define<unknown, { sessionId: string }>("session.stop")(client, { sessionId: "s1" }),
    ).rejects.toThrow();
    expect(request).toHaveBeenCalledTimes(1);
  });

  it("declares both renamed archive ops", () => {
    expect(LEGACY_OP["session.archive"]).toBe("session.mark-done");
    expect(LEGACY_OP["session.unarchive"]).toBe("session.unmark-done");
  });
});

// The regression that made this file necessary a second time: `archivedAt` was
// briefly marked required on the wire, so the generated schema rejected every
// session.state push from a peer that predates the rename — the whole payload,
// state included. Remote rows simply froze. Wire fields stay optional.
describe("session.state accepts a payload from a peer that predates the rename", () => {
  const fromOldPeer = {
    sessionId: "s1",
    state: "done",
    connected: false,
    hasDirtyWorktree: false,
    hasUncommitted: false,
    worktreeMerged: false,
    completedAt: "2026-08-24T09:00:00Z",
    commitsAhead: 0,
    commitsBehind: 0,
    branchMissing: false,
    version: 7,
  };

  it("validates without archivedAt", () => {
    const parsed = GitSnapshotSchema.safeParse(fromOldPeer);
    expect(parsed.success).toBe(true);
  });

  it("and the archive marker still reads through", () => {
    expect(readArchivedAt(fromOldPeer)).toBe("2026-08-24T09:00:00Z");
  });

  it("still validates a current peer's payload", () => {
    const fromCurrentPeer = { ...fromOldPeer, archivedAt: "2026-08-24T09:00:00Z" };
    expect(GitSnapshotSchema.safeParse(fromCurrentPeer).success).toBe(true);
  });

  // A current peer states the unseen mark on every snapshot, "" when cleared;
  // that explicit empty is what lets a read receipt clear the badge elsewhere.
  it("validates a stated-empty unseen mark and reads it as a clear", () => {
    const cleared = { ...fromOldPeer, unseenCompletedAt: "" };
    expect(GitSnapshotSchema.safeParse(cleared).success).toBe(true);
    expect(readUnseenCompletedAt(cleared)).toBe("");
  });
});

// The same required-field regression, one struct over: seq/epoch/turnIndex
// were required in the generated schemas, so every session.event and
// session.turn-started push from a paired machine on a pre-seq release failed
// validation — the WHOLE payload silently dropped, its transcript frozen.
// Wire fields stay optional; absent seq reads as unsequenced (0).
describe("event pushes accept payloads from a peer that predates sequencing", () => {
  it("validates a session.event without seq/epoch", () => {
    const parsed = PushSessionEventSchema.safeParse({
      sessionId: "s1",
      event: { type: "text", content: "hello" },
    });
    expect(parsed.success).toBe(true);
  });

  it("validates a session.turn-started without turnIndex", () => {
    const parsed = PushTurnStartedSchema.safeParse({
      sessionId: "s1",
      prompt: "do the thing",
    });
    expect(parsed.success).toBe(true);
  });

  it("still validates a current peer's stamped payload", () => {
    const parsed = PushSessionEventSchema.safeParse({
      sessionId: "s1",
      event: { type: "text", content: "hello" },
      seq: 4,
      epoch: 2,
    });
    expect(parsed.success).toBe(true);
  });
});
