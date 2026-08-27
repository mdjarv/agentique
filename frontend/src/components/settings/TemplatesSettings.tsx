/**
 * Settings › Templates — the prompt library.
 *
 * A template is configuration, not a destination: it is text plus saved
 * settings, it never changes on its own, and the place it is actually *used* is
 * the composer's `TemplatePicker`. So it lives here rather than in the sidebar's
 * tools menu, which is for places where work happens.
 *
 * The editor takes over the whole panel rather than opening a dialog, because a
 * template body is long-form text and a dialog gives it a scrollbar.
 */
import { FileText, Pencil, Plus, Trash2, Variable } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { SettingsSection } from "~/components/settings/SettingsLayout";
import { TemplateForm } from "~/components/templates/TemplateForm";
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
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import type { PromptTemplate } from "~/lib/generated-types";
import { extractVariables, parseTags } from "~/lib/template-utils";
import { getErrorMessage } from "~/lib/utils";
import { useTemplateStore } from "~/stores/template-store";

export function TemplatesSettings() {
  const { templates, loaded, load, remove } = useTemplateStore();
  const [editing, setEditing] = useState<PromptTemplate | null>(null);
  const [creating, setCreating] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<PromptTemplate | null>(null);

  useEffect(() => {
    if (!loaded) load();
  }, [loaded, load]);

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      await remove(deleteTarget.id);
      toast.success(`Deleted "${deleteTarget.name}"`);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to delete template"));
    }
    setDeleteTarget(null);
  };

  const handleDone = () => {
    setEditing(null);
    setCreating(false);
  };

  if (creating || editing) {
    return <TemplateForm template={editing} onDone={handleDone} onCancel={handleDone} />;
  }

  return (
    <SettingsSection
      title="Templates"
      description={`${templates.length} saved`}
      action={
        <Button size="sm" onClick={() => setCreating(true)}>
          <Plus className="h-4 w-4" />
          New template
        </Button>
      }
    >
      {templates.length === 0 && loaded && (
        <div className="flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed border-border/60 py-12">
          <FileText className="h-8 w-8 text-muted-foreground/20" />
          <p className="text-[13px] text-muted-foreground">
            No templates yet. Save a reusable prompt to get started.
          </p>
        </div>
      )}

      <div className="space-y-2">
        {templates.map((tmpl) => {
          const tags = parseTags(tmpl.tags);
          const vars = extractVariables(tmpl.content);
          return (
            <div
              key={tmpl.id}
              className="group flex items-start gap-3 rounded-lg border border-border/60 bg-card px-3.5 py-3 transition-colors hover:bg-muted/50"
            >
              <FileText className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="text-[13px] font-medium">{tmpl.name}</span>
                  {vars.length > 0 && (
                    <span
                      className="flex items-center gap-0.5 text-xs text-muted-foreground"
                      title={`${vars.length} variable${vars.length === 1 ? "" : "s"}`}
                    >
                      <Variable className="h-3 w-3" />
                      {vars.length}
                    </span>
                  )}
                </div>
                {tmpl.description && (
                  <p className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">
                    {tmpl.description}
                  </p>
                )}
                {tags.length > 0 && (
                  <div className="mt-1.5 flex flex-wrap gap-1">
                    {tags.map((tag) => (
                      <Badge key={tag} variant="secondary" className="px-1.5 py-0 text-[10px]">
                        {tag}
                      </Badge>
                    ))}
                  </div>
                )}
              </div>
              {/* Revealed on hover on a pointer, but focus-within keeps them
                  reachable by keyboard, where there is no hover to have. */}
              <div className="flex items-center gap-1 opacity-0 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100">
                <button
                  type="button"
                  onClick={() => setEditing(tmpl)}
                  className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                  title="Edit"
                >
                  <Pencil className="h-3.5 w-3.5" />
                </button>
                <button
                  type="button"
                  onClick={() => setDeleteTarget(tmpl)}
                  className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
                  title="Delete"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            </div>
          );
        })}
      </div>

      <AlertDialog open={!!deleteTarget} onOpenChange={() => setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete template</AlertDialogTitle>
            <AlertDialogDescription>
              Delete &ldquo;{deleteTarget?.name}&rdquo;? This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete}>Delete</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  );
}
