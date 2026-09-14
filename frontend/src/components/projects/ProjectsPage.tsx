/**
 * Projects — every checkout as it stands on disk.
 *
 * Settings › Projects is the registration (path, name, colour, slug). This is
 * the other half of a project: the checkout itself, which changes on its own —
 * a branch moves, files go uncommitted, a remote gets ahead — and is therefore
 * a place where work lives, which is what the sidebar's ⋯ menu lists.
 *
 * Rows are LOGICAL projects (docs/multi-machine.md) and each checkout is its
 * own line, because uncommitted files belong to one machine's copy. A line
 * opens that checkout's page (`/project/$slug/`): the project's own working
 * tree, outside any session's worktree.
 */
import { Link } from "@tanstack/react-router";
import { ChevronRight, FolderOpen, GitBranch } from "lucide-react";
import { type ReactNode, useMemo, useState } from "react";
import { useShallow } from "zustand/shallow";
import { GitIndicators } from "~/components/layout/git/GitIndicators";
import { PageHeader } from "~/components/layout/PageHeader";
import { MachineTag } from "~/components/machines/MachineTag";
import { Input } from "~/components/ui/input";
import { useLogicalProjects } from "~/hooks/useLogicalProjects";
import { type NamedMachine, useMachineNaming } from "~/hooks/useMachineNaming";
import { useTheme } from "~/hooks/useTheme";
import type { LogicalMemberVM } from "~/lib/machines/logical-derive";
import { compareLogicalProjects, matchesLogicalProject } from "~/lib/machines/logical-derive";
import { getProjectColor } from "~/lib/project-colors";
import { isFolderProject } from "~/lib/project-kind";
import { projectLabel } from "~/lib/project-label";
import { useAppStore } from "~/stores/app-store";

function truncatePath(path: string): string {
  return path.replace(/^\/home\/[^/]+/, "~").replace(/^\/Users\/[^/]+/, "~");
}

/** One machine's checkout: its branch and how far it has drifted. */
function CheckoutLine({
  member,
  machine,
  showMachine,
  title,
}: {
  member: LogicalMemberVM;
  machine: NamedMachine;
  showMachine: boolean;
  /** The row's own lead, when the project has one checkout and one line. */
  title?: ReactNode;
}) {
  const status = useAppStore((s) => s.projectGitStatus[member.projectId]);
  const folder = useAppStore((s) =>
    isFolderProject(s.projects.find((p) => p.id === member.projectId)),
  );

  return (
    <Link
      to="/project/$projectSlug"
      params={{ projectSlug: member.slug }}
      className="group flex min-w-0 items-center gap-2 rounded px-1.5 py-1 text-xs text-muted-foreground hover:bg-muted/50 hover:text-foreground"
    >
      {title}
      {showMachine && (
        <MachineTag machine={machine} offline={member.offline} className="max-w-32 shrink-0" />
      )}
      {folder ? (
        <span className="flex shrink-0 items-center gap-1 text-muted-foreground-faint">
          <FolderOpen className="size-3" />
          folder
        </span>
      ) : status?.branch ? (
        <span className="flex min-w-0 shrink items-center gap-1">
          <GitBranch className="size-3 shrink-0 text-muted-foreground-dim" />
          <span className="truncate font-mono">{status.branch}</span>
        </span>
      ) : null}
      <span className="hidden min-w-0 flex-1 truncate text-muted-foreground-faint sm:block">
        {truncatePath(member.path)}
      </span>
      <span className="ml-auto flex shrink-0 items-center gap-1.5">
        {status && (
          <GitIndicators
            uncommittedCount={status.uncommittedCount}
            aheadCount={status.aheadRemote}
            behindCount={status.behindRemote}
          />
        )}
        <ChevronRight className="size-3.5 text-muted-foreground-faint group-hover:text-foreground" />
      </span>
    </Link>
  );
}

export function ProjectsPage() {
  const projects = useAppStore((s) => s.projects);
  const projectIds = useAppStore(useShallow((s) => s.projects.map((p) => p.id)));
  const rows = useLogicalProjects();
  const { resolvedTheme } = useTheme();
  const { named, nameOf } = useMachineNaming();
  const [filter, setFilter] = useState("");

  const visible = useMemo(() => {
    const byId = new Map(projects.map((p) => [p.id, p]));
    return rows
      .filter((row) => matchesLogicalProject(row, byId, filter, true))
      .sort(compareLogicalProjects);
  }, [rows, projects, filter]);

  return (
    <div className="flex h-full flex-col">
      <PageHeader>
        <span className="flex-1 truncate font-semibold">Projects</span>
        <Link
          to="/settings/projects"
          className="shrink-0 text-xs text-muted-foreground hover:text-foreground"
        >
          Manage
        </Link>
      </PageHeader>
      <div className="flex-1 overflow-y-auto px-4 py-4">
        <div className="mx-auto max-w-3xl space-y-3">
          {projects.length > 6 && (
            <Input
              placeholder="Filter projects..."
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              className="max-w-xs"
            />
          )}
          {projects.length === 0 && (
            <p className="py-8 text-center text-[13px] text-muted-foreground">No projects yet.</p>
          )}
          {visible.length === 0 && projects.length > 0 && (
            <p className="py-8 text-center text-[13px] text-muted-foreground">
              No projects matching &ldquo;{filter}&rdquo;
            </p>
          )}
          <div className="space-y-2">
            {visible.map((row) => {
              const color = getProjectColor(row.color, row.id, projectIds, resolvedTheme);
              const lead = (
                <span className="flex min-w-0 shrink-0 items-center gap-2 text-foreground">
                  <span
                    className="size-2.5 shrink-0 rounded-full"
                    style={{ backgroundColor: color.bg }}
                  />
                  <span className="max-w-48 truncate text-[13px] font-medium">
                    {projectLabel(row.name, row.slug)}
                  </span>
                </span>
              );
              // One checkout on one machine: the project and its checkout are
              // the same line. Otherwise the name heads a line per checkout.
              const single = !named && row.members.length === 1;
              return (
                <div
                  key={row.id}
                  className="rounded-lg border border-border/60 bg-card px-2 py-1.5"
                >
                  {!single && <div className="px-1.5 py-1">{lead}</div>}
                  {row.members.map((member) => (
                    <CheckoutLine
                      key={member.projectId}
                      member={member}
                      machine={nameOf(member)}
                      showMachine={named}
                      title={single ? lead : undefined}
                    />
                  ))}
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}
