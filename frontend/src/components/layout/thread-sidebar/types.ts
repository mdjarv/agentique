/**
 * Thread-sidebar view-model.
 *
 * Deliberately decoupled from store/generated types: the integrator maps
 * `SessionData` (+ project/machine lookups) into `ThreadRowVM` via the pure
 * helpers in `derive.ts`, so these components never import from `~/stores/*`
 * or `~/lib/generated-types`.
 */

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
  | "draft"
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
export type MachineTone = "work" | "attn" | "unread" | "fail" | "merge" | "draft" | "muted";

export interface MachineLine {
  text: string;
  tone: MachineTone;
}

export interface ThreadRowVM {
  sessionId: string;
  name: string;
  untitled: boolean;
  /** Routing slug — qualified with a machine suffix for remote projects. */
  projectSlug: string;
  /** The slug as read: suffix dropped, since the row names the machine. */
  projectLabel: string;
  projectInitials: string;
  /** Bright project color (hex) — tinted to ~12% for the icon background. */
  projectColorBg: string;
  /** Theme-appropriate project accent (hex) — icon initials / glyph color. */
  projectColorFg: string;
  projectIconId?: string;
  badge: ThreadBadge;
  /** Identity wakes (project hue) for any live badge OR a connected CLI —
   *  an idle session with its claude process alive is active, just quiet. */
  awake: boolean;
  /** Line 3, live rows only — the state phrase in its tone. Absent at rest. */
  livePhrase?: MachineLine;
  /** Refines the `working` glyph; ignored for every other badge. */
  workKind?: WorkKind;
  /** One-word outcome shown inline on resting rows ("stopped", "evicted", …). */
  restToken: string;
  timeLabel: string;
  struck: boolean;
  unread: boolean;
  todo?: { done: number; total: number };
  workers?: number;
  pinned: boolean;
  remoteMachineLabel?: string;
  /** Icon id for that machine — this host's presentation of it. */
  remoteMachineIcon?: string;
  /** That machine is unreachable right now: the row is a cached snapshot. */
  remoteMachineOffline?: boolean;
  /** Epoch ms of last activity — drives the recency half of the Open sort. */
  lastActivity: number;
  /** Focused-card extras — rendered only on the selected row. */
  branch?: string;
  model?: string;
  turns?: number;
}

export interface ThreadGroups {
  pinned: ThreadRowVM[];
  open: ThreadRowVM[];
  /** Terminal + seen + quiet for a day — collected by the shelf below Open. */
  stale: ThreadRowVM[];
  archived: ThreadRowVM[];
}
