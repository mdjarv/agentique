/**
 * What a proposal says on a card: the verb in words, the evidence as facts, and
 * the outcome once somebody has decided it.
 *
 * One table, two surfaces — the thread's card and the deck's row — on
 * `REST_GLYPH`'s precedent: a row that says "Delete the worktree and branch" on
 * the deck cannot say something softer in the thread.
 *
 * The words are the CLIENT's, deliberately, even though the server has its own
 * sentence per verb (`verbCheck.words`). That one is written for the head to
 * read back in a conversation; this one is a button's neighbour, and the two
 * have different jobs. What the server owns and this never guesses at is the
 * evidence, the rationale and the outcome — all three arrive as text.
 */

/** The eight uncontained verbs, in the words a card shows. */
const VERB_WORDS: Record<string, string> = {
  merge_session: "Merge the branch",
  rebase_session: "Rebase the branch",
  archive_session: "Archive the session",
  delete_session: "Delete the worktree and branch",
  reclaim_session: "Reclaim the disk",
  dissolve_channel: "Dissolve the channel",
  set_session_model: "Change the model",
  set_session_mode: "Change the permission mode",
};

/**
 * The verb in words.
 *
 * A verb this build has never heard of reads as its own name with the
 * underscores taken out, rather than as a blank: a peer one release ahead can
 * propose something this client cannot name, and a card with no title is one
 * nobody can judge — where "set session priority" at least says what it is.
 */
export function proposalWords(verb: string | undefined): string {
  if (!verb) return "A proposal";
  return VERB_WORDS[verb] ?? verb.replace(/_/g, " ");
}

/** Whether accepting this verb destroys something. Delete and dissolve only. */
export function proposalIsDestructive(verb: string | undefined): boolean {
  return verb === "delete_session" || verb === "dissolve_channel";
}

/** One quoted server fact: what it is, and what it said. */
export interface ProposalFact {
  key: string;
  label: string;
  value: string;
}

function commits(n: number): string {
  return n === 1 ? "1 commit" : `${n} commits`;
}

/**
 * How each evidence key reads. Keyed by the name the server writes, and the
 * order here is the order a card prints them in — the git facts as a sentence
 * reads them (ahead, behind, worktree, merge), then the process, then the
 * storage verdict.
 *
 * `busy` is two different facts under one name: a boolean for a session ("a
 * turn is in flight") and a count for a channel ("2 of its members"). Both are
 * rendered rather than one of them being assumed, because the reading that
 * guesses wrong prints "true" beside a member count.
 */
const FACTS: { key: string; label: string; render: (value: unknown) => string | null }[] = [
  { key: "channel", label: "channel", render: (v) => (typeof v === "string" ? v : null) },
  { key: "members", label: "members", render: (v) => (typeof v === "number" ? String(v) : null) },
  { key: "ahead", label: "ahead", render: (v) => (typeof v === "number" ? commits(v) : null) },
  { key: "behind", label: "behind", render: (v) => (typeof v === "number" ? commits(v) : null) },
  {
    key: "dirty",
    label: "worktree",
    // Not "clean": `mergeStatus` says "clean" too, about a different thing, and
    // two facts on one line reading identically is worse than a longer phrase.
    render: (v) =>
      typeof v === "boolean" ? (v ? "uncommitted changes" : "nothing uncommitted") : null,
  },
  { key: "mergeStatus", label: "merge", render: (v) => (typeof v === "string" ? v : null) },
  {
    key: "busy",
    label: "busy",
    render: (v) => {
      if (typeof v === "boolean") return v ? "a turn is in flight" : "no turn in flight";
      if (typeof v === "number") return v === 1 ? "1 member working" : `${v} members working`;
      return null;
    },
  },
  {
    key: "safety",
    label: "git",
    render: (v) => (typeof v === "string" && v ? v : null),
  },
  {
    key: "reason",
    label: "because",
    render: (v) => (typeof v === "string" && v ? v : null),
  },
  {
    key: "reclaimable",
    label: "reclaimable",
    render: (v) => (typeof v === "boolean" ? (v ? "yes" : "no") : null),
  },
  { key: "model", label: "now running", render: (v) => (typeof v === "string" ? v : null) },
  { key: "mode", label: "now in", render: (v) => (typeof v === "string" ? v : null) },
  {
    key: "live",
    label: "CLI",
    render: (v) => (typeof v === "boolean" ? (v ? "running" : "not attached") : null),
  },
];

const KNOWN = new Set(FACTS.map((f) => f.key));

/**
 * The evidence as facts to quote, in a fixed order.
 *
 * `safe` is deliberately not in the table: it is `safety`'s boolean twin, and
 * printing both says the same thing twice in two vocabularies. A key this build
 * has no rendering for still prints — under its own name, JSON-ish — because
 * the evidence is the whole reason a card can be judged, and silently dropping
 * a fact a newer server judged on is worse than an ugly line.
 */
export function proposalFacts(evidence: Record<string, unknown> | undefined): ProposalFact[] {
  if (!evidence) return [];
  const out: ProposalFact[] = [];
  for (const fact of FACTS) {
    if (!(fact.key in evidence)) continue;
    const value = fact.render(evidence[fact.key]);
    if (value === null) continue;
    out.push({ key: fact.key, label: fact.label, value });
  }
  for (const key of Object.keys(evidence).sort()) {
    if (KNOWN.has(key) || key === "safe") continue;
    const raw = evidence[key];
    const value = typeof raw === "string" ? raw : JSON.stringify(raw);
    if (value === undefined) continue;
    out.push({ key, label: key, value });
  }
  return out;
}

/**
 * What a decided proposal says in place of its two buttons.
 *
 * Five statuses and five sentences, because they are not degrees of one thing:
 * `stale` performed NOTHING and `failed` tried and could not, and a card that
 * blurred those would have the reader guessing whether their branch moved.
 * `outcome` is the server's own one line and is appended where there is one.
 */
export function proposalOutcomeWords(status: string | undefined): string {
  switch (status) {
    case "accepted":
      return "Accepted";
    case "declined":
      return "Declined — nothing was done";
    case "stale":
      return "The facts had moved, so nothing was done";
    case "failed":
      return "It was attempted and failed";
    case "expired":
      return "It expired undecided";
    default:
      return "Decided";
  }
}

/** Whether a decided status should read as a failure. */
export function proposalWentWrong(status: string | undefined): boolean {
  return status === "failed" || status === "stale";
}
