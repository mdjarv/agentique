import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog";
import { Button } from "~/components/ui/button";
import { Separator } from "~/components/ui/separator";
import { useProjects } from "~/hooks/useProjects";
import { deleteProject } from "~/lib/api";
import { useAppStore } from "~/stores/app-store";

export const Route = createFileRoute("/project/$projectId_/settings")({
  component: ProjectSettingsPage,
});

function ProjectSettingsPage() {
  const { projectId } = Route.useParams();
  const navigate = useNavigate();
  const projects = useProjects();
  const removeProject = useAppStore((s) => s.removeProject);
  const [showDeleteDialog, setShowDeleteDialog] = useState(false);

  const project = projects.find((p) => p.id === projectId);
  if (!project) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <p className="text-muted-foreground">Project not found</p>
      </div>
    );
  }

  const handleDelete = async () => {
    try {
      await deleteProject(project.id);
      removeProject(project.id);
      navigate({ to: "/" });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to delete project");
    }
  };

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="max-w-2xl mx-auto p-8 space-y-8">
        <div className="space-y-1">
          <button
            type="button"
            onClick={() =>
              navigate({ to: "/project/$projectId", params: { projectId: project.id } })
            }
            className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors mb-4"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            Back to project
          </button>
          <h1 className="text-2xl font-semibold">{project.name}</h1>
          <p className="text-sm text-muted-foreground">{project.path}</p>
        </div>

        <Separator />

        <section className="space-y-4">
          <div>
            <h2 className="text-lg font-medium text-destructive">Danger zone</h2>
            <p className="text-sm text-muted-foreground mt-1">
              Permanently delete this project and all its sessions.
            </p>
          </div>
          <Button variant="destructive" onClick={() => setShowDeleteDialog(true)}>
            Delete project
          </Button>
        </section>

        <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Delete project</AlertDialogTitle>
              <AlertDialogDescription>
                This will remove &ldquo;{project.name}&rdquo; and all its sessions. This cannot be
                undone.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction onClick={handleDelete}>Delete</AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
    </div>
  );
}
