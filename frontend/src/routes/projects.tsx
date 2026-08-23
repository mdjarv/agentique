/**
 * The repo inventory. Rows are LOGICAL projects (multi-machine): one repo,
 * one row, with a line per machine that holds a checkout. The row's identity
 * comes from the representative — the primary machine's copy when it exists —
 * and every action still targets one physical member.
 */
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { FolderPlus, Plus, Settings } from "lucide-react";
import { useMemo, useState } from "react";
import { useShallow } from "zustand/shallow";
import { PageHeader } from "~/components/layout/PageHeader";
import { NewProjectDialog } from "~/components/layout/project/NewProjectDialog";

import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { useLogicalProjects } from "~/hooks/useLogicalProjects";
import { useTheme } from "~/hooks/useTheme";
import { DEFAULT_MACHINE_ICON, getMachineIcon } from "~/lib/machines/icons";
import type { LogicalMemberVM } from "~/lib/machines/logical-derive";
import { compareLogicalProjects, matchesLogicalProject } from "~/lib/machines/logical-derive";
import { getProjectColor } from "~/lib/project-colors";
import { useAppStore } from "~/stores/app-store";

export const Route = createFileRoute("/projects")({
  component: ProjectsPage,
});

function truncatePath(path: string): string {
  return path.replace(/^\/home\/[^/]+/, "~").replace(/^\/Users\/[^/]+/, "~");
}

/** One machine's checkout of the repo — the physical thing a click targets. */
function MemberLine({ member, onLaunch }: { member: LogicalMemberVM; onLaunch: () => void }) {
  const Icon = getMachineIcon(member.machineIcon ?? "") ?? DEFAULT_MACHINE_ICON;
  return (
    <button
      type="button"
      onClick={onLaunch}
      disabled={member.offline}
      title={
        member.offline
          ? `${member.machineLabel} is offline`
          : `New session on ${member.machineLabel || "this machine"}`
      }
      className={`flex w-full items-center gap-1.5 rounded px-1 py-0.5 text-left text-xs ${
        member.offline
          ? "cursor-not-allowed text-muted-foreground-faint/70"
          : "cursor-pointer text-muted-foreground-faint hover:bg-muted/50 hover:text-foreground"
      }`}
    >
      <Icon className="size-3 shrink-0" />
      <span className="shrink-0">{member.machineLabel || "This machine"}</span>
      <span className="truncate">{truncatePath(member.path)}</span>
      {member.offline && <span className="ml-auto shrink-0 font-mono text-[10px]">offline</span>}
    </button>
  );
}

function ProjectsPage() {
  const navigate = useNavigate();
  const projects = useAppStore((s) => s.projects);
  const projectIds = useAppStore(useShallow((s) => s.projects.map((p) => p.id)));
  const rows = useLogicalProjects();
  const { resolvedTheme } = useTheme();
  const [filter, setFilter] = useState("");

  const filteredProjects = useMemo(() => {
    const byId = new Map(projects.map((p) => [p.id, p]));
    return rows
      .filter((row) => matchesLogicalProject(row, byId, filter, true))
      .sort(compareLogicalProjects);
  }, [rows, projects, filter]);

  return (
    <div className="flex flex-col h-full">
      <PageHeader>
        <span className="font-semibold">Projects</span>
      </PageHeader>
      <div className="flex-1 overflow-y-auto">
        <div className="max-w-3xl mx-auto p-8 max-md:p-4 space-y-6">
          <div className="flex items-center justify-between gap-4">
            <Input
              placeholder="Filter projects..."
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              className="max-w-xs"
            />
            <NewProjectDialog
              trigger={
                <Button>
                  <FolderPlus className="h-4 w-4" />
                  New project
                </Button>
              }
            />
          </div>

          {filteredProjects.length === 0 && projects.length > 0 && (
            <p className="text-sm text-muted-foreground text-center py-8">
              No projects matching &ldquo;{filter}&rdquo;
            </p>
          )}

          {projects.length === 0 && (
            <div className="flex flex-col items-center justify-center gap-4 py-16">
              <FolderPlus className="h-10 w-10 text-muted-foreground/20" />
              <p className="text-sm text-muted-foreground">
                No projects yet. Create one to get started.
              </p>
            </div>
          )}

          <div className="space-y-2">
            {filteredProjects.map((row) => {
              const color = getProjectColor(row.color, row.id, projectIds, resolvedTheme);
              const newSession = (slug: string) =>
                navigate({
                  to: "/project/$projectSlug/session/new",
                  params: { projectSlug: slug },
                });
              return (
                <div
                  key={row.id}
                  className="flex items-center gap-3 rounded-lg border px-4 py-3 transition-colors hover:bg-muted/50"
                >
                  <span
                    className="size-3 rounded-full shrink-0"
                    style={{ backgroundColor: color.bg }}
                  />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="font-medium text-sm truncate">{row.name}</span>
                      <code className="text-xs text-muted-foreground bg-muted px-1.5 py-0.5 rounded shrink-0">
                        {row.slug}
                      </code>
                      {row.favorite && (
                        <span className="text-[10px] text-muted-foreground-faint">fav</span>
                      )}
                    </div>
                    {/* One repo on one machine reads as a path; the moment it
                        spans machines, each checkout gets its own launchable
                        line — the row itself never targets two things. */}
                    {row.spansMachines ? (
                      <div className="mt-1 space-y-0.5">
                        {row.members.map((member) => (
                          <MemberLine
                            key={member.projectId}
                            member={member}
                            onLaunch={() => newSession(member.slug)}
                          />
                        ))}
                      </div>
                    ) : (
                      <span className="text-xs text-muted-foreground-faint truncate block mt-0.5">
                        {truncatePath(row.path)}
                      </span>
                    )}
                  </div>
                  <div className="flex items-center gap-1 shrink-0">
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      disabled={row.away}
                      onClick={() => newSession(row.slug)}
                      title={
                        row.away ? "Every machine holding this repo is offline" : "New session"
                      }
                    >
                      <Plus className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      onClick={() =>
                        navigate({
                          to: "/project/$projectSlug/settings",
                          params: { projectSlug: row.slug },
                        })
                      }
                      title="Settings"
                    >
                      <Settings className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}
