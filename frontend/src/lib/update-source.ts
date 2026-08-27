/**
 * What a machine's local checkout is asking for, in one closed union
 * (docs/upgrades.md).
 *
 * The release channel and the source channel are different claims with
 * different costs, so a machine can be behind on both at once and neither
 * hides the other. This module decides only what the SOURCE half says. It does
 * no version arithmetic: the server compared the commits, and the client
 * compares nothing but the strings it was handed.
 *
 * The union is closed on purpose — a new state has to choose its words here
 * rather than inherit a blank row.
 */

import type { UpdateSourceStatus } from "~/lib/generated-types";

export type SourceToken =
  /** No checkout configured on that machine. The row renders nothing. */
  | "off"
  /** The running build is the branch's HEAD, and nothing is staged. */
  | "in-step"
  /** The branch has moved and this machine can build it. */
  | "ready"
  /** A newer binary is already at the install path; only the process is old. */
  | "staged"
  /** The branch has moved but building would be wrong or impossible. */
  | "blocked"
  /** Git could not answer. Unknown is never a licence to act. */
  | "unknown";

export interface SourceVerdict {
  token: SourceToken;
  /** The row's one line. */
  text: string;
  /** The second line, when there is more worth saying. */
  detail?: string;
  /** The button, when one can honestly be offered. */
  action?: { label: string; kind: "source" | "restart" };
  /** Whether this is asking for the operator's attention. */
  attention: boolean;
}

const NOTHING: SourceVerdict = { token: "off", text: "", attention: false };

/**
 * Rank: the cheapest COMPLETE answer, then the reason there is none.
 *
 * A staged binary built from the branch head wins outright — restarting is
 * seconds and there is nothing left to compile, so offering a rebuild there
 * would spend two minutes reproducing the identical commit. That is exactly
 * the state `just install` leaves behind.
 *
 * Otherwise rebuild outranks restart: a staged binary the branch has since
 * moved past is itself stale, and restarting into it would land the operator
 * one commit short and asking again.
 */
export function sourceVerdict(source: UpdateSourceStatus | undefined): SourceVerdict {
  if (!source?.dir) return NOTHING;

  // A binary this checkout did not produce has no source verdict, and the row
  // renders nothing rather than explaining itself. Someone who installed a
  // release does not need a line about a channel that will never apply to them
  // — their updates come from the release row directly above.
  if (source.origin !== "local") return NOTHING;

  if (source.staged && source.stagedIsCurrent) {
    return {
      token: "staged",
      text: "a newer build is installed",
      detail: source.installedVersion
        ? `${source.installedVersion} — restart to run it`
        : "restart to run it",
      action: { label: "Restart to finish", kind: "restart" },
      attention: true,
    };
  }

  if (source.behind && source.buildable) {
    return {
      token: "ready",
      text: `rebuild ${source.branch} · ${plural(source.ahead, "commit")} ahead`,
      detail: source.headSubject ? `${source.head} ${source.headSubject}` : source.head,
      action: { label: "Rebuild and restart", kind: "source" },
      attention: true,
    };
  }

  if (source.staged) {
    return {
      token: "staged",
      text: "a newer build is installed",
      detail: source.installedVersion
        ? `${source.installedVersion} — restart to run it`
        : "restart to run it",
      action: { label: "Restart to finish", kind: "restart" },
      attention: true,
    };
  }

  if (source.checkError) {
    return {
      token: "unknown",
      text: "checkout unreadable",
      detail: source.checkError,
      attention: false,
    };
  }

  // Behind on the facts, but something stops us acting on them. This is the
  // ordinary case on a machine someone is working on, so it is stated plainly
  // and asks for nothing.
  if (source.behind || source.ahead > 0) {
    return {
      token: "blocked",
      text: `${plural(source.ahead, "commit")} ahead on ${source.branch}`,
      detail: source.blocker || undefined,
      attention: false,
    };
  }

  return { token: "in-step", text: `running local ${source.branch}`, attention: false };
}

/** Whether this machine's checkout wants the operator to do something. */
export function sourceWantsAttention(source: UpdateSourceStatus | undefined): boolean {
  return sourceVerdict(source).attention;
}

function plural(n: number, noun: string): string {
  return `${n} ${noun}${n === 1 ? "" : "s"}`;
}
