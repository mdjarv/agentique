import { describe, expect, it } from "vitest";
import { compareDraftRows, draftHasMore, draftMatchesQuery, draftTitle } from "../draft-rows";
import type { DraftRowVM } from "../types";

function row(overrides: Partial<DraftRowVM> = {}): DraftRowVM {
  return {
    draftKey: "new:p1",
    projectId: "p1",
    projectSlug: "agentique",
    projectLabel: "agentique",
    projectName: "Agentique",
    projectInitials: "A",
    projectColorBg: "#000000",
    projectColorFg: "#ffffff",
    title: "fix the reaper guard",
    more: false,
    ...overrides,
  };
}

describe("draftTitle", () => {
  it("takes the first line with content", () => {
    expect(draftTitle("\n\n  fix the reaper guard\nand the sweep\n")).toBe("fix the reaper guard");
  });

  it("collapses inner whitespace so a wrapped line reads as one", () => {
    expect(draftTitle("fix\tthe   reaper")).toBe("fix the reaper");
  });

  it("is empty for a blank draft, so the row is dropped", () => {
    expect(draftTitle("   \n\t\n")).toBe("");
  });

  it("caps a pasted spec rather than putting it in the render tree", () => {
    expect(draftTitle("x".repeat(500))).toHaveLength(160);
  });
});

describe("draftHasMore", () => {
  it("is false for a one-line draft", () => {
    expect(draftHasMore("just this")).toBe(false);
  });

  it("ignores trailing blank lines", () => {
    expect(draftHasMore("just this\n\n   \n")).toBe(false);
  });

  it("is true when another line has content", () => {
    expect(draftHasMore("first\n\nsecond")).toBe(true);
  });

  it("is true when the first line alone overflows the cap", () => {
    expect(draftHasMore("x".repeat(200))).toBe(true);
  });

  it("is false for a blank draft", () => {
    expect(draftHasMore("\n \n")).toBe(false);
  });
});

describe("draftMatchesQuery", () => {
  it("matches the draft's own words", () => {
    expect(draftMatchesQuery(row(), "reaper")).toBe(true);
  });

  it("matches the project it targets, by label, slug or name", () => {
    const vm = row({ projectLabel: "webticket-ui", projectSlug: "webticket-ui~ad3e932" });
    expect(draftMatchesQuery(vm, "webticket")).toBe(true);
    expect(draftMatchesQuery(vm, "ad3e932")).toBe(true);
    expect(draftMatchesQuery(row(), "agentique")).toBe(true);
  });

  it("rejects a miss, and passes everything through an empty query", () => {
    expect(draftMatchesQuery(row(), "nothing here")).toBe(false);
    expect(draftMatchesQuery(row(), "")).toBe(true);
  });
});

describe("compareDraftRows", () => {
  it("orders by project, then key — drafts carry no timestamp to sort on", () => {
    const a = row({ draftKey: "new:p1", projectLabel: "agentique" });
    const b = row({ draftKey: "new:p2", projectLabel: "webtickets" });
    expect([b, a].sort(compareDraftRows)).toEqual([a, b]);
  });

  it("breaks a tie on the key so the order survives a re-render", () => {
    const a = row({ draftKey: "new:p1" });
    const b = row({ draftKey: "new:p2" });
    expect([b, a].sort(compareDraftRows)).toEqual([a, b]);
  });
});
