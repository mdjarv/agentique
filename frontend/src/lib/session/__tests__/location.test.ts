import { describe, expect, it } from "vitest";
import { machineHue, machineWash } from "~/lib/machine-colors";
import {
  machineTitle,
  WORKTREE_GLYPH,
  WORKTREE_LABEL,
  worktreeKind,
  worktreeZone,
} from "~/lib/session/location";

describe("worktreeKind", () => {
  it("reads the branch, not the path — worktreePath is set for both", () => {
    expect(worktreeKind("session-3f5")).toBe("linked");
    expect(worktreeKind(undefined)).toBe("main");
    expect(worktreeKind(null)).toBe("main");
    expect(worktreeKind("")).toBe("main");
  });
});

describe("worktreeZone", () => {
  it("names the session's own branch for a linked worktree, quietly", () => {
    const z = worktreeZone({ worktreeBranch: "session-3f5" });
    expect(z).toMatchObject({ kind: "linked", label: "session-3f5", tone: "quiet" });
  });

  it("names the project's branch for the main worktree, and warns", () => {
    const z = worktreeZone({ projectBranch: "master" });
    expect(z).toMatchObject({ kind: "main", label: "master", tone: "warn" });
    expect(z.title).toContain("main worktree");
  });

  it("falls back to words when the project branch has not arrived", () => {
    // projectBranch rides a git-status push that can land after first render;
    // an empty second zone would be worse than the category name.
    expect(worktreeZone({}).label).toBe(WORKTREE_LABEL.main);
    expect(worktreeZone({ projectBranch: "" }).label).toBe(WORKTREE_LABEL.main);
  });

  it("a missing branch outranks naming it", () => {
    const z = worktreeZone({ worktreeBranch: "session-3f5", branchMissing: true });
    expect(z).toMatchObject({ label: "branch gone", tone: "fault" });
  });

  it("branchMissing on the main worktree is not the session's problem", () => {
    // The flag is about the session's own branch; the main worktree has none.
    expect(worktreeZone({ branchMissing: true, projectBranch: "master" }).tone).toBe("warn");
  });

  it("gives every kind a glyph", () => {
    expect(Object.keys(WORKTREE_GLYPH).sort()).toEqual(["linked", "main"]);
  });
});

describe("machineHue", () => {
  const ids = ["m-charlie", "m-alpha", "m-bravo"];

  it("returns null for the primary, so a plain header keeps meaning 'here'", () => {
    expect(machineHue(undefined, ids, "dark")).toBeNull();
    expect(machineHue(null, ids, "dark")).toBeNull();
  });

  it("is stable regardless of the order the caller supplies", () => {
    const a = machineHue("m-bravo", ids, "dark");
    const b = machineHue("m-bravo", [...ids].reverse(), "dark");
    expect(a).toEqual(b);
  });

  it("gives different machines different hues", () => {
    const a = machineHue("m-alpha", ids, "dark");
    const b = machineHue("m-bravo", ids, "dark");
    expect(a?.bg).not.toBe(b?.bg);
  });

  it("colours an unknown id rather than reporting it as primary", () => {
    expect(machineHue("m-unknown", ids, "dark")).not.toBeNull();
  });

  it("takes light ink on light backgrounds", () => {
    const dark = machineHue("m-alpha", ids, "dark");
    const light = machineHue("m-alpha", ids, "light");
    expect(dark?.fg).not.toBe(light?.fg);
    expect(dark?.bg).toBe(light?.bg);
  });
});

describe("machineWash", () => {
  it("paints nothing for the primary", () => {
    expect(machineWash(null)).toBeUndefined();
  });

  it("drains to neutral when the machine is away", () => {
    const hue = machineHue("m-alpha", ["m-alpha"], "dark");
    const live = machineWash(hue);
    const away = machineWash(hue, { away: true });
    expect(live?.backgroundImage).toContain(hue?.bg.replace("#", ""));
    expect(away?.backgroundImage).not.toContain(hue?.bg.replace("#", ""));
  });
});

describe("machineTitle", () => {
  it("names the fault above everything else", () => {
    expect(machineTitle("zbook", { fault: "credential rejected", status: "connected" })).toBe(
      "zbook: credential rejected",
    );
  });

  it("stays quiet about a healthy connection", () => {
    expect(machineTitle("zbook", { baseUrl: "https://z:8790", status: "connected" })).toBe(
      "Runs on zbook (https://z:8790)",
    );
  });

  it("names a status that is not connected", () => {
    expect(machineTitle("zbook", { status: "reconnecting" })).toBe(
      "Runs on zbook — reconnecting",
    );
  });
});
