import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { FolderPlus, Plus, Settings } from "lucide-react";
import { useMemo, useState } from "react";
import { useShallow } from "zustand/shallow";
import { PageHeader } from "~/components/layout/PageHeader";
import { NewProjectDialog } from "~/components/layout/project/NewProjectDialog";

import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { useTheme } from "~/hooks/useTheme";
import { getProjectColor } from "~/lib/project-colors";
import { useAppStore } from "~/stores/app-store";

export const Route = createFileRoute("/projects")({
  component: ProjectsPage,
});

function truncatePath(path: string): string {
  return path.replace(/^\/home\/[^/]+/, "~").replace(/^\/Users\/[^/]+/, "~");
}

function ProjectsPage() {
  const navigate = useNavigate();
  const projects = useAppStore((s) => s.projects);
  const projectIds = useAppStore(useShallow((s) => s.projects.map((p) => p.id)));
  const { resolvedTheme } = useTheme();
  const [filter, setFilter] = useState("");

  const filteredProjects = useMemo(() => {
    const sorted = [...projects].sort((a, b) => {
      if (a.favorite !== b.favorite) return b.favorite - a.favorite;
      return a.name.localeCompare(b.name);
    });
    if (!filter) return sorted;
    const q = filter.toLowerCase();
    return sorted.filter(
      (p) =>
        p.name.toLowerCase().includes(q) ||
        p.slug.toLowerCase().includes(q) ||
        p.path.toLowerCase().includes(q),
    );
  }, [projects, filter]);

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
            {filteredProjects.map((project) => {
              const color = getProjectColor(project.color, project.id, projectIds, resolvedTheme);
              return (
                <div
                  key={project.id}
                  className="flex items-center gap-3 rounded-lg border px-4 py-3 transition-colors hover:bg-muted/50"
                >
                  <span
                    className="size-3 rounded-full shrink-0"
                    style={{ backgroundColor: color.bg }}
                  />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="font-medium text-sm truncate">{project.name}</span>
                      <code className="text-xs text-muted-foreground bg-muted px-1.5 py-0.5 rounded shrink-0">
                        {project.slug}
                      </code>
                      {project.favorite === 1 && (
                        <span className="text-[10px] text-muted-foreground-faint">fav</span>
                      )}
                    </div>
                    <span className="text-xs text-muted-foreground-faint truncate block mt-0.5">
                      {truncatePath(project.path)}
                    </span>
                  </div>
                  <div className="flex items-center gap-1 shrink-0">
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      onClick={() =>
                        navigate({
                          to: "/project/$projectSlug/session/new",
                          params: { projectSlug: project.slug },
                        })
                      }
                      title="New session"
                    >
                      <Plus className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      onClick={() =>
                        navigate({
                          to: "/project/$projectSlug/settings",
                          params: { projectSlug: project.slug },
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
