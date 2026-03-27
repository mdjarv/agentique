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
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { Separator } from "~/components/ui/separator";
import { deleteProject, updateProject } from "~/lib/api";
import { getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";

export const Route = createFileRoute("/project/$projectSlug_/settings")({
  component: ProjectSettingsPage,
});

function ProjectSettingsPage() {
  const { projectSlug } = Route.useParams();
  const navigate = useNavigate();
  const project = useAppStore((s) => s.projects.find((p) => p.slug === projectSlug));
  const updateProjectStore = useAppStore((s) => s.updateProject);
  const removeProject = useAppStore((s) => s.removeProject);
  const [showDeleteDialog, setShowDeleteDialog] = useState(false);
  const [slug, setSlug] = useState("");
  const [slugEditing, setSlugEditing] = useState(false);
  const [slugSaving, setSlugSaving] = useState(false);

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
      toast.error(getErrorMessage(err, "Failed to delete project"));
    }
  };

  const handleSlugEdit = () => {
    setSlug(project.slug);
    setSlugEditing(true);
  };

  const handleSlugSave = async () => {
    if (slug === project.slug) {
      setSlugEditing(false);
      return;
    }
    setSlugSaving(true);
    try {
      const updated = await updateProject(project.id, { slug });
      updateProjectStore(updated);
      setSlugEditing(false);
      navigate({
        to: "/project/$projectSlug/settings",
        params: { projectSlug: updated.slug },
        replace: true,
      });
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to update slug"));
    } finally {
      setSlugSaving(false);
    }
  };

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="max-w-2xl mx-auto p-8 space-y-8">
        <div className="space-y-1">
          <button
            type="button"
            onClick={() =>
              navigate({
                to: "/project/$projectSlug",
                params: { projectSlug: project.slug },
              })
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
            <h2 className="text-lg font-medium">URL slug</h2>
            <p className="text-sm text-muted-foreground mt-1">
              Used in browser URLs. Changing it will break existing bookmarks.
            </p>
          </div>
          {slugEditing ? (
            <div className="flex items-end gap-2">
              <div className="space-y-1 flex-1">
                <Label htmlFor="slug">Slug</Label>
                <Input
                  id="slug"
                  value={slug}
                  onChange={(e) => setSlug(e.target.value)}
                  placeholder="my-project"
                  pattern="[a-z0-9][a-z0-9-]*[a-z0-9]|[a-z0-9]"
                />
              </div>
              <Button onClick={handleSlugSave} disabled={slugSaving}>
                {slugSaving ? "Saving..." : "Save"}
              </Button>
              <Button variant="outline" onClick={() => setSlugEditing(false)}>
                Cancel
              </Button>
            </div>
          ) : (
            <div className="flex items-center gap-2">
              <code className="text-sm bg-muted px-2 py-1 rounded">{project.slug}</code>
              <Button variant="outline" size="sm" onClick={handleSlugEdit}>
                Edit
              </Button>
            </div>
          )}
        </section>

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
