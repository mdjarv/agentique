import { useMemo } from "react";
import { useShallow } from "zustand/react/shallow";
import { useTheme } from "~/hooks/useTheme";
import { groupProjects } from "~/lib/machines/grouping";
import { getProjectColor, type ProjectColor } from "~/lib/project-colors";
import type { Project } from "~/lib/types";
import { useAppStore } from "~/stores/app-store";

export interface ProjectPresentation {
  /** The representative checkout — what the repo looks like, not where it runs. */
  project: Project | undefined;
  color: ProjectColor | undefined;
  /** Representative's Lucide icon id; "" when unset. */
  icon: string;
}

const NO_PRESENTATION: ProjectPresentation = { project: undefined, color: undefined, icon: "" };

/**
 * How a project looks, resolved through its logical representative.
 *
 * Presentation is representative-owned (docs/multi-machine.md): the same repo
 * checked out on two machines is one repo, and the machine serving this UI owns
 * what it looks like. Reading the physical row instead gives a session on a
 * remote machine that machine's opinion — its own colour if it set one, and
 * otherwise an auto-assigned hue computed against a different project list, so
 * the identical repo wore two different colours depending on which machine the
 * session happened to run on. That is exactly how a red project's chat pane
 * came out green while its sidebar row (which already resolves through the
 * representative) stayed red.
 *
 * Session-scoped surfaces take a physical project id and must come through
 * here. Surfaces that LIST projects already deal in logical rows
 * (`useLogicalProjects`) and can read the row's own fields.
 */
export function useProjectPresentation(projectId: string | undefined): ProjectPresentation {
  const projects = useAppStore((s) => s.projects);
  const projectIds = useAppStore(useShallow((s) => s.projects.map((p) => p.id)));
  const { resolvedTheme } = useTheme();

  return useMemo(() => {
    if (!projectId) return NO_PRESENTATION;
    let rep: Project | undefined;
    for (const group of groupProjects(projects)) {
      if (group.members.some((m) => m.id === projectId)) {
        rep = group.project;
        break;
      }
    }
    if (!rep) return NO_PRESENTATION;
    return {
      project: rep,
      // The auto-assign key is the representative's id, so a repo keeps one hue
      // however many machines hold a copy of it.
      color: getProjectColor(rep.color, rep.id, projectIds, resolvedTheme),
      icon: rep.icon ?? "",
    };
  }, [projectId, projects, projectIds, resolvedTheme]);
}
