import { describe, expect, it } from "vitest";
import { deriveRestToken, isParked, REST_GLYPH, type RestToken } from "../rest-state";

describe("deriveRestToken", () => {
  it("ranks merged over stopped over finished", () => {
    expect(deriveRestToken({ state: "stopped", merged: true, connected: true })).toBe("merged");
    expect(deriveRestToken({ state: "stopped", merged: false, connected: true })).toBe("stopped");
    // "finished", never "done": the state means the CLI exited, and "done" would
    // read as the user's verdict on the work — that verdict is Archive.
    expect(deriveRestToken({ state: "done", merged: false, connected: true })).toBe("finished");
  });

  it("marks a disconnected idle session as evicted", () => {
    expect(deriveRestToken({ state: "idle", merged: false, connected: false })).toBe("evicted");
  });

  it("says nothing for a connected idle session", () => {
    expect(deriveRestToken({ state: "idle", merged: false, connected: true })).toBe("");
  });
});

// The idle sweep reclaims a CLI through the same StopSession the stop button
// takes, so both arrive as state "stopped" and only the server's mark separates
// them. Without it every reclaim read as somebody's deliberate stop.
describe("an idle-evicted session", () => {
  it("says evicted, not stopped", () => {
    expect(
      deriveRestToken({ state: "stopped", merged: false, connected: false, evicted: true }),
    ).toBe("evicted");
  });

  it("still says stopped when nothing marked it", () => {
    expect(
      deriveRestToken({ state: "stopped", merged: false, connected: false, evicted: false }),
    ).toBe("stopped");
  });

  // A peer that predates the field never speaks it. Silence has to read as the
  // answer every release before this one gave, not as an eviction.
  it("reads an absent mark as a plain stop", () => {
    expect(deriveRestToken({ state: "stopped", merged: false, connected: false })).toBe("stopped");
  });

  // Merged outranks it for the reason it outranks a plain stop: it is a fact
  // about the work, where this is a fact about a process.
  it("does not hide merged work", () => {
    expect(
      deriveRestToken({ state: "stopped", merged: true, connected: false, evicted: true }),
    ).toBe("merged");
  });

  // Unlike the `!connected` guess, the mark is a record of something that
  // happened, so a cached row may still report it. This mirrors the existing
  // rule that an offline machine's stopped session says "stopped", not "away".
  it("survives being read off an unreachable machine", () => {
    expect(
      deriveRestToken({
        state: "stopped",
        merged: false,
        connected: false,
        machineOffline: true,
        evicted: true,
      }),
    ).toBe("evicted");
  });
});

// "evicted" is a claim about something agentique DID (reclaimed the CLI). A
// machine we cannot reach has its sessions frozen to connected:false so no row
// claims to be live — reading that as "evicted" turns an honest unknown into a
// false statement about a CLI that is probably still running over there.
describe("a session on an unreachable machine", () => {
  it("says away, not evicted", () => {
    expect(
      deriveRestToken({ state: "idle", merged: false, connected: false, machineOffline: true }),
    ).toBe("away");
  });

  it("still says evicted when the machine is reachable", () => {
    expect(
      deriveRestToken({ state: "idle", merged: false, connected: false, machineOffline: false }),
    ).toBe("evicted");
  });

  // A real outcome outranks reachability: merged work is merged wherever it ran.
  it("does not hide a real outcome behind away", () => {
    expect(
      deriveRestToken({ state: "stopped", merged: true, connected: false, machineOffline: true }),
    ).toBe("merged");
    expect(
      deriveRestToken({ state: "done", merged: false, connected: false, machineOffline: true }),
    ).toBe("finished");
  });
});

describe("isParked", () => {
  it("covers the tokens that mean the process left, not the work", () => {
    expect(isParked("stopped")).toBe(true);
    expect(isParked("evicted")).toBe(true);
    expect(isParked("away")).toBe(true);
  });

  it("excludes the outcomes and the running case", () => {
    expect(isParked("finished")).toBe(false);
    expect(isParked("merged")).toBe(false);
    expect(isParked("")).toBe(false);
  });
});

// The token union is closed precisely so a new one cannot ship without a mark;
// this is the assertion that makes that promise real rather than a comment.
describe("REST_GLYPH", () => {
  it("has a glyph for every token but the empty one", () => {
    const tokens: Exclude<RestToken, "">[] = ["merged", "finished", "stopped", "evicted", "away"];
    for (const token of tokens) expect(REST_GLYPH[token]).toBeDefined();
    expect(Object.keys(REST_GLYPH).sort()).toEqual([...tokens].sort());
  });
});
