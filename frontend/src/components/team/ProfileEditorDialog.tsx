import { Loader2, Sparkles, UserPlus } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { Button } from "~/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "~/components/ui/dialog";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { Separator } from "~/components/ui/separator";
import { Textarea } from "~/components/ui/textarea";
import { useWebSocket } from "~/hooks/useWebSocket";
import { listPresetDefinitions } from "~/lib/api";
import {
  EFFORT_LABELS,
  EFFORT_LEVELS,
  type EffortLevel,
  PERMISSION_LABELS,
  PERMISSION_MODES,
} from "~/lib/composer-constants";
import type { BehaviorPresets, PresetDefinition } from "~/lib/generated-types";
import { MODEL_LABELS, MODELS } from "~/lib/session/actions";
import type { AgentProfileConfig, AgentProfileInfo } from "~/lib/team-actions";
import { createAgentProfile, generateAgentProfile, updateAgentProfile } from "~/lib/team-actions";
import { getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import type { AutoApproveMode } from "~/stores/chat-store";
import { useTeamStore } from "~/stores/team-store";

const DEFAULT_PRESETS: BehaviorPresets = {
  autoCommit: false,
  suggestParallel: false,
  planFirst: false,
  terse: false,
};

function emptyConfig(): AgentProfileConfig {
  return {
    model: "",
    effort: "",
    autoApproveMode: "",
    behaviorPresets: { ...DEFAULT_PRESETS },
    systemPromptAdditions: "",
  };
}

function hydrateConfig(c: AgentProfileConfig | undefined): AgentProfileConfig {
  const base = emptyConfig();
  if (!c) return base;
  return {
    model: c.model ?? "",
    effort: c.effort ?? "",
    autoApproveMode: c.autoApproveMode ?? "",
    behaviorPresets: { ...DEFAULT_PRESETS, ...(c.behaviorPresets ?? {}) },
    systemPromptAdditions: c.systemPromptAdditions ?? "",
  };
}

function stripConfig(c: AgentProfileConfig): AgentProfileConfig {
  const out: AgentProfileConfig = {};
  if (c.model) out.model = c.model;
  if (c.effort) out.effort = c.effort;
  if (c.autoApproveMode) out.autoApproveMode = c.autoApproveMode;
  const bp = c.behaviorPresets;
  if (
    bp &&
    (bp.autoCommit ||
      bp.suggestParallel ||
      bp.planFirst ||
      bp.terse ||
      bp.customInstructions?.trim())
  ) {
    out.behaviorPresets = {
      autoCommit: bp.autoCommit,
      suggestParallel: bp.suggestParallel,
      planFirst: bp.planFirst,
      terse: bp.terse,
      ...(bp.customInstructions?.trim() ? { customInstructions: bp.customInstructions } : {}),
    };
  }
  if (c.systemPromptAdditions?.trim()) out.systemPromptAdditions = c.systemPromptAdditions;
  return out;
}

export function ProfileEditorDialog({ profile }: { profile?: AgentProfileInfo }) {
  const ws = useWebSocket();
  const projects = useAppStore((s) => s.projects);
  const isEdit = !!profile;

  const [open, setOpen] = useState(false);
  const [name, setName] = useState(profile?.name ?? "");
  const [role, setRole] = useState(profile?.role ?? "");
  const [description, setDescription] = useState(profile?.description ?? "");
  const [projectId, setProjectId] = useState(profile?.projectId ?? "");
  const [avatar, setAvatar] = useState(profile?.avatar ?? "");
  const [config, setConfig] = useState<AgentProfileConfig>(() => hydrateConfig(profile?.config));
  const [generating, setGenerating] = useState(false);
  const [brief, setBrief] = useState("");
  const [showBrief, setShowBrief] = useState(false);
  const [saving, setSaving] = useState(false);
  const [presetDefs, setPresetDefs] = useState<PresetDefinition[]>([]);

  useEffect(() => {
    if (!open || presetDefs.length > 0) return;
    listPresetDefinitions()
      .then(setPresetDefs)
      .catch((err) => console.error("listPresetDefinitions failed", err));
  }, [open, presetDefs.length]);

  const togglePreset = useCallback((key: keyof BehaviorPresets) => {
    setConfig((prev) => ({
      ...prev,
      behaviorPresets: {
        ...DEFAULT_PRESETS,
        ...prev.behaviorPresets,
        [key]: !prev.behaviorPresets?.[key],
      },
    }));
  }, []);

  const setCustomInstructions = useCallback((value: string) => {
    setConfig((prev) => ({
      ...prev,
      behaviorPresets: {
        ...DEFAULT_PRESETS,
        ...prev.behaviorPresets,
        customInstructions: value,
      },
    }));
  }, []);

  const handleGenerate = useCallback(async () => {
    if (!projectId || generating) return;
    setGenerating(true);
    try {
      const result = await generateAgentProfile(ws, {
        projectId,
        brief: brief.trim() || undefined,
      });
      setName(result.name);
      setRole(result.role);
      setDescription(result.description);
      setAvatar(result.avatar);
    } catch (e) {
      toast.error(getErrorMessage(e, "Failed to generate profile"));
    } finally {
      setGenerating(false);
    }
  }, [ws, projectId, brief, generating]);

  const handleSave = useCallback(async () => {
    setSaving(true);
    try {
      const params = {
        name,
        role,
        description,
        projectId,
        avatar,
        config: JSON.stringify(stripConfig(config)),
      };
      if (isEdit && profile) {
        const updated = await updateAgentProfile(ws, { id: profile.id, ...params });
        useTeamStore.getState().updateProfile(updated);
      } else {
        const created = await createAgentProfile(ws, params);
        useTeamStore.getState().addProfile(created);
      }
      setOpen(false);
      if (!isEdit) {
        setName("");
        setRole("");
        setDescription("");
        setProjectId("");
        setAvatar("");
        setConfig(emptyConfig());
      }
    } catch (e) {
      toast.error(getErrorMessage(e, "Operation failed"));
    } finally {
      setSaving(false);
    }
  }, [ws, isEdit, profile, name, role, description, projectId, avatar, config]);

  const handleOpenChange = useCallback(
    (nextOpen: boolean) => {
      setOpen(nextOpen);
      if (nextOpen && profile) {
        setName(profile.name);
        setRole(profile.role);
        setDescription(profile.description);
        setProjectId(profile.projectId);
        setAvatar(profile.avatar);
        setConfig(hydrateConfig(profile.config));
      }
      if (!nextOpen) {
        setBrief("");
        setShowBrief(false);
      }
    },
    [profile],
  );

  const trigger = isEdit ? (
    <button type="button" className="font-medium hover:underline text-left">
      {profile.name || "Unnamed"}
    </button>
  ) : (
    <Button variant="ghost" size="icon" className="size-6">
      <UserPlus className="size-3" />
    </Button>
  );

  const bp = config.behaviorPresets ?? DEFAULT_PRESETS;

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent className="sm:max-w-lg max-h-[85vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle>{isEdit ? "Edit Agent Profile" : "New Agent Profile"}</DialogTitle>
          <DialogDescription>
            {isEdit
              ? "Update this agent's identity and session defaults."
              : "Create a persistent agent identity with session defaults."}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4 overflow-y-auto pr-1 -mr-1">
          {/* Identity */}
          <div className="space-y-3">
            <div className="grid grid-cols-[1fr_80px] gap-3">
              <div>
                <Label htmlFor="profile-name">Name</Label>
                <Input
                  id="profile-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="Backend Expert"
                  autoFocus
                />
              </div>
              <div>
                <Label htmlFor="profile-avatar">Avatar</Label>
                <Input
                  id="profile-avatar"
                  value={avatar}
                  onChange={(e) => setAvatar(e.target.value)}
                  placeholder="🤖"
                  className="text-center"
                />
              </div>
            </div>
            <div>
              <Label htmlFor="profile-role">Role</Label>
              <Input
                id="profile-role"
                value={role}
                onChange={(e) => setRole(e.target.value)}
                placeholder="backend architect"
              />
            </div>
            <div>
              <Label htmlFor="profile-desc">Description</Label>
              <Textarea
                id="profile-desc"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Handles API endpoints, database migrations, and backend infrastructure."
                rows={3}
              />
            </div>
            <div>
              <Label htmlFor="profile-project">Home Project</Label>
              <select
                id="profile-project"
                className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors"
                value={projectId}
                onChange={(e) => setProjectId(e.target.value)}
              >
                <option value="">None</option>
                {projects.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </div>
            {projectId && (
              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={handleGenerate}
                    disabled={generating}
                  >
                    {generating ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <Sparkles className="h-3.5 w-3.5" />
                    )}
                    {generating ? "Generating..." : "Generate from project"}
                  </Button>
                  <button
                    type="button"
                    className="text-xs text-muted-foreground hover:text-foreground"
                    onClick={() => setShowBrief((v) => !v)}
                  >
                    {showBrief ? "Hide brief" : "+ Add brief"}
                  </button>
                </div>
                {showBrief && (
                  <Input
                    value={brief}
                    onChange={(e) => setBrief(e.target.value)}
                    placeholder="e.g. Focus on API endpoints and database migrations"
                    className="text-xs"
                    disabled={generating}
                  />
                )}
              </div>
            )}
          </div>

          <Separator />

          {/* Session defaults */}
          <div className="space-y-3">
            <div>
              <h3 className="text-sm font-medium">Session defaults</h3>
              <p className="text-xs text-muted-foreground mt-0.5">
                Applied when a session is created from this profile. Explicit overrides still win.
              </p>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div>
                <Label htmlFor="profile-model">Model</Label>
                <select
                  id="profile-model"
                  className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm"
                  value={config.model ?? ""}
                  onChange={(e) => setConfig((c) => ({ ...c, model: e.target.value }))}
                >
                  <option value="">Project default</option>
                  {MODELS.map((m) => (
                    <option key={m} value={m}>
                      {MODEL_LABELS[m]}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <Label htmlFor="profile-effort">Effort</Label>
                <select
                  id="profile-effort"
                  className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm"
                  value={config.effort ?? ""}
                  onChange={(e) => setConfig((c) => ({ ...c, effort: e.target.value }))}
                >
                  <option value="">Project default</option>
                  {EFFORT_LEVELS.filter((l): l is Exclude<EffortLevel, ""> => l !== "").map((l) => (
                    <option key={l} value={l}>
                      {EFFORT_LABELS[l]}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            <div>
              <Label htmlFor="profile-autoapprove">Permission mode</Label>
              <select
                id="profile-autoapprove"
                className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm"
                value={config.autoApproveMode ?? ""}
                onChange={(e) => setConfig((c) => ({ ...c, autoApproveMode: e.target.value }))}
              >
                <option value="">Project default</option>
                {PERMISSION_MODES.map((m: AutoApproveMode) => (
                  <option key={m} value={m}>
                    {PERMISSION_LABELS[m]}
                  </option>
                ))}
              </select>
            </div>
          </div>

          <Separator />

          {/* Behavior presets */}
          <div className="space-y-3">
            <div>
              <h3 className="text-sm font-medium">Behavior presets</h3>
              <p className="text-xs text-muted-foreground mt-0.5">
                Override the project's preset toggles for sessions launched from this profile.
              </p>
            </div>
            <div className="space-y-2">
              {presetDefs.map((def) => {
                const key = def.key as keyof BehaviorPresets;
                const active = !!bp[key];
                return (
                  <button
                    key={def.key}
                    type="button"
                    onClick={() => togglePreset(key)}
                    className="flex items-start gap-3 w-full text-left rounded-lg border px-3 py-2 transition-colors hover:bg-muted/50"
                  >
                    <div
                      className={`mt-0.5 h-5 w-9 flex-shrink-0 rounded-full transition-colors ${
                        active ? "bg-primary" : "bg-muted-foreground/30"
                      } relative`}
                    >
                      <div
                        className={`absolute top-0.5 h-4 w-4 rounded-full bg-white transition-transform ${
                          active ? "translate-x-4" : "translate-x-0.5"
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
            <div>
              <Label htmlFor="profile-custom-instructions">Custom instructions</Label>
              <Textarea
                id="profile-custom-instructions"
                value={bp.customInstructions ?? ""}
                onChange={(e) => setCustomInstructions(e.target.value)}
                placeholder="Additional preset instructions (e.g., 'only touch backend files')..."
                rows={2}
              />
            </div>
          </div>

          <Separator />

          {/* System prompt additions */}
          <div className="space-y-2">
            <div>
              <h3 className="text-sm font-medium">System prompt additions</h3>
              <p className="text-xs text-muted-foreground mt-0.5">
                Appended to the session preamble. Use for persistent role context.
              </p>
            </div>
            <Textarea
              id="profile-system-prompt"
              value={config.systemPromptAdditions ?? ""}
              onChange={(e) => setConfig((c) => ({ ...c, systemPromptAdditions: e.target.value }))}
              placeholder="You are a senior backend architect. Prioritize correctness over speed..."
              rows={4}
            />
          </div>
        </div>
        <DialogFooter>
          <Button onClick={handleSave} disabled={!name.trim() || generating || saving}>
            {saving ? "Saving..." : isEdit ? "Save" : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
