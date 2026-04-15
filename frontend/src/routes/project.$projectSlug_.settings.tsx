import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { ArrowLeft, Check } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { PageHeader, StatusPage } from "~/components/layout/PageHeader";
import { IconPicker } from "~/components/layout/project/IconPicker";
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
import { COLORS } from "~/lib/color-palette";
import type { BehaviorPresets, PresetDefinition } from "~/lib/generated-types";
import { cn, getErrorMessage, slugify } from "~/lib/utils";
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
  const [renameEditing, setRenameEditing] = useState(false);
  const [renameName, setRenameName] = useState("");
  const [renameSlug, setRenameSlug] = useState("");
  const [slugManual, setSlugManual] = useState(false);
  const [renameSaving, setRenameSaving] = useState(false);

  const [presets, setPresets] = useState<BehaviorPresets>(() =>
    parsePresets(project?.default_behavior_presets ?? ""),
  );
  const [presetsSaving, setPresetsSaving] = useState(false);
  const [presetsChanged, setPresetsChanged] = useState(false);
  const [presetDefs, setPresetDefs] = useState<PresetDefinition[]>([]);

  useEffect(() => {
    listPresetDefinitions()
      .then(setPresetDefs)
      .catch((err) => {
        console.error("listPresetDefinitions failed", err);
        toast.error("Failed to load preset definitions");
      });
  }, []);

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
    return <StatusPage message="Project not found" />;
  }

  const handleColorChange = async (color: string) => {
    try {
      const updated = await updateProject(project.id, { color });
      updateProjectStore(updated);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to update color"));
    }
  };

  const handleIconChange = async (icon: string) => {
    try {
      const updated = await updateProject(project.id, { icon });
      updateProjectStore(updated);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to update icon"));
    }
  };

  const handleDelete = async () => {
    try {
      await deleteProject(project.id);
      removeProject(project.id);
      navigate({ to: "/" });
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to delete project"));
    }
  };

  const handleRenameEdit = () => {
    setRenameName(project.name);
    setRenameSlug(project.slug);
    setSlugManual(true);
    setRenameEditing(true);
  };

  const handleRenameNameChange = (value: string) => {
    setRenameName(value);
    if (!slugManual) {
      setRenameSlug(slugify(value));
    }
  };

  const handleRenameSlugChange = (value: string) => {
    setRenameSlug(value);
    setSlugManual(true);
  };

  const handleRenameSave = async () => {
    const nameChanged = renameName !== project.name;
    const slugChanged = renameSlug !== project.slug;
    if (!nameChanged && !slugChanged) {
      setRenameEditing(false);
      return;
    }
    setRenameSaving(true);
    try {
      const updates: { name?: string; slug?: string } = {};
      if (nameChanged) updates.name = renameName;
      if (slugChanged) updates.slug = renameSlug;
      const updated = await updateProject(project.id, updates);
      updateProjectStore(updated);
      setRenameEditing(false);
      if (slugChanged) {
        navigate({
          to: "/project/$projectSlug/settings",
          params: { projectSlug: updated.slug },
          replace: true,
        });
      }
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to rename project"));
    } finally {
      setRenameSaving(false);
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
    <div className="flex flex-col h-full">
      <PageHeader>
        <span className="font-semibold truncate">{project.name} — Settings</span>
      </PageHeader>
      <div className="flex-1 overflow-y-auto">
        <div className="max-w-2xl mx-auto p-8 max-md:p-4 space-y-8 max-md:space-y-6">
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

          {/* ── Identity ── */}
          <section className="space-y-4">
            <div>
              <h2 className="text-lg font-medium">Project identity</h2>
              <p className="text-sm text-muted-foreground mt-1">
                Name, slug, color, and icon. The icon and color appear in the sidebar rail.
              </p>
            </div>

            {/* Name / slug */}
            {renameEditing ? (
              <div className="space-y-3">
                <div className="space-y-1">
                  <Label htmlFor="rename-name">Name</Label>
                  <Input
                    id="rename-name"
                    value={renameName}
                    onChange={(e) => handleRenameNameChange(e.target.value)}
                    placeholder="My Project"
                  />
                </div>
                <div className="space-y-1">
                  <Label htmlFor="rename-slug">Slug</Label>
                  <Input
                    id="rename-slug"
                    value={renameSlug}
                    onChange={(e) => handleRenameSlugChange(e.target.value)}
                    placeholder="my-project"
                    pattern="[a-z0-9][a-z0-9-]*[a-z0-9]|[a-z0-9]"
                  />
                </div>
                <div className="flex gap-2">
                  <Button onClick={handleRenameSave} disabled={renameSaving}>
                    {renameSaving ? "Saving..." : "Save"}
                  </Button>
                  <Button variant="outline" onClick={() => setRenameEditing(false)}>
                    Cancel
                  </Button>
                </div>
              </div>
            ) : (
              <div className="space-y-2">
                <div className="flex items-center gap-3">
                  <span className="text-sm font-medium w-12">Name</span>
                  <span className="text-sm">{project.name}</span>
                </div>
                <div className="flex items-center gap-3">
                  <span className="text-sm font-medium w-12">Slug</span>
                  <code className="text-sm bg-muted px-2 py-1 rounded">{project.slug}</code>
                </div>
                <Button variant="outline" size="sm" onClick={handleRenameEdit}>
                  Rename
                </Button>
              </div>
            )}

            {/* Color */}
            <div className="space-y-2">
              <Label>Color</Label>
              <div className="flex items-center gap-2 flex-wrap">
                <button
                  type="button"
                  onClick={() => handleColorChange("")}
                  className={cn(
                    "size-7 rounded-full border-2 transition-colors flex items-center justify-center",
                    !project.color
                      ? "border-primary"
                      : "border-muted-foreground/20 hover:border-muted-foreground/40",
                  )}
                  title="Auto"
                >
                  <span className="text-[10px] font-medium text-muted-foreground">A</span>
                </button>
                {COLORS.map((c) => (
                  <button
                    key={c.id}
                    type="button"
                    onClick={() => handleColorChange(c.id)}
                    className={cn(
                      "size-7 rounded-full border-2 transition-all flex items-center justify-center",
                      project.color === c.id
                        ? "border-primary scale-110"
                        : "border-transparent hover:scale-105",
                    )}
                    style={{ backgroundColor: c.bg }}
                    title={c.label}
                  >
                    {project.color === c.id && (
                      <Check className="size-3" style={{ color: c.text }} />
                    )}
                  </button>
                ))}
              </div>
            </div>

            {/* Icon */}
            <div className="space-y-2">
              <Label>Icon</Label>
              <IconPicker value={project.icon} onChange={handleIconChange} />
            </div>
          </section>

          <Separator />

          {/* ── Session behavior ── */}
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

          {/* ── Danger zone ── */}
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
    </div>
  );
}
