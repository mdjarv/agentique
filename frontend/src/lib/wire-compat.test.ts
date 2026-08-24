import { describe, expect, it, vi } from "vitest";
import { isUnknownOpError, LEGACY_OP, readArchivedAt } from "~/lib/wire-compat";
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
