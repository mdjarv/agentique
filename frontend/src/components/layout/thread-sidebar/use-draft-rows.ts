/**
 * Maps the UI store's unsent New-session drafts into sidebar rows.
 *
 * A draft is text and a project id, nothing more — the composer persists it on
 * every keystroke and `NewChatPanel` only creates the session on send. So the
 * work is a lookup: resolve the project (through its logical representative, so
 * one repo reads the same on every machine) and drop drafts whose project is
 * gone or whose text is blank.
 */
import { useMemo } from "react";
import { useTheme } from "~/hooks/useTheme";
import { groupProjects } from "~/lib/machines/grouping";
import { displaySlug } from "~/lib/machines/slug";
import { getProjectColor } from "~/lib/project-colors";
import { projectInitials, projectLabel } from "~/lib/project-label";
import { newSessionDraftProjectId } from "~/lib/session/new-session-draft";
import type { Project } from "~/lib/types";
import { useAppStore } from "~/stores/app-store";
import { useMachineStore } from "~/stores/machine-store";
import { useUIStore } from "~/stores/ui-store";
import { compareDraftRows, draftHasMore, draftMatchesQuery, draftTitle } from "./draft-rows";
import type { DraftRowVM } from "./types";

export function useDraftRows(searchQuery: string): DraftRowVM[] {
  const drafts = useUIStore((s) => s.drafts);
  const projects = useAppStore((s) => s.projects);
  const machines = useMachineStore((s) => s.machines);
  const machineStatuses = useMachineStore((s) => s.statuses);
  const { resolvedTheme } = useTheme();

  return useMemo(() => {
    const projectById = new Map<string, Project>(projects.map((p) => [p.id, p]));
    const projectIds = projects.map((p) => p.id);
    // Presentation comes from the logical project's representative; the draft
    // still targets the physical project it was typed for.
    const repById = new Map<string, Project>();
    for (const { project: rep, members } of groupProjects(projects)) {
      for (const member of members) repById.set(member.id, rep);
    }

    const query = searchQuery.trim().toLowerCase();
    const rows: DraftRowVM[] = [];

    for (const [draftKey, text] of Object.entries(drafts)) {
      const projectId = newSessionDraftProjectId(draftKey);
      if (!projectId) continue;
      const title = draftTitle(text);
      if (!title) continue;
      // A draft for a project this client can no longer see (deleted, or a
      // machine unpaired) has nowhere to open — skip it rather than render a
      // row whose click goes nowhere.
      const project = projectById.get(projectId);
      if (!project) continue;
      const rep = repById.get(project.id) ?? project;
      const color = getProjectColor(rep.color, rep.id, projectIds, resolvedTheme);
      const remoteMachine = project.machineId ? machines[project.machineId] : undefined;
      const repLabel = projectLabel(rep.name, displaySlug(rep.slug));

      const vm: DraftRowVM = {
        draftKey,
        projectId,
        projectSlug: project.slug,
        projectLabel: repLabel,
        projectName: rep.name,
        projectInitials: projectInitials(repLabel),
        projectColorBg: color.bg,
        projectColorFg: color.fg,
        projectIconId: rep.icon || undefined,
        title,
        more: draftHasMore(text),
        remoteMachineLabel: remoteMachine?.label,
        remoteMachineIcon: remoteMachine?.icon || undefined,
        remoteMachinePlatform: remoteMachine?.platformOs || undefined,
        remoteMachineOffline: project.machineId
          ? machineStatuses[project.machineId] !== "connected"
          : undefined,
      };
      if (draftMatchesQuery(vm, query)) rows.push(vm);
    }

    return rows.sort(compareDraftRows);
  }, [drafts, projects, machines, machineStatuses, resolvedTheme, searchQuery]);
}
