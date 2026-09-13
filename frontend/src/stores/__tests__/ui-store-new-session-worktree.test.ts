import { describe, expect, it } from "vitest";
import { useUIStore } from "~/stores/ui-store";

describe("newSessionWorktree", () => {
  // Isolation is the default a fresh browser must get: a main-worktree session
  // edits the checkout the operator has open, so it is only ever chosen.
  it("starts on a linked worktree", () => {
    expect(useUIStore.getState().newSessionWorktree).toBe("linked");
  });

  it("is persisted, so the choice survives a reload", () => {
    useUIStore.getState().setNewSessionWorktree("main");
    expect(useUIStore.getState().newSessionWorktree).toBe("main");
    const stored = JSON.parse(localStorage.getItem("agentique:ui") ?? "{}");
    expect(stored.state.newSessionWorktree).toBe("main");
  });
});
