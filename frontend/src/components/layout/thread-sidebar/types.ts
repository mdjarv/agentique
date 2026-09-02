/**
 * Thread-sidebar view-model.
 *
 * Deliberately decoupled from store/generated types: the integrator maps
 * `SessionData` (+ project/machine lookups) into `ThreadRowVM` via the pure
 * helpers in `derive.ts`, so these components never import from `~/stores/*`
 * or `~/lib/generated-types`.
 */

import type { WorktreeKind } from "~/lib/session/location";
import type { RestToken } from "~/lib/session/rest-state";

export type { RestToken, WorktreeKind };

/** Corner-badge state on the project icon. `null` = at rest, no badge. */
export type ThreadBadge =
  | "working"
  | "planning"
  /** Blocked on a tool approval. */
  | "attention"
  /** Blocked on an answer — same amber, its own glyph. */
  | "question"
  | "unread"
  | "failed"
  | "merging"
  | "off"
  | null;

/**
 * What kind of work a running session is doing — refines the `working` badge's
 * glyph so the state line says *what sort* of work without spending a word on
 * it. Derived from the pulse's tool category; `generic` when unknown.
 */
export type WorkKind =
  | "run"
  | "edit"
  | "read"
  | "web"
  | "delegate"
  | "task"
  | "plan"
  | "configure"
  | "tool"
  | "generic";

/** Color tone of the machine line (maps onto the shared state palette tokens). */
export type MachineTone = "work" | "attn" | "fail" | "merge" | "muted";

export interface MachineLine {
  text: string;
  tone: MachineTone;
}

export interface ThreadRowVM {
  sessionId: string;
  name: string;
  untitled: boolean;
  /** The lead that spawned this session, when one did. */
  parentSessionId?: string;
  /**
   * How far this row is indented under a lead. Nesting is one level deep by
   * construction: a worker cannot spawn, so it can never have children of its
   * own, and a depth the data cannot reach is a state to keep out of the type.
   */
  depth: 0 | 1;
  /** Last worker under its lead — the connector rail stops at this row. */
  lastChild?: boolean;
  /** A lead whose workers are folded away. Undefined on every other row. */
  collapsed?: boolean;
  /** Routing slug — qualified with a machine suffix for remote projects. */
  projectSlug: string;
  /** The project as a reader names it — its name, not its slug. */
  projectLabel: string;
  projectInitials: string;
  /**
   * Which worktree the session edits — its own linked one, or the project's
   * main one. Derived by `worktreeKind` from the same field the session
   * header's location pill reads, so one session cannot read as linked in the
   * rail and main in the pane it opens.
   */
  workspace: WorktreeKind;
  /** Bright project color (hex) — tinted to ~12% for the icon background. */
  projectColorBg: string;
  /** Theme-appropriate project accent (hex) — icon initials / glyph color. */
  projectColorFg: string;
  projectIconId?: string;
  badge: ThreadBadge;
  /** Live enough to earn a third line: any live badge OR a connected CLI —
   *  an idle session with its claude process alive is active, just quiet. */
  awake: boolean;
  /**
   * Identity carries the project hue. Tracks "still mine to deal with", NOT
   * "a CLI is attached" — see {@link import("./derive").isHued}.
   */
  hued: boolean;
  /** Line 3, live rows only — the state phrase in its tone. Absent at rest. */
  livePhrase?: MachineLine;
  /** Refines the `working` glyph; ignored for every other badge. */
  workKind?: WorkKind;
  /**
   * The CLI is producing right now — the chip traces its comet and the time
   * slot yields to the orbit. Narrower than {@link awake}: a row blocked on an
   * approval is awake and must not animate, because nothing is moving.
   */
  live: boolean;
  /** One-word outcome shown inline on resting rows ("stopped", "evicted", …). */
  restToken: RestToken;
  /**
   * `restToken` is a parked one — no process attached, work unfinished. The
   * row wears this on the chip's corner instead of spelling it on the repo
   * line: parked is the row's least consequential fact and was its longest
   * word, and the chip is the one element at a constant x on every row shape,
   * so a mark there can be scanned down the column without reading.
   */
  parked: boolean;
  timeLabel: string;
  struck: boolean;
  unread: boolean;
  todo?: { done: number; total: number };
  workers?: number;
  pinned: boolean;
  /** Already filed away — the row's action unarchives instead of archiving. */
  archived: boolean;
  /**
   * Archiving is available. False while a turn is in flight: the server refuses
   * it there (the turn would keep running behind a row the user believes is
   * filed away), so the row must not offer the action either.
   */
  canArchive: boolean;
  remoteMachineLabel?: string;
  /** Icon id for that machine — this host's presentation of it. */
  remoteMachineIcon?: string;
  /** That machine's own OS (GOOS) — its platform mark when no icon is set. */
  remoteMachinePlatform?: string;
  /** That machine is unreachable right now: the row is a cached snapshot. */
  remoteMachineOffline?: boolean;
  /** A proven fault on that machine — away is silent, this is not. */
  remoteMachineFault?: string;
  /** Epoch ms of last activity — drives the recency half of the Open sort. */
  lastActivity: number;
  /** Focused-card extras — rendered only on the selected row. */
  branch?: string;
  model?: string;
  turns?: number;
}

/**
 * An unsent New-session prompt. Deliberately NOT a `ThreadRowVM`: no session
 * exists yet, so there is no state, no outcome, no pin and nothing to archive —
 * only the project it targets and the words the user left behind.
 */
export interface DraftRowVM {
  /** The ui-store draft key — row identity, and what Discard clears. */
  draftKey: string;
  projectId: string;
  /** Routing slug — machine-qualified for a remote project, like a session's. */
  projectSlug: string;
  /** The project as a reader names it — its name, not its slug. */
  projectLabel: string;
  /** Human project name — search matches it, the row does not show it. */
  projectName: string;
  projectInitials: string;
  projectColorBg: string;
  projectColorFg: string;
  projectIconId?: string;
  /** First line with content, whitespace collapsed. Never empty. */
  title: string;
  /** More text follows the title — the row shows an excerpt. */
  more: boolean;
  remoteMachineLabel?: string;
  remoteMachineIcon?: string;
  remoteMachinePlatform?: string;
  /** That machine is unreachable — sending will fail until it is back. */
  remoteMachineOffline?: boolean;
}

export interface ThreadGroups {
  pinned: ThreadRowVM[];
  open: ThreadRowVM[];
  /** Terminal + seen + quiet for a day — collected by the shelf below Open. */
  stale: ThreadRowVM[];
  archived: ThreadRowVM[];
}
