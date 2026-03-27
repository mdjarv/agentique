import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
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
import { deleteProject, listPresetDefinitions, updateProject } from "~/lib/api";
import type { BehaviorPresets, PresetDefinition } from "~/lib/generated-types";
import { getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";

export const Route = createFileRoute("/project/$projectSlug_/settings")({
  component: ProjectSettingsPage,
});

const DEFAULT_PRESETS: BehaviorPresets = {
  autoCommit: true,
  suggestParallel: true,
  planFirst: false,
  terse: false,
};

function parsePresets(raw: string): BehaviorPresets {
  if (!raw || raw === "{}") return { ...DEFAULT_PRESETS };
  try {
    return { ...DEFAULT_PRESETS, ...(JSON.parse(raw) as Partial<BehaviorPresets>) };
  } catch {
    return { ...DEFAULT_PRESETS };
  }
}

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

  const [presets, setPresets] = useState<BehaviorPresets>(() =>
    parsePresets(project?.default_behavior_presets ?? ""),
  );
  const [presetsSaving, setPresetsSaving] = useState(false);
  const [presetsChanged, setPresetsChanged] = useState(false);
  const [presetDefs, setPresetDefs] = useState<PresetDefinition[]>([]);

  useEffect(() => {
    listPresetDefinitions()
      .then(setPresetDefs)
      .catch(() => {});
  }, []);

  // Reset presets when project's stored defaults change
  const projectPresetsRaw = project?.default_behavior_presets;
  useEffect(() => {
    if (projectPresetsRaw != null) {
      setPresets(parsePresets(projectPresetsRaw));
      setPresetsChanged(false);
    }
  }, [projectPresetsRaw]);

  const togglePreset = useCallback((key: keyof BehaviorPresets) => {
    setPresets((prev) => ({ ...prev, [key]: !prev[key] }));
    setPresetsChanged(true);
  }, []);

  const setCustomInstructions = useCallback((value: string) => {
    setPresets((prev) => ({ ...prev, customInstructions: value }));
    setPresetsChanged(true);
  }, []);

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

  const handlePresetsSave = async () => {
    setPresetsSaving(true);
    try {
      const updated = await updateProject(project.id, { behaviorPresets: presets });
      updateProjectStore(updated);
      setPresetsChanged(false);
      toast.success("Session behavior defaults saved");
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to save behavior presets"));
    } finally {
      setPresetsSaving(false);
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
            <h2 className="text-lg font-medium">Session behavior</h2>
            <p className="text-sm text-muted-foreground mt-1">
              Default system prompt presets for new sessions in this project.
            </p>
          </div>
          <div className="space-y-3">
            {presetDefs.map((def) => {
              const key = def.key as keyof BehaviorPresets;
              return (
                <button
                  key={def.key}
                  type="button"
                  onClick={() => togglePreset(key)}
                  className="flex items-start gap-3 w-full text-left rounded-lg border px-4 py-3 transition-colors hover:bg-muted/50"
                >
                  <div
                    className={`mt-0.5 h-5 w-9 flex-shrink-0 rounded-full transition-colors ${
                      presets[key] ? "bg-primary" : "bg-muted-foreground/30"
                    } relative`}
                  >
                    <div
                      className={`absolute top-0.5 h-4 w-4 rounded-full bg-white transition-transform ${
                        presets[key] ? "translate-x-4" : "translate-x-0.5"
                      }`}
                    />
                  </div>
                  <div className="min-w-0">
                    <div className="text-sm font-medium">{def.title}</div>
                    <div className="text-xs text-muted-foreground mt-0.5">{def.description}</div>
                  </div>
                </button>
              );
            })}
          </div>
          <div className="space-y-2">
            <Label htmlFor="custom-instructions">Custom instructions</Label>
            <textarea
              id="custom-instructions"
              value={presets.customInstructions ?? ""}
              onChange={(e) => setCustomInstructions(e.target.value)}
              placeholder="Additional instructions appended to the system prompt (e.g., 'only touch backend files', 'use conventional commits')..."
              className="w-full min-h-[80px] rounded-md border bg-transparent px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring resize-y"
              rows={3}
            />
          </div>
          {presetsChanged && (
            <Button onClick={handlePresetsSave} disabled={presetsSaving}>
              {presetsSaving ? "Saving..." : "Save behavior defaults"}
            </Button>
          )}
        </section>

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
