/**
 * Where a session's code lives: which machine, and which worktree on it.
 *
 * These were two chips with four other things between them — a machine pill and
 * a workspace pill — which is two elements for one fact. They are two segments
 * of one address, and the interesting case is the one where they compound: the
 * *main* worktree on a *remote* machine is the configuration that costs most
 * when something goes wrong, and nothing on the old bar said the two facts
 * together.
 *
 * The vocabulary is git's own. `git-worktree(1)`: a repository has "a main
 * worktree and zero or more linked worktrees", and `git worktree list` puts the
 * main one first. That is what the operator reads in their own terminal, and
 * what this app's worktrees are linked *to*, so it beats any word we could
 * invent ("live repo", "local", "root").
 *
 * Both zones name a **branch**, which is what removes the naming problem rather
 * than solving it: the kind rides the glyph and the colour, so the two halves
 * carry the same kind of content and the reader compares like with like. Only
 * when the project's branch has not arrived yet does the main case fall back to
 * words.
 *
 * The union is closed for the reason `REST_GLYPH`'s is: every surface that
 * shows a session reads this one table, so a new situation has to choose its
 * mark rather than inherit a blank.
 */
import { FolderOpen, GitBranch, type LucideIcon } from "lucide-react";

/** Which worktree of the project this session edits. */
export type WorktreeKind = "linked" | "main";

/**
 * How loudly a zone speaks. `warn` is the main worktree — edits land in the
 * checkout the operator has open in an editor — and `fault` is a branch git can
 * no longer find. Everything else is quiet, because the common case is correct.
 */
export type ZoneTone = "quiet" | "warn" | "fault" | "hue";

export interface WorktreeZone {
  kind: WorktreeKind;
  /** Branch name, or the fallback words when there is no branch to name. */
  label: string;
  tone: ZoneTone;
  title: string;
}

export interface LocationInput {
  /** The session's own branch. Absent means it runs in the project checkout. */
  worktreeBranch?: string | null;
  /** Git says the branch is gone — the verbs are already suppressed. */
  branchMissing?: boolean;
  /** The project checkout's current branch, for the main-worktree case. */
  projectBranch?: string | null;
}

export const WORKTREE_GLYPH: Record<WorktreeKind, LucideIcon> = {
  linked: GitBranch,
  // An open folder rather than FolderGit2: at 10px the branch node inside the
  // folder collapses into noise, and the two glyphs then differ by a detail too
  // small to see. Open-vs-branch is the distinction, so draw that.
  main: FolderOpen,
};

/** What a kind is called in prose — tooltips, popovers, aria labels. */
export const WORKTREE_LABEL: Record<WorktreeKind, string> = {
  linked: "linked worktree",
  main: "main worktree",
};

/**
 * A session is in a linked worktree iff it has a branch of its own. Absent
 * means the main worktree — which is why this reads the branch rather than the
 * path: `worktreePath` is set for both.
 */
export function worktreeKind(worktreeBranch?: string | null): WorktreeKind {
  return worktreeBranch ? "linked" : "main";
}

/**
 * The second zone of the location pill.
 *
 * A missing branch outranks everything else it could say: the session's own
 * branch is gone, so naming it would be naming something that is not there.
 */
export function worktreeZone(input: LocationInput): WorktreeZone {
  const kind = worktreeKind(input.worktreeBranch);

  if (kind === "linked") {
    if (input.branchMissing) {
      return {
        kind,
        label: "branch gone",
        tone: "fault",
        title: `Worktree branch ${input.worktreeBranch} no longer exists`,
      };
    }
    return {
      kind,
      // biome-ignore lint/style/noNonNullAssertion: kind is "linked" iff the branch is set
      label: input.worktreeBranch!,
      tone: "quiet",
      title: `Linked worktree on ${input.worktreeBranch} — edits are isolated from the project checkout`,
    };
  }

  // The main worktree names the project's current branch when it is known. The
  // words are the fallback, not the default: `projectBranch` arrives on a git
  // status push that can land after the first render.
  return {
    kind,
    label: input.projectBranch || WORKTREE_LABEL.main,
    tone: "warn",
    title: input.projectBranch
      ? `The main worktree, on ${input.projectBranch} — edits land in the checkout everything else is linked to`
      : "The main worktree — edits land in the checkout everything else is linked to",
  };
}

/** What the machine zone says on hover. Absent machine means the primary. */
export function machineTitle(
  label: string,
  opts: { baseUrl?: string; status?: string; fault?: string } = {},
): string {
  if (opts.fault) return `${label}: ${opts.fault}`;
  const where = opts.baseUrl ? ` (${opts.baseUrl})` : "";
  const state = opts.status && opts.status !== "connected" ? ` — ${opts.status}` : "";
  return `Runs on ${label}${where}${state}`;
}
