import { useNavigate } from "@tanstack/react-router";
import { ArrowLeft, FileText, Pencil, Plus, Trash2, Variable } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { PageHeader } from "~/components/layout/PageHeader";
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
import { TemplateForm } from "./TemplateForm";

export function TemplateListPage() {
  const navigate = useNavigate();
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

  const handleSaved = () => {
    setEditing(null);
    setCreating(false);
  };

  if (creating || editing) {
    return <TemplateForm template={editing} onDone={handleSaved} onCancel={handleSaved} />;
  }

  return (
    <div className="flex flex-col h-full">
      <PageHeader>
        <span className="font-semibold">Prompt Templates</span>
      </PageHeader>
      <div className="flex-1 overflow-y-auto">
        <div className="max-w-3xl mx-auto p-8 max-md:p-4 space-y-6">
          <div className="space-y-1">
            <button
              type="button"
              onClick={() => navigate({ to: "/" })}
              className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors mb-4"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              Home
            </button>
            <div className="flex items-center justify-between">
              <div>
                <h1 className="text-2xl font-semibold">Prompt Templates</h1>
                <p className="text-sm text-muted-foreground mt-1">
                  Reusable prompts with saved settings. Use them on any project.
                </p>
              </div>
              <Button onClick={() => setCreating(true)} size="sm">
                <Plus className="h-4 w-4 mr-1.5" />
                New template
              </Button>
            </div>
          </div>

          {templates.length === 0 && loaded && (
            <div className="text-center py-16 text-muted-foreground">
              <FileText className="h-10 w-10 mx-auto mb-3 opacity-40" />
              <p>No templates yet</p>
              <p className="text-sm mt-1">Create a reusable prompt to get started.</p>
            </div>
          )}

          <div className="space-y-2">
            {templates.map((tmpl) => {
              const tags = parseTags(tmpl.tags);
              const vars = extractVariables(tmpl.content);
              return (
                <div
                  key={tmpl.id}
                  className="group flex items-start gap-3 rounded-lg border px-4 py-3 hover:bg-muted/30 transition-colors"
                >
                  <FileText className="h-5 w-5 text-muted-foreground mt-0.5 shrink-0" />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-medium text-sm">{tmpl.name}</span>
                      {vars.length > 0 && (
                        <span className="flex items-center gap-0.5 text-xs text-muted-foreground">
                          <Variable className="h-3 w-3" />
                          {vars.length}
                        </span>
                      )}
                    </div>
                    {tmpl.description && (
                      <p className="text-xs text-muted-foreground mt-0.5 line-clamp-2">
                        {tmpl.description}
                      </p>
                    )}
                    {tags.length > 0 && (
                      <div className="flex gap-1 mt-1.5 flex-wrap">
                        {tags.map((tag) => (
                          <Badge key={tag} variant="secondary" className="text-[10px] px-1.5 py-0">
                            {tag}
                          </Badge>
                        ))}
                      </div>
                    )}
                  </div>
                  <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                    <button
                      type="button"
                      onClick={() => setEditing(tmpl)}
                      className="h-7 w-7 rounded-md hover:bg-muted flex items-center justify-center text-muted-foreground hover:text-foreground transition-colors"
                      title="Edit"
                    >
                      <Pencil className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      onClick={() => setDeleteTarget(tmpl)}
                      className="h-7 w-7 rounded-md hover:bg-destructive/10 flex items-center justify-center text-muted-foreground hover:text-destructive transition-colors"
                      title="Delete"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
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
    </div>
  );
}
