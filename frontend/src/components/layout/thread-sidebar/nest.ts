/**
 * Places a lead's workers directly under it, on a connector rail.
 *
 * Runs as a pass over the already-partitioned sections rather than inside the
 * partition, because the two answer different questions and only one of them
 * is about sections. `sectionFor` decides where a session is *filed*; this
 * decides what a row is *next to*. Keeping them apart is what stops nesting
 * from having an opinion about pinning, staleness and archival.
 *
 * The output is still a flat list. A worker cannot spawn, so the tree is one
 * level deep and an ordered list with a `depth` flag carries all of it — which
 * leaves search, sorting and the drag-to-reorder handle working on exactly the
 * arrays they already worked on.
 *
 * Three rules, and each one is a case that showed up:
 *
 * - **A worker joins its lead's section**, wherever it was filed. A lead in
 *   Pinned with a worker in Open drew the rail across a section heading and
 *   claimed a parentage the layout could not honour.
 * - **A worker with no visible lead stays exactly where it is**, at depth 0.
 *   An archived lead, or one a search filtered out, must not take its workers
 *   off screen with it — an indent under nothing is an orphan that reads as a
 *   rendering bug.
 * - **Archived is never nested.** That section is a shelf, and a run somebody
 *   filed away has stopped being a hierarchy worth drawing.
 */
import type { ThreadGroups, ThreadRowVM } from "./types";

/** The three sections a worker may be pulled between. Archived is not one. */
type NestableSection = "pinned" | "open" | "stale";

const SECTIONS: NestableSection[] = ["pinned", "open", "stale"];

/**
 * Workers sort by recency among themselves, newest first, independent of how
 * their section sorts. A crew is read as a set — who is still out — and
 * inheriting Pinned's manual order would rank workers by an order nobody set.
 */
function compareWorkers(a: ThreadRowVM, b: ThreadRowVM): number {
  return b.lastActivity - a.lastActivity || a.sessionId.localeCompare(b.sessionId);
}

export function nestWorkers(
  groups: ThreadGroups,
  collapsedLeads: ReadonlySet<string>,
): ThreadGroups {
  // Which section each row currently sits in, and which rows exist at all.
  const sectionOf = new Map<string, NestableSection>();
  for (const section of SECTIONS) {
    for (const vm of groups[section]) sectionOf.set(vm.sessionId, section);
  }

  // A row is a worker only when its lead is visible in a nestable section.
  // Everything else keeps its own place, which is what makes the pass safe on
  // a filtered or partially-loaded list.
  const workersByLead = new Map<string, ThreadRowVM[]>();
  const nested = new Set<string>();
  for (const section of SECTIONS) {
    for (const vm of groups[section]) {
      const lead = vm.parentSessionId;
      if (!lead || !sectionOf.has(lead)) continue;
      const siblings = workersByLead.get(lead);
      if (siblings) siblings.push(vm);
      else workersByLead.set(lead, [vm]);
      nested.add(vm.sessionId);
    }
  }
  if (nested.size === 0) return groups;

  for (const siblings of workersByLead.values()) siblings.sort(compareWorkers);

  const rebuild = (section: NestableSection): ThreadRowVM[] => {
    const rows: ThreadRowVM[] = [];
    for (const vm of groups[section]) {
      // Workers are emitted under their lead, never in their own right — and
      // the lead may live in another section, which is why this is a skip
      // rather than a move.
      if (nested.has(vm.sessionId)) continue;
      const workers = workersByLead.get(vm.sessionId);
      if (!workers) {
        rows.push(vm.depth === 0 ? vm : { ...vm, depth: 0 });
        continue;
      }
      const collapsed = collapsedLeads.has(vm.sessionId);
      rows.push({ ...vm, depth: 0, collapsed });
      if (collapsed) continue;
      workers.forEach((worker, i) => {
        rows.push({ ...worker, depth: 1, lastChild: i === workers.length - 1 });
      });
    }
    return rows;
  };

  return {
    pinned: rebuild("pinned"),
    open: rebuild("open"),
    stale: rebuild("stale"),
    archived: groups.archived,
  };
}
