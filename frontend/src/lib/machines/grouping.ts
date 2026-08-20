import type { Project } from "~/lib/types";

/**
 * Cross-machine project grouping (multi-machine). Projects on different
 * machines whose primary git remotes canonicalize to the same key
 * (`remote_url`, e.g. "github.com/org/repo") are one logical project. The
 * grouping is display-only — commands always target a physical member — and
 * the PRIMARY machine's copy, when present, is the representative that
 * drives the group's name, color, icon, folder, and navigation slug.
 * Projects without a usable remote never group.
 */
export interface LogicalProject {
  /** Representative member — the primary machine's copy when it exists. */
  project: Project;
  /** All physical members, representative first. */
  members: Project[];
}

export function groupProjects(projects: Project[]): LogicalProject[] {
  const byKey = new Map<string, Project[]>();
  const order: string[] = [];
  for (const p of projects) {
    // '' = no remote — such projects stay singleton groups keyed by id.
    const key = p.remote_url ? `r:${p.remote_url}` : `p:${p.id}`;
    const bucket = byKey.get(key);
    if (bucket) bucket.push(p);
    else {
      byKey.set(key, [p]);
      order.push(key);
    }
  }
  const groups: LogicalProject[] = [];
  for (const key of order) {
    const members = byKey.get(key);
    if (!members || members.length === 0) continue;
    const rep = members.find((m) => !m.machineId) ?? members[0];
    if (!rep) continue;
    groups.push({ project: rep, members: [rep, ...members.filter((m) => m !== rep)] });
  }
  return groups;
}
