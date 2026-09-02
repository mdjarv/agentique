/**
 * The project list as the UI should show it: one row per repo, not per
 * checkout (multi-machine). Every surface that lists projects to pick from
 * goes through here so a repo cloned on two machines can never render as two
 * unrelated rows again.
 *
 * Store selectors return the stores' own references (never a fresh
 * map/filter) and the derivation is memoized outside the selector — the
 * repo's Zustand stable-reference rule.
 */
import { useMemo } from "react";
import type { LogicalProjectVM, MachineFacts } from "~/lib/machines/logical-derive";
import { deriveLogicalProjects } from "~/lib/machines/logical-derive";
import { useAppStore } from "~/stores/app-store";
import { useMachineStore } from "~/stores/machine-store";

export function useLogicalProjects(): LogicalProjectVM[] {
  const projects = useAppStore((s) => s.projects);
  const machines = useMachineStore((s) => s.machines);
  const statuses = useMachineStore((s) => s.statuses);

  return useMemo(() => {
    const facts: Record<string, MachineFacts> = {};
    for (const [machineId, entry] of Object.entries(machines)) {
      facts[machineId] = {
        label: entry.label,
        icon: entry.icon,
        platformOs: entry.platformOs,
        status: statuses[machineId] ?? "disconnected",
      };
    }
    return deriveLogicalProjects(projects, facts);
  }, [projects, machines, statuses]);
}

/** The logical row a physical project belongs to, or undefined if unknown. */
export function useLogicalProjectOf(projectId: string | undefined): LogicalProjectVM | undefined {
  const rows = useLogicalProjects();
  return useMemo(
    () =>
      projectId ? rows.find((r) => r.members.some((m) => m.projectId === projectId)) : undefined,
    [rows, projectId],
  );
}
