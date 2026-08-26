import type { SessionMetadata } from "~/stores/chat-store";

/**
 * What a worktree session's branch needs doing to it right now.
 *
 * Merge and rebase are not two options for one job — they answer to different
 * counts. Merge applies when the branch is *ahead*; rebase when it is *behind*.
 * Being behind with nothing committed yet is ordinary, which is why rebase can
 * never live inside merge's control: there would be no merge control to open.
 *
 * The union is closed on purpose. A new branch situation has to pick its own
 * member rather than inherit a blank, and every surface that renders a branch
 * control reads this one function — the desktop header, the mobile strip, and
 * the Changes bar in the dock. They each computed their own eligibility before,
 * and they already disagreed: the header counted `merging` as busy, the Changes
 * bar did not.
 */
export type BranchSync =
  /** Nothing to do, or nothing safe to offer (busy, no worktree, already merged). */
  | { kind: "none" }
  /** Ahead only. Merge fast-forwards. */
  | { kind: "merge" }
  /** Behind only. Rebase brings the branch onto current main. */
  | { kind: "rebase"; behind: number }
  /**
   * Diverged. Rebase leads and merge is demoted into its menu — merging a
   * branch that is behind is a `--ff-only` merge that cannot fast-forward, so
   * the server answers `needs_rebase` and the client has to ask for a rebase
   * anyway. Putting rebase first is the same answer, one step earlier.
   */
  | { kind: "rebase-first"; behind: number }
  /**
   * The merge-tree dry run says this branch will not apply cleanly. Neither
   * verb is honest here: a rebase would hit the same conflicts and the server
   * would abort it, and a merge would refuse. The established answer is to hand
   * the named files to the agent, which is what the Changes tab has always
   * done.
   */
  | { kind: "conflicts"; files: string[] };

/**
 * A session is busy for branch purposes while a turn is running or a git
 * operation holds it. `merging` is the state the server broadcasts while it
 * runs a merge or rebase, so a control offered then would race the operation
 * that is already in flight.
 */
function isBusy(state: string): boolean {
  return state === "running" || state === "merging";
}

/**
 * Decide what to offer for this session's branch.
 *
 * `hasGit` is the caller's git-actions availability: the header renders without
 * them on some routes, and a merge control with nothing behind it is worse than
 * none. Rebase needs them too, so it gates both.
 */
export function branchSync(meta: SessionMetadata, hasGit: boolean): BranchSync {
  const none: BranchSync = { kind: "none" };

  if (!hasGit || !meta.worktreeBranch || meta.branchMissing) return none;
  if (isBusy(meta.state)) return none;

  const ahead = meta.commitsAhead ?? 0;
  const behind = meta.commitsBehind ?? 0;

  // Already merged and settled — the row says so elsewhere; there is no verb.
  if (meta.worktreeMerged && ahead === 0 && behind === 0) return none;
  if (ahead === 0 && behind === 0) return none;

  // Conflicts outrank both verbs: offering one that predictably fails teaches
  // people to ignore the control.
  if (meta.mergeStatus === "conflicts") {
    return { kind: "conflicts", files: meta.mergeConflictFiles ?? [] };
  }

  if (ahead > 0 && behind > 0) return { kind: "rebase-first", behind };
  if (behind > 0) return { kind: "rebase", behind };
  if (ahead > 0) return { kind: "merge" };
  return none;
}

/** Whether any branch control renders for this session. */
export function hasBranchSync(meta: SessionMetadata, hasGit: boolean): boolean {
  return branchSync(meta, hasGit).kind !== "none";
}

/**
 * The prompt that hands a conflicted branch to the agent.
 *
 * Spelled once because two surfaces send it — the header control and the
 * Changes tab's conflicts section — and because the worktree caveat in it is
 * load-bearing: rebasing onto `origin/main` in a worktree is the wrong base,
 * so the prompt derives the *local* project HEAD instead.
 */
export function resolveConflictsPrompt(files: string[]): string {
  const list = files.join(", ");
  return (
    "This is a git worktree. Rebase onto the local project HEAD (not origin). " +
    "Get it via: main_wt=$(git worktree list --porcelain | head -1 | sed 's/worktree //') && " +
    'git rebase $(git -C "$main_wt" rev-parse HEAD). ' +
    `Resolve conflicts in: ${list}`
  );
}
