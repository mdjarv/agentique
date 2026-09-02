/**
 * Settings › Projects — the repo inventory.
 *
 * A project is a registration, not a place work accumulates: a path, a name, a
 * colour, a slug, and which machines hold a checkout. That is the same kind of
 * fact as a machine's name, which is why this sits beside Machines rather than
 * in the sidebar's tools menu.
 *
 * Rows are LOGICAL projects (multi-machine): one repo, one row, with a line per
 * machine that holds a checkout. The row's identity comes from the
 * representative — the primary machine's copy when it exists — and every action
 * still targets one physical member. Launching stays available here, but the
 * way in to work is the deck and the sidebar's New session button.
 */
import { useNavigate } from "@tanstack/react-router";
import { FolderPlus, Plus, Settings } from "lucide-react";
import { useMemo, useState } from "react";
import { useShallow } from "zustand/shallow";
import { NewProjectDialog } from "~/components/layout/project/NewProjectDialog";
import { SettingsSection } from "~/components/settings/SettingsLayout";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { useLogicalProjects } from "~/hooks/useLogicalProjects";
import { useTheme } from "~/hooks/useTheme";
import type { LogicalMemberVM } from "~/lib/machines/logical-derive";
import { compareLogicalProjects, matchesLogicalProject } from "~/lib/machines/logical-derive";
import { resolveMachineGlyph } from "~/lib/machines/platform";
import { getProjectColor } from "~/lib/project-colors";
import { useAppStore } from "~/stores/app-store";

function truncatePath(path: string): string {
  return path.replace(/^\/home\/[^/]+/, "~").replace(/^\/Users\/[^/]+/, "~");
}

/** One machine's checkout of the repo — the physical thing a click targets. */
function MemberLine({ member, onLaunch }: { member: LogicalMemberVM; onLaunch: () => void }) {
  const Icon = resolveMachineGlyph(member.machineIcon, member.machinePlatform);
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

export function ProjectsSettings() {
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
    <SettingsSection
      title="Projects"
      description={`${projects.length} registered`}
      action={
        <NewProjectDialog
          trigger={
            <Button size="sm">
              <FolderPlus className="h-4 w-4" />
              New project
            </Button>
          }
        />
      }
    >
      {/* The filter earns its place only once the list is long enough to
          scan badly. */}
      {projects.length > 6 && (
        <Input
          placeholder="Filter projects..."
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="max-w-xs"
        />
      )}

      {projects.length === 0 && (
        <div className="flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed border-border/60 py-12">
          <FolderPlus className="h-8 w-8 text-muted-foreground/20" />
          <p className="text-[13px] text-muted-foreground">
            No projects yet. Add a repo to get started.
          </p>
        </div>
      )}

      {filteredProjects.length === 0 && projects.length > 0 && (
        <p className="py-8 text-center text-[13px] text-muted-foreground">
          No projects matching &ldquo;{filter}&rdquo;
        </p>
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
              className="flex items-center gap-3 rounded-lg border border-border/60 bg-card px-3.5 py-3 transition-colors hover:bg-muted/50"
            >
              <span
                className="size-3 shrink-0 rounded-full"
                style={{ backgroundColor: color.bg }}
              />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate text-[13px] font-medium">{row.name}</span>
                  <code className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
                    {row.slug}
                  </code>
                  {row.favorite && (
                    <span className="text-[10px] text-muted-foreground-faint">fav</span>
                  )}
                </div>
                {/* A repo that lives only on this machine reads as a plain
                    path. As soon as another machine holds a checkout —
                    whether alongside this one or instead of it — every
                    member gets its own launchable line, so where the code
                    lives and whether it is reachable are both on the row. */}
                {row.members.some((m) => m.machineId) ? (
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
                  <span className="mt-0.5 block truncate text-xs text-muted-foreground-faint">
                    {truncatePath(row.path)}
                  </span>
                )}
              </div>
              <div className="flex shrink-0 items-center gap-1">
                <Button
                  variant="ghost"
                  size="icon-sm"
                  disabled={row.away}
                  onClick={() => newSession(row.slug)}
                  title={row.away ? "Every machine holding this repo is offline" : "New session"}
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
                  title="Project settings"
                >
                  <Settings className="h-4 w-4" />
                </Button>
              </div>
            </div>
          );
        })}
      </div>
    </SettingsSection>
  );
}
