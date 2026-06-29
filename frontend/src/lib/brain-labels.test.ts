import { describe, expect, it } from "vitest";
import type { Memory } from "~/lib/brain-api";
import { evidenceChip, isCapture, lifecycleBadge, volatilityChip } from "~/lib/brain-labels";

function mem(over: Partial<Memory> = {}): Memory {
  return {
    id: "m1",
    scope: "global",
    text: "x",
    category: "fact",
    source: "agent",
    pinned: false,
    locked: false,
    uses: 0,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...over,
  };
}

describe("brain-labels", () => {
  it("returns no evidence chip for the default (inferred) or unknown/empty", () => {
    expect(evidenceChip("inferred")).toBeNull();
    expect(evidenceChip(undefined)).toBeNull();
    expect(evidenceChip("nonsense")).toBeNull();
  });

  it("maps noteworthy evidence tiers to short labels", () => {
    expect(evidenceChip("code_verified")?.label).toBe("✓ code");
    expect(evidenceChip("corroborated")?.label).toBe("corroborated");
    expect(evidenceChip("user_stated")?.label).toBe("stated");
    expect(evidenceChip("observed_once")?.label).toBe("seen once");
  });

  it("returns no volatility chip for the default (slow), shows the rest", () => {
    expect(volatilityChip("slow")).toBeNull();
    expect(volatilityChip("evergreen")?.label).toBe("evergreen");
    expect(volatilityChip("ephemeral")?.label).toBe("ephemeral");
  });

  it("badges only non-active lifecycles", () => {
    expect(lifecycleBadge(mem({ lifecycle: "active" }))).toBeNull();
    expect(lifecycleBadge(mem())).toBeNull(); // unset → no badge
    expect(lifecycleBadge(mem({ lifecycle: "archived" }))?.variant).toBe("archived");
    expect(lifecycleBadge(mem({ lifecycle: "superseded" }))?.variant).toBe("superseded");
  });

  it("flags captures off source", () => {
    expect(isCapture(mem({ source: "capture" }))).toBe(true);
    expect(isCapture(mem({ source: "agent" }))).toBe(false);
  });
});
