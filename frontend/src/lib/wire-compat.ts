/**
 * Cross-version wire compatibility — the single owner of every "this peer runs
 * an older release" rule.
 *
 * A client talks to several servers at once: the primary plus one per paired
 * machine, each on whatever release that machine happens to be running. So a
 * rename to the wire vocabulary is never atomic, and shipping one without a
 * transition breaks the machines that have not upgraded yet. That is exactly
 * what the `completedAt` → `archivedAt` rename did: remote archived sessions
 * came back as open work, and pressing Archive on them returned
 * `unknown message type: session.archive`.
 *
 * Both directions are handled, and both live here rather than at call sites:
 *
 *  - **Reading** — {@link readArchivedAt} accepts either field name.
 *  - **Sending** — {@link LEGACY_OP} declares the old name for a renamed RPC;
 *    `ws-rpc` retries with it once when a peer rejects the new one, and
 *    remembers the answer per connection.
 *
 * The server half is symmetric: it emits both field names (see GitSnapshot's
 * MarshalJSON) and accepts both op names (see handlerRegistry). An entry can be
 * deleted from this file once no supported release predates its rename.
 */

/** Anything carrying a session's archive marker, from any release. */
interface ArchivableWire {
  archivedAt?: string;
  /** @deprecated Pre-rename name. Read only; never write. */
  completedAt?: string;
}

/**
 * The archive marker, whichever name the peer used.
 *
 * Returns `undefined` for a session that is not archived, and — importantly —
 * distinguishes that from a payload that never mentioned the field at all. A
 * new peer always states `archivedAt` (empty when open), so an absent value
 * means the peer is old and its `completedAt` is authoritative.
 */
export function readArchivedAt(wire: ArchivableWire | undefined): string | undefined {
  if (!wire) return undefined;
  if (wire.archivedAt !== undefined) return wire.archivedAt || undefined;
  return wire.completedAt || undefined;
}

/**
 * When a session finished a turn nobody has looked at yet, as the peer that
 * owns it says — or `undefined` when it did not say.
 *
 * The mark used to be client-only, so it lived in one tab and died with it. It
 * is server state now, which makes it cross-tab and cross-machine — but only
 * from a peer new enough to keep it. Reading through here rather than off the
 * generated type is deliberate twice over: a paired machine may predate the
 * field entirely, and the generated types do not carry it until the next
 * typegen run.
 *
 * Takes `unknown` because it is asked of raw push payloads as often as of typed
 * rows, and a payload from any release is exactly what it exists to survive.
 */
export function readUnseenCompletedAt(row: unknown): string | undefined {
  if (typeof row !== "object" || row === null) return undefined;
  const value = (row as { unseenCompletedAt?: unknown }).unseenCompletedAt;
  if (typeof value !== "string") return undefined;
  return value || undefined;
}

/**
 * Old names for renamed RPCs, tried when a peer rejects the current one.
 *
 * Keep this list short: an entry is a promise to support a release that is on
 * its way out, not a permanent second spelling.
 */
export const LEGACY_OP: Record<string, string> = {
  "session.archive": "session.mark-done",
  "session.unarchive": "session.unmark-done",
};

/**
 * The error a server returns for an op it has never heard of. Matched on text
 * because the pre-rename releases we are compensating for did not send a code.
 */
export function isUnknownOpError(err: unknown): boolean {
  return err instanceof Error && /unknown message type/i.test(err.message);
}
