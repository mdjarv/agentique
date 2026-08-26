import { describe, expect, it } from "vitest";
import { branchSync, hasBranchSync, resolveConflictsPrompt } from "~/lib/session/branch-sync";
import type { SessionMetadata } from "~/stores/chat-store";

function meta(over: Partial<SessionMetadata> = {}): SessionMetadata {
  return {
    id: "s1",
    projectId: "p1",
    name: "Voice Switchboard",
    state: "idle",
    worktreeBranch: "session-a1b2",
    commitsAhead: 0,
    commitsBehind: 0,
    mergeStatus: "clean",
    ...over,
  } as SessionMetadata;
}

describe("branchSync", () => {
  it("offers nothing when the branch is current", () => {
    expect(branchSync(meta(), true)).toEqual({ kind: "none" });
  });

  it("offers merge when ahead only", () => {
    expect(branchSync(meta({ commitsAhead: 3 }), true)).toEqual({ kind: "merge" });
  });

  // The state the whole change is for: main moved, nothing committed here yet.
  // There is no merge control for a rebase to hide inside.
  it("offers rebase when behind only", () => {
    expect(branchSync(meta({ commitsBehind: 2 }), true)).toEqual({ kind: "rebase", behind: 2 });
  });

  it("leads with rebase when diverged", () => {
    expect(branchSync(meta({ commitsAhead: 3, commitsBehind: 2 }), true)).toEqual({
      kind: "rebase-first",
      behind: 2,
    });
  });

  it("carries the behind count so the control can name it", () => {
    const sync = branchSync(meta({ commitsBehind: 7 }), true);
    expect(sync).toHaveProperty("behind", 7);
  });
});

describe("branchSync conflicts", () => {
  // A rebase would hit the same conflicts and the server would abort it; a
  // --ff-only merge would refuse. Neither verb is honest, so neither is offered.
  it("outranks both verbs when the merge-tree check says conflicts", () => {
    const sync = branchSync(
      meta({
        commitsAhead: 3,
        commitsBehind: 2,
        mergeStatus: "conflicts",
        mergeConflictFiles: ["a.ts", "b.ts"],
      }),
      true,
    );
    expect(sync).toEqual({ kind: "conflicts", files: ["a.ts", "b.ts"] });
  });

  it("still reports conflicts when the file list is absent", () => {
    const sync = branchSync(meta({ commitsAhead: 3, mergeStatus: "conflicts" }), true);
    expect(sync).toEqual({ kind: "conflicts", files: [] });
  });

  // "unknown" is git failing to answer, not a conflict. It must not silently
  // suppress the verbs, or a branch stays unmergeable with no explanation.
  it("treats an unknown merge status as no obstacle", () => {
    expect(branchSync(meta({ commitsAhead: 3, mergeStatus: "unknown" }), true)).toEqual({
      kind: "merge",
    });
  });
});

describe("branchSync guards", () => {
  it("offers nothing without git actions", () => {
    expect(branchSync(meta({ commitsAhead: 3 }), false)).toEqual({ kind: "none" });
  });

  it("offers nothing for a session with no worktree", () => {
    expect(branchSync(meta({ worktreeBranch: undefined, commitsAhead: 3 }), true)).toEqual({
      kind: "none",
    });
  });

  it("offers nothing when the branch is gone", () => {
    expect(branchSync(meta({ commitsAhead: 3, branchMissing: true }), true)).toEqual({
      kind: "none",
    });
  });

  it("offers nothing mid-turn", () => {
    expect(branchSync(meta({ commitsAhead: 3, state: "running" }), true)).toEqual({ kind: "none" });
  });

  // The regression this consolidation fixes: the header counted `merging` as
  // busy and the Changes bar did not, so the bar offered a rebase while a git
  // operation already held the session.
  it("offers nothing while a git operation holds the session", () => {
    expect(branchSync(meta({ commitsBehind: 2, state: "merging" }), true)).toEqual({
      kind: "none",
    });
    expect(branchSync(meta({ commitsAhead: 3, state: "merging" }), true)).toEqual({
      kind: "none",
    });
  });

  it("offers nothing once merged and settled", () => {
    expect(branchSync(meta({ worktreeMerged: true }), true)).toEqual({ kind: "none" });
  });

  // Merged but main has moved on since: still worth rebasing, so the flag alone
  // must not silence the control.
  it("still offers rebase on a merged branch that has fallen behind", () => {
    expect(branchSync(meta({ worktreeMerged: true, commitsBehind: 4 }), true)).toEqual({
      kind: "rebase",
      behind: 4,
    });
  });
});

describe("hasBranchSync", () => {
  it("agrees with branchSync", () => {
    expect(hasBranchSync(meta(), true)).toBe(false);
    expect(hasBranchSync(meta({ commitsBehind: 1 }), true)).toBe(true);
    expect(hasBranchSync(meta({ commitsAhead: 1 }), true)).toBe(true);
    expect(hasBranchSync(meta({ commitsAhead: 1, state: "running" }), true)).toBe(false);
  });
});

describe("resolveConflictsPrompt", () => {
  // The worktree caveat is load-bearing: rebasing onto origin/main in a
  // worktree is the wrong base, so the prompt derives the local project HEAD.
  it("tells the agent to rebase onto the local project HEAD, not origin", () => {
    const prompt = resolveConflictsPrompt(["a.ts"]);
    expect(prompt).toContain("not origin");
    expect(prompt).toContain("git worktree list --porcelain");
    expect(prompt).not.toContain("origin/main");
  });

  it("names every conflicting file", () => {
    expect(resolveConflictsPrompt(["a.ts", "b/c.tsx"])).toContain("a.ts, b/c.tsx");
  });

  it("survives an empty file list", () => {
    expect(() => resolveConflictsPrompt([])).not.toThrow();
  });
});
