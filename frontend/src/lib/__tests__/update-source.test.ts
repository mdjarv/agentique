import { describe, expect, it } from "vitest";
import type { UpdateSourceStatus, UpdateStatus } from "~/lib/generated-types";
import { sourceVerdict, sourceWantsAttention } from "~/lib/update-source";
import { behindKeys } from "~/stores/update-store";

/** A source status with the shape the server sends, overridable per case. */
function src(over: Partial<UpdateSourceStatus> = {}): UpdateSourceStatus {
  return {
    dir: "/home/u/git/agentique",
    branch: "master",
    ahead: 0,
    behind: false,
    dirty: false,
    staged: false,
    buildable: false,
    origin: "local",
    ...over,
  };
}

function status(over: Partial<UpdateStatus> = {}): UpdateStatus {
  return {
    current: "v0.6.0-67-g1ae969a",
    latest: "",
    behind: false,
    channel: "dev",
    asset: "",
    supported: false,
    platform: "linux/amd64",
    checkedAt: "",
    installable: false,
    busy: false,
    busyTurns: 0,
    ...over,
  };
}

describe("sourceVerdict", () => {
  it("renders nothing when no checkout is configured", () => {
    expect(sourceVerdict(undefined).token).toBe("off");
    expect(sourceVerdict(src({ dir: "" })).token).toBe("off");
  });

  it("offers a rebuild when the branch has moved and the machine can build it", () => {
    const v = sourceVerdict(src({ ahead: 5, behind: true, buildable: true, head: "abc1234" }));
    expect(v.token).toBe("ready");
    expect(v.action).toEqual({ label: "Rebuild and restart", kind: "source" });
    expect(v.text).toContain("5 commits");
    expect(v.attention).toBe(true);
  });

  it("counts one commit in the singular", () => {
    const v = sourceVerdict(src({ ahead: 1, behind: true, buildable: true }));
    expect(v.text).toContain("1 commit ");
  });

  it("offers a restart when a newer binary is already installed", () => {
    const v = sourceVerdict(src({ staged: true, installedVersion: "v0.6.0-72-gdeadbee" }));
    expect(v.token).toBe("staged");
    expect(v.action).toEqual({ label: "Restart to finish", kind: "restart" });
    expect(v.detail).toContain("v0.6.0-72-gdeadbee");
  });

  // A staged binary the branch has moved past is itself stale, so restarting
  // would land one commit short and ask again.
  it("prefers a rebuild when the staged binary is not the branch head", () => {
    const v = sourceVerdict(
      src({ ahead: 2, behind: true, buildable: true, staged: true, stagedIsCurrent: false }),
    );
    expect(v.token).toBe("ready");
  });

  // …but a staged binary that IS the head needs no build at all. This is what
  // `just install` leaves behind, and rebuilding would recompile the identical
  // commit for two minutes.
  it("prefers a restart when the staged binary is already the branch head", () => {
    const v = sourceVerdict(
      src({ ahead: 5, behind: true, buildable: true, staged: true, stagedIsCurrent: true }),
    );
    expect(v.token).toBe("staged");
    expect(v.action).toEqual({ label: "Restart to finish", kind: "restart" });
  });

  it("states a dirty checkout without offering anything", () => {
    const v = sourceVerdict(
      src({ ahead: 3, dirty: true, blocker: "the checkout has uncommitted changes" }),
    );
    expect(v.token).toBe("blocked");
    expect(v.action).toBeUndefined();
    expect(v.attention).toBe(false);
    expect(v.detail).toContain("uncommitted");
  });

  it("states another branch without offering anything", () => {
    const v = sourceVerdict(
      src({ ahead: 4, checkedOut: "feature", blocker: "the checkout is on feature, not master" }),
    );
    expect(v.token).toBe("blocked");
    expect(v.action).toBeUndefined();
  });

  // Behind is the server's verdict; buildable is its preflight. A machine that
  // is behind but cannot build says so instead of offering a dead button.
  it("never offers a button when the server says it is not buildable", () => {
    const v = sourceVerdict(src({ ahead: 2, behind: true, buildable: false, blocker: "npm" }));
    expect(v.token).toBe("blocked");
    expect(v.action).toBeUndefined();
  });

  it("reports an unreadable checkout as unknown, never as an upgrade", () => {
    const v = sourceVerdict(src({ checkError: "not a git checkout" }));
    expect(v.token).toBe("unknown");
    expect(v.action).toBeUndefined();
    expect(v.attention).toBe(false);
  });

  it("says nothing worth acting on when in step", () => {
    const v = sourceVerdict(src());
    expect(v.token).toBe("in-step");
    expect(v.action).toBeUndefined();
    expect(v.attention).toBe(false);
  });

  // Only a locally-built binary has a relationship with this checkout. A
  // downloaded release renders nothing at all — not a blocker, not a line:
  // its updates come from the release row directly above.
  it("renders nothing for a binary this checkout did not build", () => {
    expect(
      sourceVerdict(src({ origin: "release", ahead: 5, behind: true, buildable: true })).token,
    ).toBe("off");
    expect(sourceVerdict(src({ origin: "", staged: true })).token).toBe("off");
  });
});

describe("sourceWantsAttention", () => {
  it("is true only for the two states with an action", () => {
    expect(sourceWantsAttention(src({ ahead: 1, behind: true, buildable: true }))).toBe(true);
    expect(sourceWantsAttention(src({ staged: true }))).toBe(true);
    expect(sourceWantsAttention(src({ ahead: 1, dirty: true }))).toBe(false);
    expect(sourceWantsAttention(src())).toBe(false);
    expect(sourceWantsAttention(undefined)).toBe(false);
  });
});

describe("behindKeys", () => {
  it("counts a release upgrade and a moved checkout alike", () => {
    const keys = behindKeys({
      primary: status({ source: src({ ahead: 2, behind: true, buildable: true }) }),
      remote: status({ behind: true, latest: "v0.7.0", channel: "release" }),
      quiet: status({ source: src() }),
    });
    expect(keys).toEqual(["primary", "remote"]);
  });

  it("puts the primary first", () => {
    const keys = behindKeys({
      remote: status({ behind: true, latest: "v0.7.0" }),
      primary: status({ behind: true, latest: "v0.7.0" }),
    });
    expect(keys[0]).toBe("primary");
  });

  it("ignores a machine whose checkout is dirty", () => {
    const keys = behindKeys({
      primary: status({ source: src({ ahead: 9, dirty: true }) }),
    });
    expect(keys).toEqual([]);
  });
});
