/**
 * View-model for cross-machine project merging (multi-machine).
 *
 * `groupProjects` answers "which physical projects are the same repo";
 * this file answers "how does that repo render as ONE row" — the shape every
 * project-listing surface (New palette, /projects, Run-in menus) consumes so
 * they can't drift apart again. The rules it encodes:
 *
 * - The representative — the primary machine's copy when it exists — owns
 *   everything presentational: name, slug (routing), color, icon, and the
 *   favorite star. A remote machine's own star is its host's opinion and is
 *   deliberately ignored here; the machine serving this UI owns presentation.
 * - A row is `away` only when EVERY member's machine is away. A repo that
 *   also lives locally is always launchable.
 * - Members stay individually addressable (`members`), because commands —
 *   launching, settings, git — always target one physical project.
 */
import type { Project } from "~/lib/types";
import type { MachineStatus } from "~/stores/machine-store";
import { groupProjects } from "./grouping";

/** What a machine looks like to a row. Passed in so this stays pure. */
export interface MachineFacts {
  label: string;
  /** Lucide icon id chosen for the machine on this host; "" falls back. */
  icon?: string;
  /** The machine's own OS (GOOS), for the platform mark when no icon is set. */
  platformOs?: string;
  status: MachineStatus;
}

export interface LogicalMemberVM {
  projectId: string;
  /** Absent = the primary machine (the one serving this UI). */
  machineId?: string;
  /** "" for the primary — callers name it themselves ("This machine"). */
  machineLabel: string;
  machineIcon?: string;
  /** The machine's own OS (GOOS); absent when unknown. */
  machinePlatform?: string;
  offline: boolean;
  path: string;
  /** The member's own slug — routing to it needs the qualified one. */
  slug: string;
}

export interface LogicalProjectVM {
  /** Representative project id — the default command target. */
  id: string;
  /** Representative slug: what routes resolve. */
  slug: string;
  name: string;
  /** Representative-owned (see file header). */
  favorite: boolean;
  color: string;
  icon: string;
  folder: string;
  path: string;
  /** Every member's machine is away — nothing can run here right now. */
  away: boolean;
  /** True once the repo exists on more than one machine. */
  spansMachines: boolean;
  members: LogicalMemberVM[];
  /** Members on a remote machine, representative excluded. */
  remoteMembers: LogicalMemberVM[];
}

/**
 * Collapse the physical project list into logical rows. `machines` maps a
 * machineId to its facts; a machineId missing from it is treated as away
 * (an entry we know nothing about cannot be claimed reachable).
 */
export function deriveLogicalProjects(
  projects: Project[],
  machines: Record<string, MachineFacts>,
): LogicalProjectVM[] {
  const rows: LogicalProjectVM[] = [];
  for (const { project: rep, members } of groupProjects(projects)) {
    const memberVMs = members.map<LogicalMemberVM>((m) => {
      const facts = m.machineId ? machines[m.machineId] : undefined;
      return {
        projectId: m.id,
        machineId: m.machineId,
        machineLabel: facts?.label ?? (m.machineId ? "Unknown machine" : ""),
        machineIcon: facts?.icon || undefined,
        machinePlatform: facts?.platformOs || undefined,
        // The primary serves this page: it is reachable by definition.
        offline: !!m.machineId && facts?.status !== "connected",
        path: m.path,
        slug: m.slug,
      };
    });
    rows.push({
      id: rep.id,
      slug: rep.slug,
      name: rep.name,
      favorite: rep.favorite === 1,
      color: rep.color,
      icon: rep.icon,
      folder: rep.folder,
      path: rep.path,
      away: memberVMs.every((m) => m.offline),
      spansMachines: memberVMs.length > 1,
      members: memberVMs,
      remoteMembers: memberVMs.filter((m) => m.projectId !== rep.id && !!m.machineId),
    });
  }
  return rows;
}

/** Favorites first, then by name — the shared order of every project list. */
export function compareLogicalProjects(a: LogicalProjectVM, b: LogicalProjectVM): number {
  if (a.favorite !== b.favorite) return a.favorite ? -1 : 1;
  return a.name.localeCompare(b.name);
}

/**
 * Free-text match across the WHOLE group: typing a remote copy's name
 * ("Agentique" when only zbook calls it that) must still find the merged row.
 */
export function matchesLogicalProject(
  row: LogicalProjectVM,
  projectsById: Map<string, Project>,
  query: string,
  includePath = false,
): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  for (const member of row.members) {
    const p = projectsById.get(member.projectId);
    if (!p) continue;
    if (p.name.toLowerCase().includes(q)) return true;
    if (p.slug.toLowerCase().includes(q)) return true;
    if (includePath && p.path.toLowerCase().includes(q)) return true;
  }
  return false;
}
