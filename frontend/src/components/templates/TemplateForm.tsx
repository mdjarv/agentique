import { ArrowLeft, Plus, Variable, X } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { PageHeader } from "~/components/layout/PageHeader";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { Separator } from "~/components/ui/separator";
import { EFFORT_LABELS, EFFORT_LEVELS, type EffortLevel } from "~/lib/composer-constants";
import type { PromptTemplate } from "~/lib/generated-types";
import { MODEL_LABELS, MODELS, type ModelId } from "~/lib/session/actions";
import {
  extractVariables,
  formatVariableName,
  parseSettings,
  parseTags,
  stringifySettings,
  stringifyTags,
  type TemplateSettings,
} from "~/lib/template-utils";
import { getErrorMessage } from "~/lib/utils";
import type { AutoApproveMode } from "~/stores/chat-store";
import { useTemplateStore } from "~/stores/template-store";

const PERMISSION_MODES = ["manual", "auto", "fullAuto"] as const;
const PERMISSION_LABELS: Record<string, string> = {
  manual: "Manual",
  auto: "Auto",
  fullAuto: "Full Auto",
};

interface TemplateFormProps {
  template: PromptTemplate | null;
  onDone: () => void;
  onCancel: () => void;
}

export function TemplateForm({ template, onDone, onCancel }: TemplateFormProps) {
  const { create, update } = useTemplateStore();
  const isEdit = !!template;

  const [name, setName] = useState(template?.name ?? "");
  const [description, setDescription] = useState(template?.description ?? "");
  const [content, setContent] = useState(template?.content ?? "");
  const [settings, setSettings] = useState<TemplateSettings>(() =>
    parseSettings(template?.settings ?? "{}"),
  );
  const [tags, setTags] = useState<string[]>(() => parseTags(template?.tags ?? "[]"));
  const [tagInput, setTagInput] = useState("");
  const [saving, setSaving] = useState(false);

  const variables = useMemo(() => extractVariables(content), [content]);

  const updateSetting = <K extends keyof TemplateSettings>(key: K, value: TemplateSettings[K]) => {
    setSettings((prev) => {
      const next = { ...prev };
      if (value === undefined || value === "") {
        delete next[key];
      } else {
        next[key] = value;
      }
      return next;
    });
  };

  const addTag = () => {
    const tag = tagInput.trim().toLowerCase();
    if (tag && !tags.includes(tag)) {
      setTags([...tags, tag]);
    }
    setTagInput("");
  };

  const removeTag = (tag: string) => {
    setTags(tags.filter((t) => t !== tag));
  };

  const handleTagKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") {
      e.preventDefault();
      addTag();
    }
  };

  const handleSave = async () => {
    if (!name.trim()) {
      toast.error("Name is required");
      return;
    }
    setSaving(true);
    try {
      const payload = {
        name: name.trim(),
        description: description.trim(),
        content,
        settings: stringifySettings(settings),
        tags: stringifyTags(tags),
      };
      if (isEdit) {
        await update(template.id, payload);
        toast.success("Template updated");
      } else {
        await create(payload);
        toast.success("Template created");
      }
      onDone();
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to save template"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex flex-col h-full">
      <PageHeader>
        <span className="font-semibold">{isEdit ? "Edit Template" : "New Template"}</span>
      </PageHeader>
      <div className="flex-1 overflow-y-auto">
        <div className="max-w-3xl mx-auto p-8 max-md:p-4 space-y-6">
          <button
            type="button"
            onClick={onCancel}
            className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            Back to templates
          </button>

          {/* Basic info */}
          <section className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="tmpl-name">Name</Label>
              <Input
                id="tmpl-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g., Reliability Review"
                autoFocus
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="tmpl-desc">Description</Label>
              <Input
                id="tmpl-desc"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Short description of what this template does"
              />
            </div>
          </section>

          <Separator />

          {/* Prompt content */}
          <section className="space-y-4">
            <div>
              <h2 className="text-lg font-medium">Prompt</h2>
              <p className="text-sm text-muted-foreground mt-1">
                {
                  "Use {{variable_name}} for placeholders — you'll be prompted to fill them at launch."
                }
              </p>
            </div>
            <textarea
              value={content}
              onChange={(e) => setContent(e.target.value)}
              placeholder={"Review the codebase for {{focus_area}} issues. Check for..."}
              className="w-full min-h-[200px] rounded-md border bg-transparent px-3 py-2 text-sm font-mono placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring resize-y"
              rows={8}
            />
            {variables.length > 0 && (
              <div className="flex items-center gap-2 flex-wrap">
                <Variable className="h-4 w-4 text-muted-foreground" />
                <span className="text-xs text-muted-foreground">Variables:</span>
                {variables.map((v) => (
                  <Badge key={v} variant="outline" className="text-xs font-mono">
                    {formatVariableName(v)}
                  </Badge>
                ))}
              </div>
            )}
          </section>

          <Separator />

          {/* Tags */}
          <section className="space-y-4">
            <div>
              <h2 className="text-lg font-medium">Tags</h2>
              <p className="text-sm text-muted-foreground mt-1">Organize templates by category.</p>
            </div>
            <div className="flex gap-2">
              <Input
                value={tagInput}
                onChange={(e) => setTagInput(e.target.value)}
                onKeyDown={handleTagKeyDown}
                placeholder="Add tag..."
                className="flex-1"
              />
              <Button variant="outline" size="sm" onClick={addTag} disabled={!tagInput.trim()}>
                <Plus className="h-3.5 w-3.5" />
              </Button>
            </div>
            {tags.length > 0 && (
              <div className="flex gap-1.5 flex-wrap">
                {tags.map((tag) => (
                  <Badge key={tag} variant="secondary" className="gap-1 pr-1">
                    {tag}
                    <button
                      type="button"
                      onClick={() => removeTag(tag)}
                      className="h-3.5 w-3.5 rounded-full hover:bg-foreground/10 flex items-center justify-center"
                    >
                      <X className="h-2.5 w-2.5" />
                    </button>
                  </Badge>
                ))}
              </div>
            )}
          </section>

          <Separator />

          {/* Session settings */}
          <section className="space-y-4">
            <div>
              <h2 className="text-lg font-medium">Session defaults</h2>
              <p className="text-sm text-muted-foreground mt-1">
                Pre-fill session settings when this template is used. Leave blank to use project
                defaults.
              </p>
            </div>
            <div className="grid grid-cols-2 max-md:grid-cols-1 gap-4">
              {/* Model */}
              <div className="space-y-2">
                <Label>Model</Label>
                <select
                  value={settings.model ?? ""}
                  onChange={(e) =>
                    updateSetting("model", (e.target.value || undefined) as ModelId | undefined)
                  }
                  className="w-full h-9 rounded-md border bg-transparent px-3 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                >
                  <option value="">Default</option>
                  {MODELS.map((m) => (
                    <option key={m} value={m}>
                      {MODEL_LABELS[m]}
                    </option>
                  ))}
                </select>
              </div>

              {/* Effort */}
              <div className="space-y-2">
                <Label>Effort</Label>
                <select
                  value={settings.effort ?? ""}
                  onChange={(e) =>
                    updateSetting(
                      "effort",
                      (e.target.value || undefined) as EffortLevel | undefined,
                    )
                  }
                  className="w-full h-9 rounded-md border bg-transparent px-3 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                >
                  <option value="">Default</option>
                  {EFFORT_LEVELS.filter((l) => l !== "").map((l) => (
                    <option key={l} value={l}>
                      {EFFORT_LABELS[l]}
                    </option>
                  ))}
                </select>
              </div>

              {/* Permission */}
              <div className="space-y-2">
                <Label>Permission mode</Label>
                <select
                  value={settings.autoApproveMode ?? ""}
                  onChange={(e) =>
                    updateSetting(
                      "autoApproveMode",
                      (e.target.value || undefined) as AutoApproveMode | undefined,
                    )
                  }
                  className="w-full h-9 rounded-md border bg-transparent px-3 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                >
                  <option value="">Default</option>
                  {PERMISSION_MODES.map((m) => (
                    <option key={m} value={m}>
                      {PERMISSION_LABELS[m]}
                    </option>
                  ))}
                </select>
              </div>

              {/* Worktree */}
              <div className="space-y-2">
                <Label>Worktree</Label>
                <select
                  value={
                    settings.worktree === undefined ? "" : settings.worktree ? "true" : "false"
                  }
                  onChange={(e) => {
                    const v = e.target.value;
                    updateSetting("worktree", v === "" ? undefined : v === "true");
                  }}
                  className="w-full h-9 rounded-md border bg-transparent px-3 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                >
                  <option value="">Default</option>
                  <option value="true">Worktree (isolated branch)</option>
                  <option value="false">Local (project directory)</option>
                </select>
              </div>

              {/* Plan mode */}
              <div className="space-y-2">
                <Label>Plan mode</Label>
                <select
                  value={
                    settings.planMode === undefined ? "" : settings.planMode ? "true" : "false"
                  }
                  onChange={(e) => {
                    const v = e.target.value;
                    updateSetting("planMode", v === "" ? undefined : v === "true");
                  }}
                  className="w-full h-9 rounded-md border bg-transparent px-3 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                >
                  <option value="">Default</option>
                  <option value="true">Plan first</option>
                  <option value="false">Chat (no plan)</option>
                </select>
              </div>
            </div>
          </section>

          <Separator />

          <div className="flex gap-3 pb-8">
            <Button onClick={handleSave} disabled={saving || !name.trim()}>
              {saving ? "Saving..." : isEdit ? "Save changes" : "Create template"}
            </Button>
            <Button variant="outline" onClick={onCancel}>
              Cancel
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
